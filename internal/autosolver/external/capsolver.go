// Package external provides implementations for third-party CAPTCHA
// solving services (Capsolver, 2Captcha). These are pluggable solvers
// enabled via configuration.
package external

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

// CapsolverConfig holds Capsolver API configuration.
type CapsolverConfig struct {
	APIKey  string `json:"apiKey"`
	BaseURL string `json:"baseUrl,omitempty"` // Default: https://api.capsolver.com
	// PollInterval is the getTaskResult poll cadence. Default: 3s.
	PollInterval time.Duration `json:"-"`
}

// Capsolver implements autosolver.Solver using the Capsolver API.
// It supports reCAPTCHA v2, reCAPTCHA v3, hCaptcha, Cloudflare Turnstile,
// and Arkose Labs FunCaptcha.
type Capsolver struct {
	config CapsolverConfig
	client *http.Client
}

// NewCapsolver creates a Capsolver solver with the given configuration.
func NewCapsolver(cfg CapsolverConfig) *Capsolver {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.capsolver.com"
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 3 * time.Second
	}
	return &Capsolver{
		config: cfg,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Capsolver) Name() string  { return "capsolver" }
func (c *Capsolver) Priority() int { return 200 }

// CanHandle checks if the page contains a supported CAPTCHA type.
func (c *Capsolver) CanHandle(ctx context.Context, page autosolver.Page) (bool, error) {
	if c.config.APIKey == "" {
		return false, nil
	}

	html, err := page.HTML()
	if err != nil {
		return false, nil
	}

	return detectCaptchaType(html) != "", nil
}

// Solve submits the CAPTCHA to the Capsolver API (createTask → poll
// getTaskResult), then injects the returned token into the page and
// fires the challenge callback where applicable.
func (c *Capsolver) Solve(ctx context.Context, page autosolver.Page, executor autosolver.ActionExecutor) (*autosolver.Result, error) {
	result := &autosolver.Result{SolverUsed: "capsolver"}

	if c.config.APIKey == "" {
		result.Error = "capsolver API key not configured"
		return result, fmt.Errorf("capsolver API key not configured")
	}

	html, err := page.HTML()
	if err != nil {
		result.Error = fmt.Sprintf("get HTML: %v", err)
		return result, err
	}

	captchaType := detectCaptchaType(html)
	if captchaType == "" {
		result.Error = "no supported CAPTCHA detected"
		return result, fmt.Errorf("no supported CAPTCHA detected on page")
	}

	key := extractSitekey(html, captchaType)
	if key == "" {
		result.Error = "sitekey not found"
		return result, fmt.Errorf("could not extract sitekey/public key from page")
	}

	taskType, ok := capsolverTaskType(captchaType)
	if !ok {
		result.Error = fmt.Sprintf("unsupported captcha type for capsolver: %s", captchaType)
		return result, fmt.Errorf("unsupported captcha type: %s", captchaType)
	}

	task := capsolverTask{Type: taskType, WebsiteURL: page.URL()}
	switch captchaType {
	case "funcaptcha":
		// FunCaptcha additionally needs the Arkose API JS subdomain and — for
		// modern deployments (LinkedIn) — the per-session data blob. The blob,
		// public key, and service URL are captured at document-start by the
		// bridge's Arkose hook (window.__ptArkose); prefer those live values
		// over static HTML.
		task.WebsitePublicKey = key
		task.FuncaptchaApiJSSubdomain = extractArkoseSubdomain(html)
		blob := ""
		if ac := readArkoseCapture(ctx, executor); ac != nil {
			if ac.PK != "" {
				task.WebsitePublicKey = ac.PK
			}
			if ac.Surl != "" {
				task.FuncaptchaApiJSSubdomain = ac.Surl
			}
			blob = ac.Blob
		}
		if blob != "" {
			// map[string]string can't fail to marshal; err branch is unreachable.
			if b, err := json.Marshal(map[string]string{"blob": blob}); err == nil {
				task.Data = string(b)
			}
		}
		// Match the solve UA to the browser's so Arkose accepts the token.
		task.UserAgent = readUserAgent(ctx, executor)
	case "recaptcha-v3":
		// v3 has no widget — the sitekey comes from ?render= and the task needs
		// the action passed to grecaptcha.execute (best-effort; optional).
		task.WebsiteKey = key
		task.PageAction = extractRecaptchaAction(html)
	default:
		task.WebsiteKey = key
	}

	token, err := c.solveRemote(ctx, task)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}

	if err := injectToken(ctx, executor, captchaType, token); err != nil {
		result.Error = fmt.Sprintf("inject token: %v", err)
		return result, err
	}

	result.Solved = true
	result.FinalTitle = page.Title()
	result.FinalURL = page.URL()
	return result, nil
}

// capsolverTaskType maps an internal captcha type to a Capsolver
// proxyless task type. Proxyless means Capsolver solves from its own
// infrastructure — browsing-side proxying is a separate concern.
func capsolverTaskType(captchaType string) (string, bool) {
	switch captchaType {
	case "recaptcha":
		return "ReCaptchaV2TaskProxyLess", true
	case "recaptcha-v3":
		return "ReCaptchaV3TaskProxyLess", true
	case "hcaptcha":
		return "HCaptchaTaskProxyLess", true
	case "turnstile":
		return "AntiTurnstileTaskProxyLess", true
	case "funcaptcha":
		return "FunCaptchaTaskProxyLess", true
	default:
		return "", false
	}
}

// --- Capsolver API v1 client ---

type capsolverTask struct {
	Type       string `json:"type"`
	WebsiteURL string `json:"websiteURL"`
	// reCAPTCHA / hCaptcha / Turnstile use websiteKey (data-sitekey, or the
	// ?render= sitekey for reCAPTCHA v3).
	WebsiteKey string `json:"websiteKey,omitempty"`
	// PageAction is the reCAPTCHA v3 action (the value passed to
	// grecaptcha.execute). Optional — CapSolver applies a default when absent.
	PageAction string `json:"pageAction,omitempty"`
	// FunCaptcha uses websitePublicKey (data-pkey) + the Arkose API subdomain,
	// and (for modern deployments like LinkedIn) the per-session data blob,
	// passed as a JSON-encoded string e.g. {"blob":"…"}.
	WebsitePublicKey         string `json:"websitePublicKey,omitempty"`
	FuncaptchaApiJSSubdomain string `json:"funcaptchaApiJSSubdomain,omitempty"`
	Data                     string `json:"data,omitempty"`
	// UserAgent is forwarded for FunCaptcha so CapSolver solves under the same
	// UA the browser uses — Arkose binds the token to the UA, so a mismatch is a
	// common cause of an otherwise-valid token being rejected. Optional.
	UserAgent string `json:"userAgent,omitempty"`
}

type capsolverCreateRequest struct {
	ClientKey string        `json:"clientKey"`
	Task      capsolverTask `json:"task"`
}

type capsolverResultRequest struct {
	ClientKey string `json:"clientKey"`
	TaskID    string `json:"taskId"`
}

type capsolverSolution struct {
	GRecaptchaResponse string `json:"gRecaptchaResponse"`
	Token              string `json:"token"`
}

type capsolverResponse struct {
	ErrorID          int               `json:"errorId"`
	ErrorCode        string            `json:"errorCode"`
	ErrorDescription string            `json:"errorDescription"`
	TaskID           string            `json:"taskId"`
	Status           string            `json:"status"`
	Solution         capsolverSolution `json:"solution"`
}

// solveRemote runs the full create → poll cycle for a prebuilt task and
// returns the token. The caller (Solve) populates the type-specific fields.
func (c *Capsolver) solveRemote(ctx context.Context, task capsolverTask) (string, error) {
	var created capsolverResponse
	if err := c.postJSON(ctx, "/createTask", capsolverCreateRequest{ClientKey: c.config.APIKey, Task: task}, &created); err != nil {
		return "", fmt.Errorf("capsolver createTask: %w", err)
	}
	if created.ErrorID != 0 {
		return "", fmt.Errorf("capsolver createTask error %s: %s", created.ErrorCode, created.ErrorDescription)
	}
	// Defensive fast-path: the ProxyLess task types we submit always return a
	// taskId and require polling, so created.Solution is empty here in practice.
	// This handles the documented case where a future/synchronous task type
	// returns the solution inline — guarded so we never poll a task we've solved.
	if tok := pickToken(created.Solution); tok != "" {
		return tok, nil
	}
	if created.TaskID == "" {
		return "", fmt.Errorf("capsolver createTask returned no taskId")
	}

	ticker := time.NewTicker(c.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("capsolver: context cancelled while polling: %w", ctx.Err())
		case <-ticker.C:
			var res capsolverResponse
			err := c.postJSON(ctx, "/getTaskResult", capsolverResultRequest{
				ClientKey: c.config.APIKey,
				TaskID:    created.TaskID,
			}, &res)
			if err != nil {
				return "", fmt.Errorf("capsolver getTaskResult: %w", err)
			}
			if res.ErrorID != 0 {
				return "", fmt.Errorf("capsolver getTaskResult error %s: %s", res.ErrorCode, res.ErrorDescription)
			}
			switch res.Status {
			case "ready":
				if tok := pickToken(res.Solution); tok != "" {
					return tok, nil
				}
				return "", fmt.Errorf("capsolver returned ready with empty token")
			case "failed", "error":
				// Terminal failure that didn't set errorId — stop polling
				// instead of burning the whole solver deadline.
				return "", fmt.Errorf("capsolver task failed (status %q): %s %s", res.Status, res.ErrorCode, res.ErrorDescription)
			}
			// status "processing" / "idle" → keep polling
		}
	}
}

// arkoseCapture holds the per-session Arkose values grabbed by the bridge's
// document-start hook (window.__ptArkose).
type arkoseCapture struct {
	Blob string `json:"blob"`
	PK   string `json:"pk"`
	Surl string `json:"surl"`
}

// readArkoseCapture reads window.__ptArkose from the live page. Returns nil if
// the hook captured nothing (non-Arkose page, or blob not yet set).
func readArkoseCapture(ctx context.Context, executor autosolver.ActionExecutor) *arkoseCapture {
	var raw string
	expr := `(function(){try{return window.__ptArkose?JSON.stringify(window.__ptArkose):"";}catch(e){return "";}})()`
	if err := executor.Evaluate(ctx, expr, &raw); err != nil || raw == "" {
		return nil
	}
	var ac arkoseCapture
	if err := json.Unmarshal([]byte(raw), &ac); err != nil {
		return nil
	}
	return &ac
}

// readUserAgent reads navigator.userAgent from the live page; "" if unavailable.
func readUserAgent(ctx context.Context, executor autosolver.ActionExecutor) string {
	var ua string
	if err := executor.Evaluate(ctx, "navigator.userAgent", &ua); err != nil {
		return ""
	}
	return ua
}

// pickToken returns the solve token regardless of which field CapSolver used.
// Across the four supported types the two fields are mutually exclusive —
// reCAPTCHA/hCaptcha populate gRecaptchaResponse, Turnstile/FunCaptcha populate
// token — so preference order is irrelevant today. If a future task type ever
// populates both, this preference becomes load-bearing; revisit then.
func pickToken(s capsolverSolution) string {
	if s.GRecaptchaResponse != "" {
		return s.GRecaptchaResponse
	}
	return s.Token
}

func (c *Capsolver) postJSON(ctx context.Context, path string, body, out interface{}) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.BaseURL+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return json.Unmarshal(data, out)
}

// --- token injection ---

// injectToken writes the solved token into the page's response field(s)
// and, for reCAPTCHA, fires the registered callback so the host page
// advances without a manual submit.
func injectToken(ctx context.Context, executor autosolver.ActionExecutor, captchaType, token string) error {
	var js string
	switch captchaType {
	case "recaptcha", "recaptcha-v3":
		js = recaptchaInjectJS
	case "hcaptcha":
		js = hcaptchaInjectJS
	case "turnstile":
		js = turnstileInjectJS
	case "funcaptcha":
		js = funcaptchaInjectJS
	default:
		return fmt.Errorf("no injector for captcha type %q", captchaType)
	}
	// The injector is an IIFE taking the token as its argument. Encode the
	// token with json.Marshal — not %q — so it is a valid JS string literal
	// (Go's %q does not escape U+2028/U+2029, which break a JS string).
	tokenLit, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("encode token for injection: %w", err)
	}
	var ok bool
	expr := fmt.Sprintf("(%s)(%s)", js, tokenLit)
	return executor.Evaluate(ctx, expr, &ok)
}

const recaptchaInjectJS = `function(token){
  var els=document.querySelectorAll('textarea#g-recaptcha-response, textarea[name="g-recaptcha-response"]');
  if(!els.length){
    var ta=document.createElement('textarea');
    ta.id='g-recaptcha-response'; ta.name='g-recaptcha-response';
    ta.style.display='none'; document.body.appendChild(ta);
    els=[ta];
  }
  els.forEach(function(el){el.value=token;});
  try{
    var cfg=window.___grecaptcha_cfg;
    if(cfg&&cfg.clients){
      for(var cid in cfg.clients){
        var client=cfg.clients[cid];
        for(var k in client){
          var o=client[k];
          if(o&&typeof o==='object'){
            for(var kk in o){
              var oo=o[kk];
              if(oo&&typeof oo==='object'&&typeof oo.callback==='function'){
                try{oo.callback(token);}catch(e){}
              }
            }
          }
        }
      }
    }
  }catch(e){}
  return true;
}`

const hcaptchaInjectJS = `function(token){
  document.querySelectorAll('textarea[name="h-captcha-response"], textarea[name="g-recaptcha-response"]').forEach(function(el){el.value=token;});
  return true;
}`

const turnstileInjectJS = `function(token){
  document.querySelectorAll('input[name="cf-turnstile-response"], textarea[name="cf-turnstile-response"]').forEach(function(el){el.value=token;});
  return true;
}`

// funcaptchaInjectJS is best-effort: Arkose token consumption is
// integration-specific. We populate the common hidden token fields and
// emit the standard Arkose "challenge complete" postMessage that many
// enforcement embeds listen for. Sites with a custom callback may still
// require site-specific glue after the token is obtained.
const funcaptchaInjectJS = `function(token){
  var sel='input[name="fc-token"], input[name="verification-token"], input[name="arkose-token"], input[id*="arkose"], input[name*="captcha-token"], input[name="captchaResponse"]';
  document.querySelectorAll(sel).forEach(function(el){el.value=token;});
  // Preferred path: deliver the token through the site's own Arkose completion
  // callback, captured at document-start by the bridge hook.
  try{ if(typeof window.__ptArkoseOnCompleted==='function'){ window.__ptArkoseOnCompleted({token:token}); } }catch(e){}
  try{ window.postMessage(JSON.stringify({eventId:'challenge-complete',payload:{sessionToken:token}}),'*'); }catch(e){}
  return true;
}`

// captchaWidget is the CAPTCHA embedded on a page: its type and the key to
// submit, read off the same element so the two cannot disagree.
type captchaWidget struct {
	typ string
	key string
}

// findCaptchaWidget locates the CAPTCHA embedded on the page. Every marker is an
// attribute, a class, or a resource URL, never a loose word: a page that merely
// links to another vendor would otherwise be misclassified, and CapSolver
// rejects a mismatched task type outright with ERROR_INVALID_TASK_DATA.
func findCaptchaWidget(html string) captchaWidget {
	// A key whose element names no vendor; the vendor must come from the scripts.
	unclassifiedKey := ""
	// A vendor whose element carries no key — enough to name the type, but not to
	// pair it with some other element's key.
	keylessVendor := ""

	for _, tag := range htmlTagRe.FindAllString(html, -1) {
		if pk := attrValue(tag, pkeyAttrRe); pk != "" {
			return captchaWidget{"funcaptcha", pk}
		}

		key := attrValue(tag, siteKeyAttrRe)
		// Explicit-render widgets are often marked by id rather than class —
		// Cloudflare's challenge page uses <div id="g-recaptcha">.
		vendor := vendorFromMarkers(attrValue(tag, classAttrRe) + " " + attrValue(tag, idAttrRe))

		switch {
		case vendor != "" && key != "":
			return captchaWidget{vendor, key}
		case vendor != "" && keylessVendor == "":
			keylessVendor = vendor
		case key != "" && unclassifiedKey == "":
			unclassifiedKey = key
		}
	}

	vendor := vendorFromResources(html)
	if vendor == "" {
		vendor = keylessVendor
	}
	if vendor == "" {
		return captchaWidget{}
	}

	switch vendor {
	case "funcaptcha":
		return captchaWidget{vendor, firstSubmatch(html, arkosePkURLRe, publicKeyJSONRe)}
	case "recaptcha":
		if unclassifiedKey == "" {
			// A rendered challenge frame means a real v2 widget exists even though
			// no element carries data-sitekey — the explicit-render path.
			if k := firstSubmatch(html, recaptchaFrameKeyRe); k != "" {
				return captchaWidget{vendor, k}
			}
			// No widget at all but api.js carries a render sitekey: that is v3.
			// render=explicit is v2's programmatic-render flag rather than a key.
			if m := recaptchaRenderRe.FindStringSubmatch(html); len(m) > 1 && !strings.EqualFold(m[1], "explicit") {
				return captchaWidget{"recaptcha-v3", m[1]}
			}
		}
	case "turnstile", "hcaptcha":
		if unclassifiedKey == "" {
			if k := firstSubmatch(html, frameSitekeyRe); k != "" {
				return captchaWidget{vendor, k}
			}
		}
	}
	return captchaWidget{vendor, unclassifiedKey}
}

// vendorFromMarkers names the vendor from one element's class and id tokens.
func vendorFromMarkers(markers string) string {
	switch lower := strings.ToLower(markers); {
	case strings.Contains(lower, "cf-turnstile"):
		return "turnstile"
	case strings.Contains(lower, "h-captcha"):
		return "hcaptcha"
	case strings.Contains(lower, "g-recaptcha"):
		return "recaptcha"
	}
	return ""
}

// vendorFromResources names the vendor from the scripts and frames the page
// loads, covering widgets rendered programmatically with no vendor class.
func vendorFromResources(html string) string {
	switch {
	case arkoseSrcRe.MatchString(html):
		return "funcaptcha"
	case turnstileSrcRe.MatchString(html):
		return "turnstile"
	case hcaptchaSrcRe.MatchString(html):
		return "hcaptcha"
	case recaptchaSrcRe.MatchString(html):
		return "recaptcha"
	}
	return ""
}

// attrValue reads one attribute out of a single tag, or "" when absent.
func attrValue(tag string, re *regexp.Regexp) string {
	if m := re.FindStringSubmatch(tag); len(m) > 1 {
		return m[1]
	}
	return ""
}

// firstSubmatch returns the first capture from the first regexp that matches.
func firstSubmatch(s string, res ...*regexp.Regexp) string {
	for _, re := range res {
		if m := re.FindStringSubmatch(s); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

// detectCaptchaType classifies the CAPTCHA on a page, or "" if none is
// recognized. reCAPTCHA splits into "recaptcha" (v2 checkbox/invisible) and
// "recaptcha-v3". Enterprise reCAPTCHA is not yet distinguished and takes the
// v2 path.
func detectCaptchaType(html string) string {
	return findCaptchaWidget(html).typ
}

var (
	// htmlTagRe yields one tag at a time so a marker and the key it belongs to
	// are read from the same element rather than from anywhere in the document.
	htmlTagRe   = regexp.MustCompile(`(?s)<[a-zA-Z][^>]*>`)
	classAttrRe = regexp.MustCompile(`(?i)\bclass\s*=\s*["']([^"']*)["']`)
	idAttrRe    = regexp.MustCompile(`(?i)\bid\s*=\s*["']([^"']*)["']`)

	// Explicit-render widgets carry no data-sitekey: the key rides the vendor's
	// own challenge frame URL, which is present once the widget has rendered.
	recaptchaFrameKeyRe = regexp.MustCompile(`(?i)/recaptcha/(?:api2|enterprise)/(?:anchor|bframe)\?[^"'>]*\bk=([A-Za-z0-9_-]{20,})`)
	frameSitekeyRe      = regexp.MustCompile(`(?i)[?&]sitekey=([A-Za-z0-9_-]{8,})`)

	// Vendor resource hosts. These identify the scripts and frames a page loads,
	// which is evidence of an embedded widget in a way a bare word is not.
	arkoseSrcRe    = regexp.MustCompile(`(?i)\b[a-z0-9-]*\.?arkoselabs\.com`)
	turnstileSrcRe = regexp.MustCompile(`(?i)\bchallenges\.cloudflare\.com`)
	hcaptchaSrcRe  = regexp.MustCompile(`(?i)\b(?:js\.|newassets\.)?hcaptcha\.com`)
	recaptchaSrcRe = regexp.MustCompile(`(?i)\b(?:www\.google\.com|www\.recaptcha\.net|recaptcha\.net)/recaptcha/`)

	pkeyAttrRe        = regexp.MustCompile(`(?i)data-pkey\s*=\s*["']([^"']+)["']`)
	publicKeyJSONRe   = regexp.MustCompile(`(?i)"?public_?key"?\s*[:=]\s*["']([0-9A-Fa-f-]{20,})["']`)
	arkosePkURLRe     = regexp.MustCompile(`(?i)[?&]pk=([0-9A-Fa-f-]{20,})`)
	arkoseSubdomainRe = regexp.MustCompile(`(?i)https?://([a-z0-9-]+\.arkoselabs\.com)`)
	// Case-insensitive so extraction matches detectCaptchaType (which lowercases);
	// tolerant of either quote style and whitespace around '='.
	siteKeyAttrRe = regexp.MustCompile(`(?i)data-sitekey\s*=\s*["']([^"']+)["']`)
	// reCAPTCHA v3: the sitekey rides the api.js script URL as ?…render=<key>.
	// Tolerates other query params before render= (e.g. ?onload=cb&render=key).
	recaptchaRenderRe = regexp.MustCompile(`(?i)recaptcha/(?:enterprise|api)\.js\?[^"'>]*\brender=([0-9A-Za-z_-]+)`)
	// reCAPTCHA v3 action: the value passed to grecaptcha.execute(key, {action:…}).
	recaptchaActionRe = regexp.MustCompile(`(?i)grecaptcha(?:\.enterprise)?\s*\.\s*execute\s*\(\s*["'][^"']*["']\s*,\s*\{[^}]*?action\s*:\s*["']([^"']+)["']`)
	// Fallback for the v3 action when it's declared as an HTML attribute.
	dataActionRe = regexp.MustCompile(`(?i)data-action\s*=\s*["']([^"']+)["']`)
)

// extractSitekey returns the key to submit for captchaType. It prefers the key
// found on the widget element itself, so a page hosting more than one vendor
// cannot pair one vendor's type with another's key.
func extractSitekey(html, captchaType string) string {
	if w := findCaptchaWidget(html); w.typ == captchaType && w.key != "" {
		return w.key
	}

	// The widget scan found nothing usable for this type — fall back to the
	// document-wide patterns, which is all a caller naming its own type can use.
	switch captchaType {
	case "funcaptcha":
		return firstSubmatch(html, pkeyAttrRe, arkosePkURLRe, publicKeyJSONRe)
	case "recaptcha-v3":
		// v3 has no data-sitekey widget; the key is in the api.js ?render= param.
		return firstSubmatch(html, recaptchaRenderRe)
	default:
		// reCAPTCHA v2 / hCaptcha / Turnstile all expose data-sitekey.
		return firstSubmatch(html, siteKeyAttrRe)
	}
}

// extractRecaptchaAction returns the action passed to grecaptcha.execute for a
// reCAPTCHA v3 page, or "" if none is found (the action is optional — CapSolver
// applies a default). Falls back to a data-action attribute.
func extractRecaptchaAction(html string) string {
	if m := recaptchaActionRe.FindStringSubmatch(html); len(m) > 1 {
		return m[1]
	}
	if m := dataActionRe.FindStringSubmatch(html); len(m) > 1 {
		return m[1]
	}
	return ""
}

// extractArkoseSubdomain finds the Arkose API JS host (e.g.
// client-api.arkoselabs.com, or a tenant subdomain like
// lnkd-api.arkoselabs.com) referenced by the page's enforcement script.
func extractArkoseSubdomain(html string) string {
	if m := arkoseSubdomainRe.FindStringSubmatch(html); len(m) > 1 {
		return "https://" + m[1]
	}
	return ""
}
