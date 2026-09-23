package external

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

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

// readTurnstileSitekey finds the sitekey of an explicitly rendered Turnstile.
// No DOM carries it (the widget lives in a closed shadow root); its frame URL
// does, and Resource Timing still lists that URL.
func readTurnstileSitekey(ctx context.Context, executor autosolver.ActionExecutor) string {
	var urls []string
	expr := `performance.getEntriesByType("resource").map(function(e){return e.name}).filter(function(n){return n.indexOf("/turnstile/")>=0})`
	if err := executor.Evaluate(ctx, expr, &urls); err != nil {
		return ""
	}
	for _, u := range urls {
		if m := turnstileFrameKeyRe.FindStringSubmatch(u); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

// geetestParams identifies a GeeTest challenge: gt and a single-use challenge
// for v3, a captcha ID for v4.
type geetestParams struct {
	version                  int
	gt, challenge, captchaID string
}

// readGeetest prefers what the bridge's document-start hook captured from the
// site's initGeetest call, then the vendor's own request URLs, which name the
// same values. A v3 challenge is single-use and reissued on refresh, so the most
// recent request wins.
func readGeetest(ctx context.Context, executor autosolver.ActionExecutor, html string) geetestParams {
	var hooked struct {
		Version       int
		GT, Challenge string
		CaptchaID     string `json:"captchaId"`
	}
	var raw string
	expr := `(function(){try{return window.__ptGeetest?JSON.stringify(window.__ptGeetest):"";}catch(e){return "";}})()`
	if executor.Evaluate(ctx, expr, &raw) == nil && raw != "" && json.Unmarshal([]byte(raw), &hooked) == nil {
		if hooked.Version == 4 && hooked.CaptchaID != "" {
			return geetestParams{version: 4, captchaID: hooked.CaptchaID}
		}
		if hooked.Version == 3 && hooked.GT != "" && hooked.Challenge != "" {
			return geetestParams{version: 3, gt: hooked.GT, challenge: hooked.Challenge}
		}
	}

	var urls []string
	resources := `performance.getEntriesByType("resource").map(function(e){return e.name}).filter(function(n){return n.indexOf("geetest.com")>=0})`
	_ = executor.Evaluate(ctx, resources, &urls)
	seen := html + "\n" + strings.Join(urls, "\n")
	if m := geetestV3Re.FindAllStringSubmatch(seen, -1); len(m) > 0 {
		last := m[len(m)-1]
		return geetestParams{version: 3, gt: last[1], challenge: last[2]}
	}
	if id := firstSubmatch(seen, geetestV4Re); id != "" {
		return geetestParams{version: 4, captchaID: id}
	}
	return geetestParams{}
}

// readMTCaptchaSitekey reads the key from window.mtcaptchaConfig, which holds it
// before the widget frame (and the key in its URL) has rendered.
func readMTCaptchaSitekey(ctx context.Context, executor autosolver.ActionExecutor) string {
	var key string
	expr := `(function(){try{return (window.mtcaptchaConfig&&window.mtcaptchaConfig.sitekey)||"";}catch(e){return "";}})()`
	if err := executor.Evaluate(ctx, expr, &key); err != nil {
		return ""
	}
	return key
}

// readTencentAppID prefers the app ID the bridge's hook caught being passed to
// new TencentCaptcha, then the aid on Tencent's prehandle request.
func readTencentAppID(ctx context.Context, executor autosolver.ActionExecutor) string {
	var id string
	hooked := `(function(){try{return (window.__ptTencent&&window.__ptTencent.appId)||"";}catch(e){return "";}})()`
	if executor.Evaluate(ctx, hooked, &id) == nil && id != "" {
		return id
	}
	var urls []string
	expr := `performance.getEntriesByType("resource").map(function(e){return e.name}).filter(function(n){return n.indexOf("cap_union_prehandle")>=0})`
	if executor.Evaluate(ctx, expr, &urls) != nil {
		return ""
	}
	return firstSubmatch(strings.Join(urls, "\n"), tencentAidRe)
}

// readHookedYidunID reads the captcha ID the hook caught passed to initNECaptcha.
func readHookedYidunID(ctx context.Context, executor autosolver.ActionExecutor) string {
	var id string
	expr := `(function(){try{return (window.__ptYidun&&window.__ptYidun.captchaId)||"";}catch(e){return "";}})()`
	if executor.Evaluate(ctx, expr, &id) != nil {
		return ""
	}
	return id
}

// readLeminDivID finds the element Lemin renders into, else its conventional id.
func readLeminDivID(ctx context.Context, executor autosolver.ActionExecutor) string {
	var id string
	expr := `(function(){try{var d=document.querySelector('[id^="lemin-cropped-captcha"]');return (d&&d.id)||"";}catch(e){return "";}})()`
	if executor.Evaluate(ctx, expr, &id) != nil || id == "" {
		return "lemin-cropped-captcha"
	}
	return id
}

// readUserAgent reads navigator.userAgent from the live page; "" if unavailable.
func readUserAgent(ctx context.Context, executor autosolver.ActionExecutor) string {
	var ua string
	if err := executor.Evaluate(ctx, "navigator.userAgent", &ua); err != nil {
		return ""
	}
	return ua
}

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
		return captchaWidget{vendor, firstSubmatch(html, arkosePkURLRe, arkoseAPIPathRe, publicKeyJSONRe)}
	case "recaptcha":
		if unclassifiedKey == "" {
			// No widget but api.js carries a render sitekey: that is v3. It goes
			// first because a rendered v3 leaves an invisible anchor frame too.
			// render=explicit is v2's programmatic-render flag rather than a key.
			if m := recaptchaRenderRe.FindStringSubmatch(html); len(m) > 1 && !strings.EqualFold(m[1], "explicit") {
				return captchaWidget{"recaptcha-v3", m[1]}
			}
			// A rendered challenge frame means a real v2 widget exists even though
			// no element carries data-sitekey — the explicit-render path.
			if k := firstSubmatch(html, recaptchaFrameKeyRe); k != "" {
				return captchaWidget{vendor, k}
			}
		}
	case "lemin":
		return captchaWidget{vendor, firstSubmatch(html, leminIDRe)}
	case "yidun":
		if unclassifiedKey == "" {
			return captchaWidget{vendor, firstSubmatch(html, yidunIDRe)}
		}
	case "turnstile", "hcaptcha", "mtcaptcha", "yandex", "prosopo":
		if unclassifiedKey == "" {
			if k := firstSubmatch(html, frameSitekeyRe); k != "" {
				return captchaWidget{vendor, k}
			}
		}
	}
	return captchaWidget{vendor, unclassifiedKey}
}

// widgetAttr reads re off the first element whose class or id carries marker.
func widgetAttr(html, marker string, re *regexp.Regexp) string {
	for _, tag := range htmlTagRe.FindAllString(html, -1) {
		markers := strings.ToLower(attrValue(tag, classAttrRe) + " " + attrValue(tag, idAttrRe))
		if !strings.Contains(markers, marker) {
			continue
		}
		if v := attrValue(tag, re); v != "" {
			return v
		}
	}
	return ""
}

// isRecaptchaEnterprise reports a page on the Enterprise API: its tokens are
// only valid from Enterprise solves.
func isRecaptchaEnterprise(html string) bool {
	return recaptchaEnterpriseRe.MatchString(html)
}

// isRecaptchaInvisible reports a v2 widget with no checkbox.
func isRecaptchaInvisible(html string) bool {
	return strings.EqualFold(widgetAttr(html, "g-recaptcha", dataSizeAttrRe), "invisible") ||
		invisibleAnchorRe.MatchString(html)
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
	case strings.Contains(lower, "mtcaptcha"):
		return "mtcaptcha"
	case strings.Contains(lower, "smart-captcha"):
		return "yandex"
	case strings.Contains(lower, "procaptcha"):
		return "prosopo"
	case strings.Contains(lower, "lemin-captcha"), strings.Contains(lower, "lemin-cropped"):
		return "lemin"
	case strings.Contains(lower, "tcaptcha_"):
		// tcaptcha_transform / tcaptcha_iframe exist only while the captcha is
		// shown; TCaptcha.js alone is loaded on pages that never show it.
		return "tencent"
	case strings.Contains(lower, "yidun"):
		return "yidun"
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
	case mtcaptchaSrcRe.MatchString(html):
		return "mtcaptcha"
	case awswafCaptchaSrcRe.MatchString(html):
		return "awswaf"
	case geetestSrcRe.MatchString(html):
		return "geetest"
	case yandexSrcRe.MatchString(html):
		return "yandex"
	case prosopoSrcRe.MatchString(html):
		return "prosopo"
	case leminIDRe.MatchString(html):
		return "lemin"
	}
	return ""
}

// awsWAFPage is what an AWS WAF captcha page hands its own SDK.
type awsWAFPage struct {
	Key, IV, Context string
	ChallengeJS      string
	// CookieDomains is awsWafCookieDomainList: where the SDK writes aws-waf-token.
	CookieDomains []string
}

// readAWSWAFPage reads window.gokuProps, the challenge.js URL and the cookie
// domain list off an AWS WAF captcha page. Any may be missing; CapSolver needs
// only the page URL when the page is served fresh.
func readAWSWAFPage(html string) awsWAFPage {
	var p awsWAFPage
	if m := gokuPropsRe.FindStringSubmatch(html); len(m) > 1 {
		var goku struct{ Key, IV, Context string }
		if json.Unmarshal([]byte(m[1]), &goku) == nil {
			p.Key, p.IV, p.Context = goku.Key, goku.IV, goku.Context
		}
	}
	p.ChallengeJS = firstSubmatch(html, awswafChallengeJSRe)
	if m := awswafCookieDomainsRe.FindStringSubmatch(html); len(m) > 1 {
		for _, q := range quotedRe.FindAllStringSubmatch(m[1], -1) {
			p.CookieDomains = append(p.CookieDomains, q[1])
		}
	}
	return p
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
// "recaptcha-v3"; Enterprise is a property of either (isRecaptchaEnterprise).
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
	// A Turnstile frame path carries its sitekey as a segment. Live keys start
	// 0x; Cloudflare's 1x/2x/3x test keys are refused by every solver.
	turnstileFrameKeyRe = regexp.MustCompile(`/turnstile/[^?]*/(0x[0-9A-Za-z_-]{10,})/`)

	// Vendor resource hosts. These identify the scripts and frames a page loads,
	// which is evidence of an embedded widget in a way a bare word is not.
	arkoseSrcRe    = regexp.MustCompile(`(?i)\b[a-z0-9-]*\.?arkoselabs\.com`)
	turnstileSrcRe = regexp.MustCompile(`(?i)\bchallenges\.cloudflare\.com`)
	hcaptchaSrcRe  = regexp.MustCompile(`(?i)\b(?:js\.|newassets\.)?hcaptcha\.com`)
	recaptchaSrcRe = regexp.MustCompile(`(?i)\b(?:www\.google\.com|www\.recaptcha\.net|recaptcha\.net)/recaptcha/`)
	mtcaptchaSrcRe = regexp.MustCompile(`(?i)\bservice\d*\.mtcaptcha\.com/`)
	// Only the captcha action loads captcha.js; a challenge-only page (token.
	// awswaf.com/…/challenge.js alone) clears in the browser with nothing to buy.
	awswafCaptchaSrcRe    = regexp.MustCompile(`(?i)\.captcha\.awswaf\.com/[^"'\s]*captcha\.js`)
	awswafChallengeJSRe   = regexp.MustCompile(`(?i)(https://[^"'\s]+\.token\.awswaf\.com/[^"'\s]*challenge\.js)`)
	gokuPropsRe           = regexp.MustCompile(`(?s)window\.gokuProps\s*=\s*(\{[^}]*\})`)
	awswafCookieDomainsRe = regexp.MustCompile(`awsWafCookieDomainList\s*=\s*\[([^\]]*)\]`)
	quotedRe              = regexp.MustCompile(`["']([^"']+)["']`)

	yandexSrcRe  = regexp.MustCompile(`(?i)\bsmartcaptcha\.yandexcloud\.net/`)
	prosopoSrcRe = regexp.MustCompile(`(?i)\bjs\.prosopo\.io/`)
	// Lemin's captcha script path names its captcha ID.
	leminIDRe = regexp.MustCompile(`(?i)leminnow\.com/captcha/v1/cropped/(CROPPED_[0-9a-z_]+)/js`)
	yidunIDRe = regexp.MustCompile(`(?i)captchaId["']?\s*[:=]\s*["']?([0-9a-f]{32})`)
	// Tencent's prehandle request names its app ID as aid.
	tencentAidRe = regexp.MustCompile(`cap_union_prehandle\?[^"'\s]*?\baid=(\d{6,})`)

	geetestSrcRe = regexp.MustCompile(`(?i)\b(?:api|static|gcaptcha4)\.geetest\.com/`)
	// v3's JSONP get.php carries gt and the live challenge; DOM copies escape & as &amp;.
	geetestV3Re = regexp.MustCompile(`(?i)api\.geetest\.com/get\.php\?[^"'\s<>]*?\bgt=([0-9a-f]{32})(?:&amp;|&)challenge=([0-9a-z]{32,})`)
	geetestV4Re = regexp.MustCompile(`(?i)gcaptcha4\.geetest\.com/load\?[^"'\s<>]*?\bcaptcha_id=([0-9a-f]{32})`)

	pkeyAttrRe      = regexp.MustCompile(`(?i)data-pkey\s*=\s*["']([^"']+)["']`)
	publicKeyJSONRe = regexp.MustCompile(`(?i)"?public_?key"?\s*[:=]\s*["']([0-9A-Fa-f-]{20,})["']`)
	arkosePkURLRe   = regexp.MustCompile(`(?i)[?&]pk=([0-9A-Fa-f-]{20,})`)
	// The current embed carries the key only in the script path: /v2/<key>/api.js.
	arkoseAPIPathRe   = regexp.MustCompile(`(?i)arkoselabs\.com/v2/([0-9A-Fa-f-]{36})/api\.js`)
	arkoseSubdomainRe = regexp.MustCompile(`(?i)https?://([a-z0-9-]+\.arkoselabs\.com)`)
	// Case-insensitive so extraction matches detectCaptchaType (which lowercases);
	// tolerant of either quote style and whitespace around '='.
	siteKeyAttrRe = regexp.MustCompile(`(?i)data-sitekey\s*=\s*["']([^"']+)["']`)
	// reCAPTCHA v3: the sitekey rides the api.js script URL as ?…render=<key>.
	// Tolerates other query params before render= (e.g. ?onload=cb&render=key).
	recaptchaRenderRe = regexp.MustCompile(`(?i)recaptcha/(?:enterprise|api)\.js\?[^"'>]*\brender=([0-9A-Za-z_-]+)`)
	// reCAPTCHA v3 action: the value passed to grecaptcha.execute(key, {action:…}).
	recaptchaActionRe     = regexp.MustCompile(`(?i)grecaptcha(?:\.enterprise)?\s*\.\s*execute\s*\(\s*["'][^"']*["']\s*,\s*\{[^}]*?action\s*:\s*["']([^"']+)["']`)
	recaptchaEnterpriseRe = regexp.MustCompile(`(?i)/recaptcha/enterprise(?:\.js|/)`)
	invisibleAnchorRe     = regexp.MustCompile(`(?i)/recaptcha/(?:api2|enterprise)/anchor\?[^"'>]*\bsize=invisible`)
	dataSizeAttrRe        = regexp.MustCompile(`(?i)\bdata-size\s*=\s*["']([^"']*)["']`)
	// data-s is Google's per-load value on some v2 widgets (notably Google's own).
	dataSAttrRe     = regexp.MustCompile(`(?i)\bdata-s\s*=\s*["']([^"']*)["']`)
	dataCDataAttrRe = regexp.MustCompile(`(?i)\bdata-cdata\s*=\s*["']([^"']*)["']`)
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
		return firstSubmatch(html, pkeyAttrRe, arkosePkURLRe, arkoseAPIPathRe, publicKeyJSONRe)
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
