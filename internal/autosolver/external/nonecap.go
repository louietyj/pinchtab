package external

import (
	"net/http"
	"time"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

// NoneCapConfig holds NoneCap API configuration.
type NoneCapConfig struct {
	APIKey  string `json:"apiKey"`
	BaseURL string `json:"baseUrl,omitempty"` // Default: its createTask endpoint
	// PollInterval is the getTaskResult poll cadence. Default: 2s.
	PollInterval time.Duration `json:"-"`
}

// NoneCap implements autosolver.Solver for hCaptcha only, through the
// createTask/getTaskResult endpoint NoneCap serves for CapSolver clients. It
// solves without a human queue and bills no failed solve, so it goes before
// 2Captcha, which takes over when it fails.
type NoneCap struct {
	provider
}

// noneCapSolveTimeout leaves 2Captcha room inside a 5-minute solve run: a
// poll cut short counts as spent and ends the run, while NoneCap's own failure
// (well inside its 90 s blocking wait) hands on.
const noneCapSolveTimeout = 100 * time.Second

// NewNoneCap creates a NoneCap solver with the given configuration.
func NewNoneCap(cfg NoneCapConfig) *NoneCap {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.nonecap.com/compat/capsolver"
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	return &NoneCap{provider{
		name:     autosolver.NoneCapSolverName,
		label:    "nonecap",
		priority: 205,
		apiKey:   cfg.APIKey,
		api: &taskAPI{
			label:        "nonecap",
			baseURL:      cfg.BaseURL,
			apiKey:       cfg.APIKey,
			pollInterval: cfg.PollInterval,
			client:       &http.Client{Timeout: 30 * time.Second},
		},
		supports: map[string]bool{"hcaptcha": true},
		task:     twoCaptchaTaskFor,
		timeout:  noneCapSolveTimeout,
	}}
}
