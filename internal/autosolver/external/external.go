// Package external provides skeleton implementations for third-party
// CAPTCHA solving services (Capsolver, 2Captcha). These are designed
// as pluggable solvers enabled via configuration.
package external

import (
	"context"
	"fmt"
	"strings"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

// externalSolver is everything the third-party providers share: the same key
// check, page read, CAPTCHA detection and sitekey extraction. Only identity and
// the API call differ, and the API call deliberately stays a per-provider
// method — the two wire protocols must not be merged when they are implemented.
type externalSolver struct {
	name string
	// label is the spelling used in operator-facing errors. 2Captcha registers
	// as "twocaptcha" but has always reported itself as "2captcha", so this is
	// not derivable from name.
	label    string
	apiKey   string
	baseURL  string
	priority int
}

func (s externalSolver) Name() string  { return s.name }
func (s externalSolver) Priority() int { return s.priority }

var captchaPatterns = []string{
	"recaptcha",
	"hcaptcha",
	"challenges.cloudflare.com/turnstile",
	"g-recaptcha",
	"h-captcha",
}

// CanHandle checks if the page contains a supported CAPTCHA type.
func (s externalSolver) CanHandle(ctx context.Context, page autosolver.Page) (bool, error) {
	if s.apiKey == "" {
		return false, nil
	}

	html, err := page.HTML()
	if err != nil {
		return false, nil
	}

	lower := strings.ToLower(html)
	for _, p := range captchaPatterns {
		if strings.Contains(lower, p) {
			return true, nil
		}
	}
	return false, nil
}

// prepare runs everything both providers do before their API call and returns
// the sitekey to submit, or the Result and error to hand straight back. The
// key check comes first so a solver with no key never touches the page.
func (s externalSolver) prepare(page autosolver.Page) (string, *autosolver.Result, error) {
	result := &autosolver.Result{SolverUsed: s.name}

	if s.apiKey == "" {
		result.Error = s.label + " API key not configured"
		return "", result, fmt.Errorf("%s API key not configured", s.label)
	}

	html, err := page.HTML()
	if err != nil {
		result.Error = fmt.Sprintf("get HTML: %v", err)
		return "", result, err
	}

	captchaType := detectCaptchaType(html)
	if captchaType == "" {
		result.Error = "no supported CAPTCHA detected"
		return "", result, fmt.Errorf("no supported CAPTCHA detected on page")
	}

	sitekey := extractSitekey(html, captchaType)
	if sitekey == "" {
		result.Error = "sitekey not found"
		return "", result, fmt.Errorf("could not extract sitekey from page")
	}

	return sitekey, result, nil
}
