package external

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

// TwoCaptchaConfig holds 2Captcha API configuration.
type TwoCaptchaConfig struct {
	APIKey  string `json:"apiKey"`
	BaseURL string `json:"baseUrl,omitempty"` // Default: https://api.2captcha.com
	// PollInterval is the getTaskResult poll cadence. Default: 5s, as 2Captcha asks.
	PollInterval time.Duration `json:"-"`
}

// TwoCaptcha implements autosolver.Solver using 2Captcha's API v2. It is the
// provider for hCaptcha and FunCaptcha, which CapSolver no longer solves, and a
// fallback for the rest.
type TwoCaptcha struct {
	provider
}

// NewTwoCaptcha creates a 2Captcha solver with the given configuration.
func NewTwoCaptcha(cfg TwoCaptchaConfig) *TwoCaptcha {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.2captcha.com"
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Second
	}
	return &TwoCaptcha{provider{
		name:     autosolver.TwoCaptchaSolverName,
		label:    "2captcha",
		priority: 210,
		apiKey:   cfg.APIKey,
		api: &taskAPI{
			label:        "2captcha",
			baseURL:      cfg.BaseURL,
			apiKey:       cfg.APIKey,
			pollInterval: cfg.PollInterval,
			client:       &http.Client{Timeout: 30 * time.Second},
		},
		supports: map[string]bool{
			"recaptcha": true, "recaptcha-v3": true, "turnstile": true,
			"hcaptcha": true, "funcaptcha": true,
		},
		task:    twoCaptchaTaskFor,
		timeout: twoCaptchaSolveTimeout,
	}}
}

// twoCaptchaSolveTimeout covers a human-solved task: hCaptcha on
// accounts.hcaptcha.com/demo took 55s and 106s on consecutive tries.
const twoCaptchaSolveTimeout = 180 * time.Second

// twoCaptchaV3MinScore is the middle of the three scores 2Captcha offers (0.3,
// 0.7, 0.9): most sites gate at 0.5, and 0.9 fails more often.
const twoCaptchaV3MinScore = 0.7

func twoCaptchaTaskFor(c *captcha) any {
	task := map[string]any{"websiteURL": c.url}
	setIf := func(k, v string) {
		if v != "" {
			task[k] = v
		}
	}
	switch c.typ {
	case "recaptcha":
		task["type"] = "RecaptchaV2TaskProxyless"
		task["websiteKey"] = c.key
		if c.invisible {
			task["isInvisible"] = true
		}
		if c.enterprise {
			task["type"] = "RecaptchaV2EnterpriseTaskProxyless"
			if c.dataS != "" {
				task["enterprisePayload"] = map[string]string{"s": c.dataS}
			}
		} else {
			setIf("recaptchaDataSValue", c.dataS)
		}
	case "recaptcha-v3":
		task["type"] = "RecaptchaV3TaskProxyless"
		task["websiteKey"] = c.key
		task["minScore"] = twoCaptchaV3MinScore
		setIf("pageAction", c.pageAction)
		if c.enterprise {
			task["isEnterprise"] = true
		}
	case "turnstile":
		task["type"] = "TurnstileTaskProxyless"
		task["websiteKey"] = c.key
		setIf("action", c.turnstileAction)
		setIf("data", c.turnstileCData)
	case "hcaptcha":
		task["type"] = "HCaptchaTaskProxyless"
		task["websiteKey"] = c.key
		setIf("userAgent", c.userAgent)
	case "funcaptcha":
		task["type"] = "FunCaptchaTaskProxyless"
		task["websitePublicKey"] = c.key
		// 2Captcha wants the bare host where CapSolver took a URL.
		setIf("funcaptchaApiJSSubdomain", strings.TrimPrefix(strings.TrimPrefix(c.arkoseHost, "https://"), "http://"))
		setIf("userAgent", c.userAgent)
		if c.arkoseBlob != "" {
			// map[string]string can't fail to marshal.
			b, _ := json.Marshal(map[string]string{"blob": c.arkoseBlob})
			task["data"] = string(b)
		}
	}
	return task
}
