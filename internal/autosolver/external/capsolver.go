// Package external provides implementations for third-party CAPTCHA
// solving services (Capsolver, 2Captcha). These are pluggable solvers
// enabled via configuration.
package external

import (
	"net/http"
	"strings"
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
		supports: map[string]bool{"recaptcha": true, "recaptcha-v3": true, "turnstile": true, "mtcaptcha": true, "awswaf": true},
		task:     capsolverTaskFor,
	}}
}

func capsolverTaskFor(c *captcha) any {
	taskType, _ := capsolverTaskType(c.typ)
	task := capsolverTask{Type: taskType, WebsiteURL: c.url, WebsiteKey: c.key, PageAction: c.pageAction}
	switch c.typ {
	case "recaptcha", "recaptcha-v3":
		if c.enterprise {
			// ReCaptchaV2TaskProxyLess -> ReCaptchaV2EnterpriseTaskProxyLess, and v3 alike.
			task.Type = strings.Replace(task.Type, "TaskProxyLess", "EnterpriseTaskProxyLess", 1)
		}
		task.IsInvisible = c.invisible
		if c.dataS != "" {
			if c.enterprise {
				task.EnterprisePayload = map[string]string{"s": c.dataS}
			} else {
				task.RecaptchaDataSValue = c.dataS
			}
		}
	case "awswaf":
		task.AwsKey, task.AwsIv, task.AwsContext = c.aws.Key, c.aws.IV, c.aws.Context
		task.AwsChallengeJS = c.aws.ChallengeJS
	case "turnstile":
		if c.turnstileAction != "" || c.turnstileCData != "" {
			task.Metadata = map[string]string{}
			if c.turnstileAction != "" {
				task.Metadata["action"] = c.turnstileAction
			}
			if c.turnstileCData != "" {
				task.Metadata["cdata"] = c.turnstileCData
			}
		}
	}
	return task
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
	case "mtcaptcha":
		return "MtCaptchaTaskProxyLess", true
	case "awswaf":
		return "AntiAwsWafTaskProxyLess", true
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
	PageAction          string            `json:"pageAction,omitempty"`
	IsInvisible         bool              `json:"isInvisible,omitempty"`
	RecaptchaDataSValue string            `json:"recaptchaDataSValue,omitempty"`
	EnterprisePayload   map[string]string `json:"enterprisePayload,omitempty"`
	// Metadata carries Turnstile's action and cdata.
	Metadata map[string]string `json:"metadata,omitempty"`
	// AWS WAF: window.gokuProps and the challenge.js URL, read fresh each load.
	AwsKey         string `json:"awsKey,omitempty"`
	AwsIv          string `json:"awsIv,omitempty"`
	AwsContext     string `json:"awsContext,omitempty"`
	AwsChallengeJS string `json:"awsChallengeJS,omitempty"`
}
