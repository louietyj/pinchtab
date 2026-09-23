// Package external provides implementations for third-party CAPTCHA
// solving services (Capsolver, 2Captcha). These are pluggable solvers
// enabled via configuration.
package external

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
	api    *taskAPI
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
		api: &taskAPI{
			label:        "capsolver",
			baseURL:      cfg.BaseURL,
			apiKey:       cfg.APIKey,
			pollInterval: cfg.PollInterval,
			client:       &http.Client{Timeout: 30 * time.Second},
		},
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
		return result, autosolver.Spent(err)
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

type capsolverSolution struct {
	GRecaptchaResponse string `json:"gRecaptchaResponse"`
	Token              string `json:"token"`
}

// solveRemote runs a prebuilt task and returns its token.
func (c *Capsolver) solveRemote(ctx context.Context, task capsolverTask) (string, error) {
	raw, err := c.api.solve(ctx, task)
	if err != nil {
		return "", err
	}
	var sol capsolverSolution
	if err := json.Unmarshal(raw, &sol); err != nil {
		return "", fmt.Errorf("capsolver decode solution: %w", err)
	}
	if tok := pickToken(sol); tok != "" {
		return tok, nil
	}
	return "", fmt.Errorf("capsolver returned ready with empty token")
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
