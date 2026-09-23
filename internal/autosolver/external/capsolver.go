// Package external provides implementations for third-party CAPTCHA
// solving services (Capsolver, 2Captcha). These are pluggable solvers
// enabled via configuration.
package external

import (
	"net/http"
	"time"
)

// CapsolverConfig holds Capsolver API configuration.
type CapsolverConfig struct {
	APIKey  string `json:"apiKey"`
	BaseURL string `json:"baseUrl,omitempty"` // Default: https://api.capsolver.com
	// PollInterval is the getTaskResult poll cadence. Default: 3s.
	PollInterval time.Duration `json:"-"`
}

// Capsolver implements autosolver.Solver using the Capsolver API. It no longer
// solves hCaptcha or FunCaptcha: createTask refuses both as an unsupported service.
type Capsolver struct {
	provider
}

// NewCapsolver creates a Capsolver solver with the given configuration.
func NewCapsolver(cfg CapsolverConfig) *Capsolver {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.capsolver.com"
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 3 * time.Second
	}
	return &Capsolver{provider{
		name:     "capsolver",
		label:    "capsolver",
		priority: 200,
		apiKey:   cfg.APIKey,
		api: &taskAPI{
			label:        "capsolver",
			baseURL:      cfg.BaseURL,
			apiKey:       cfg.APIKey,
			pollInterval: cfg.PollInterval,
			client:       &http.Client{Timeout: 30 * time.Second},
		},
		supports: map[string]bool{"recaptcha": true, "recaptcha-v3": true, "turnstile": true},
		task:     capsolverTaskFor,
	}}
}

func capsolverTaskFor(c *captcha) any {
	taskType, _ := capsolverTaskType(c.typ)
	return capsolverTask{Type: taskType, WebsiteURL: c.url, WebsiteKey: c.key, PageAction: c.pageAction}
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
	case "turnstile":
		return "AntiTurnstileTaskProxyLess", true
	default:
		return "", false
	}
}

type capsolverTask struct {
	Type       string `json:"type"`
	WebsiteURL string `json:"websiteURL"`
	// WebsiteKey is data-sitekey, or the ?render= sitekey for reCAPTCHA v3.
	WebsiteKey string `json:"websiteKey,omitempty"`
	// PageAction is the reCAPTCHA v3 action (the value passed to
	// grecaptcha.execute). Optional — CapSolver applies a default when absent.
	PageAction string `json:"pageAction,omitempty"`
}
