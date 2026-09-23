package external

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

// provider is a token-solving service reached through the task API. The
// providers differ only in which CAPTCHA types they solve and how they spell
// the task for one; detection, the API call and injection are shared.
type provider struct {
	name string
	// label is the spelling used in operator-facing errors. 2Captcha registers
	// as "twocaptcha" but has always reported itself as "2captcha".
	label    string
	priority int
	apiKey   string
	api      *taskAPI
	supports map[string]bool
	task     func(c *captcha) any
	// timeout, when set, is the least a solve needs; see SolveTimeoutHinter.
	timeout time.Duration
}

// captcha is a detected challenge and everything a task may be built from.
type captcha struct {
	typ, key, url string
	// pageAction is the reCAPTCHA v3 action passed to grecaptcha.execute.
	pageAction string
	// enterprise marks reCAPTCHA Enterprise; invisible a v2 widget with no
	// checkbox; dataS the v2 widget's data-s.
	enterprise, invisible bool
	dataS                 string
	// turnstileAction and turnstileCData are the widget's data-action/data-cdata.
	turnstileAction, turnstileCData string
	// arkoseHost and arkoseBlob come from the page, preferring what the
	// bridge's document-start hook captured over static HTML.
	arkoseHost, arkoseBlob string
	// userAgent is the browser's: Arkose and hCaptcha bind the token to it.
	userAgent string
	aws       awsWAFPage
}

type tokenSolution struct {
	GRecaptchaResponse string `json:"gRecaptchaResponse"`
	Token              string `json:"token"`
	// RespKey is hCaptcha's second value, read by sites through hcaptcha.getRespKey.
	RespKey string `json:"respKey"`
	// Cookie is AWS WAF's answer: the aws-waf-token value.
	Cookie string `json:"cookie"`
}

// token returns the solve token regardless of which field the provider used.
func (s tokenSolution) token() string {
	switch {
	case s.GRecaptchaResponse != "":
		return s.GRecaptchaResponse
	case s.Token != "":
		return s.Token
	}
	return s.Cookie
}

func (p *provider) Name() string  { return p.name }
func (p *provider) Priority() int { return p.priority }

func (p *provider) SolveTimeout() time.Duration { return p.timeout }

// CanHandle reports whether the page carries a CAPTCHA this provider solves.
func (p *provider) CanHandle(_ context.Context, page autosolver.Page) (bool, error) {
	if p.apiKey == "" {
		return false, nil
	}
	html, err := page.HTML()
	if err != nil {
		return false, nil
	}
	return p.supports[detectCaptchaType(html)], nil
}

// Solve submits the page's CAPTCHA to the provider, then injects the returned
// token and fires the challenge callback where applicable.
func (p *provider) Solve(ctx context.Context, page autosolver.Page, executor autosolver.ActionExecutor) (*autosolver.Result, error) {
	result := &autosolver.Result{SolverUsed: p.name}
	fail := func(msg string, err error) (*autosolver.Result, error) {
		result.Error = msg
		return result, err
	}

	if p.apiKey == "" {
		return fail(p.label+" API key not configured", fmt.Errorf("%s API key not configured", p.label))
	}

	html, err := page.HTML()
	if err != nil {
		return fail(fmt.Sprintf("get HTML: %v", err), err)
	}

	typ := detectCaptchaType(html)
	if typ == "" {
		return fail("no supported CAPTCHA detected", fmt.Errorf("no supported CAPTCHA detected on page"))
	}
	if !p.supports[typ] {
		msg := fmt.Sprintf("%s does not solve %s", p.label, typ)
		return fail(msg, autosolver.Permanent(errors.New(msg)))
	}

	key := extractSitekey(html, typ)
	if key == "" {
		switch typ {
		case "turnstile":
			key = readTurnstileSitekey(ctx, executor)
		case "mtcaptcha":
			key = readMTCaptchaSitekey(ctx, executor)
		}
	}
	// AWS WAF has no sitekey: the page URL alone identifies it.
	if key == "" && typ != "awswaf" {
		return fail("sitekey not found", fmt.Errorf("could not extract sitekey/public key from page"))
	}

	c := readCaptcha(ctx, executor, html, typ, key, page.URL())
	raw, err := p.api.solve(ctx, p.task(c))
	if err != nil {
		return fail(err.Error(), err)
	}
	var sol tokenSolution
	if err := json.Unmarshal(raw, &sol); err != nil {
		err = autosolver.Spent(fmt.Errorf("%s decode solution: %w", p.label, err))
		return fail(err.Error(), err)
	}
	if sol.token() == "" {
		err := autosolver.Spent(fmt.Errorf("%s returned ready with empty token", p.label))
		return fail(err.Error(), err)
	}

	if typ == "awswaf" {
		err = injectAWSWAFCookie(ctx, executor, page, sol.token(), c.aws.CookieDomains)
	} else {
		err = injectToken(ctx, executor, typ, sol)
	}
	if err != nil {
		return fail(fmt.Sprintf("inject token: %v", err), autosolver.Spent(err))
	}

	result.Solved = true
	result.FinalTitle = page.Title()
	result.FinalURL = page.URL()
	return result, nil
}

// readCaptcha gathers the task inputs beyond the key, reading the live page
// where static HTML is not enough.
func readCaptcha(ctx context.Context, executor autosolver.ActionExecutor, html, typ, key, url string) *captcha {
	c := &captcha{typ: typ, key: key, url: url}
	switch typ {
	case "recaptcha":
		c.enterprise = isRecaptchaEnterprise(html)
		c.invisible = isRecaptchaInvisible(html)
		c.dataS = widgetAttr(html, "g-recaptcha", dataSAttrRe)
	case "recaptcha-v3":
		c.enterprise = isRecaptchaEnterprise(html)
		c.pageAction = extractRecaptchaAction(html)
	case "turnstile":
		c.turnstileAction = widgetAttr(html, "cf-turnstile", dataActionRe)
		c.turnstileCData = widgetAttr(html, "cf-turnstile", dataCDataAttrRe)
	case "funcaptcha":
		c.arkoseHost = extractArkoseSubdomain(html)
		if ac := readArkoseCapture(ctx, executor); ac != nil {
			if ac.PK != "" {
				c.key = ac.PK
			}
			if ac.Surl != "" {
				c.arkoseHost = ac.Surl
			}
			c.arkoseBlob = ac.Blob
		}
		c.userAgent = readUserAgent(ctx, executor)
	case "hcaptcha":
		c.userAgent = readUserAgent(ctx, executor)
	case "awswaf":
		c.aws = readAWSWAFPage(html)
	}
	return c
}
