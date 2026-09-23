package external

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

// The reported solver identity has to be the registered one. Solve now builds it
// from Name(), which makes drift impossible rather than merely detectable; this
// test reds if either one goes back to spelling the name a second time.
func TestSolverUsedMatchesTheRegisteredName(t *testing.T) {
	for _, solver := range []autosolver.Solver{
		NewCapsolver(CapsolverConfig{}),
		NewTwoCaptcha(TwoCaptchaConfig{}),
	} {
		t.Run(solver.Name(), func(t *testing.T) {
			// An unset API key is the earliest return, and it is enough: the
			// Result is built before the check.
			result, err := solver.Solve(context.Background(), nil, nil)
			if err == nil {
				t.Fatal("expected the unset-API-key error, so this test exercises the real Solve path")
			}
			if result == nil {
				t.Fatal("Solve returned no Result to check")
			}
			if result.SolverUsed != solver.Name() {
				t.Errorf("Result.SolverUsed = %q but Name() = %q — the reported solver identity has drifted from the registered one", result.SolverUsed, solver.Name())
			}
		})
	}
}

// stubPage is the smallest Page the external solvers touch: they read HTML and
// URL and nothing else.
type stubPage struct {
	html    string
	htmlErr error
}

func (p stubPage) URL() string   { return "https://example.test/challenge" }
func (p stubPage) Title() string { return "" }
func (p stubPage) HTML() (string, error) {
	return p.html, p.htmlErr
}
func (p stubPage) HTMLWithin(time.Duration) (string, error) { return p.HTML() }
func (p stubPage) Screenshot() ([]byte, error)              { return nil, nil }

const recaptchaHTML = `<div class="g-recaptcha" data-sitekey="6LtestKEY"></div>`

// Every branch of Solve is pinned by exact string, because folding the shared
// prologue into a base moved these messages across a file boundary and the
// providers spell their own label differently from their registered name.
func TestSolveMessagesAreUnchangedOnEveryPath(t *testing.T) {
	for _, provider := range []struct {
		label   string
		build   func(key string) autosolver.Solver
		wantPri int
	}{
		{"capsolver", func(key string) autosolver.Solver { return NewCapsolver(CapsolverConfig{APIKey: key}) }, 200},
		{"2captcha", func(key string) autosolver.Solver { return NewTwoCaptcha(TwoCaptchaConfig{APIKey: key}) }, 210},
	} {
		t.Run(provider.label, func(t *testing.T) {
			keyed := provider.build("key")

			type solveCase struct {
				name       string
				solver     autosolver.Solver
				page       autosolver.Page
				wantResult string
				wantErr    string
			}

			cases := []solveCase{
				{
					name:       "no api key",
					solver:     provider.build(""),
					page:       stubPage{html: recaptchaHTML},
					wantResult: provider.label + " API key not configured",
					wantErr:    provider.label + " API key not configured",
				},
				{
					name:       "page read fails",
					solver:     keyed,
					page:       stubPage{htmlErr: errors.New("boom")},
					wantResult: "get HTML: boom",
					wantErr:    "boom",
				},
				{
					name:       "no captcha on page",
					solver:     keyed,
					page:       stubPage{html: "<html><body>nothing here</body></html>"},
					wantResult: "no supported CAPTCHA detected",
					wantErr:    "no supported CAPTCHA detected on page",
				},
				{
					name:       "captcha without a sitekey",
					solver:     keyed,
					page:       stubPage{html: `<div class="g-recaptcha"></div>`},
					wantResult: "sitekey not found",
					wantErr:    "could not extract sitekey/public key from page",
				},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					result, err := tc.solver.Solve(context.Background(), tc.page, nil)
					if err == nil {
						t.Fatal("Solve reported success; every path here is an error path")
					}
					if err.Error() != tc.wantErr {
						t.Errorf("error = %q, want %q", err, tc.wantErr)
					}
					if result == nil {
						t.Fatal("Solve returned no Result")
					}
					if result.Error != tc.wantResult {
						t.Errorf("Result.Error = %q, want %q", result.Error, tc.wantResult)
					}
					if result.SolverUsed != tc.solver.Name() {
						t.Errorf("Result.SolverUsed = %q, want %q", result.SolverUsed, tc.solver.Name())
					}
				})
			}

			if got := keyed.Priority(); got != provider.wantPri {
				t.Errorf("Priority() = %d, want %d; the external stubs must keep sorting behind the real solvers", got, provider.wantPri)
			}
		})
	}
}

func TestCanHandleIsSharedAndKeyGated(t *testing.T) {
	for _, tc := range []struct {
		name   string
		solver autosolver.Solver
		page   autosolver.Page
		want   bool
	}{
		{"no key never handles", NewCapsolver(CapsolverConfig{}), stubPage{html: recaptchaHTML}, false},
		{"keyed handles a captcha page", NewCapsolver(CapsolverConfig{APIKey: "k"}), stubPage{html: recaptchaHTML}, true},
		{"keyed declines a plain page", NewCapsolver(CapsolverConfig{APIKey: "k"}), stubPage{html: "<p>hi</p>"}, false},
		{"page read failure declines", NewCapsolver(CapsolverConfig{APIKey: "k"}), stubPage{htmlErr: errors.New("boom")}, false},
		{"twocaptcha agrees with capsolver", NewTwoCaptcha(TwoCaptchaConfig{APIKey: "k"}), stubPage{html: recaptchaHTML}, true},
		{"twocaptcha declines a page that only names a vendor", NewTwoCaptcha(TwoCaptchaConfig{APIKey: "k"}), stubPage{html: "<p>solve any recaptcha</p>"}, false},
		{"capsolver declines hcaptcha", NewCapsolver(CapsolverConfig{APIKey: "k"}), stubPage{html: `<div class="h-captcha" data-sitekey="x"></div>`}, false},
		{"twocaptcha takes hcaptcha", NewTwoCaptcha(TwoCaptchaConfig{APIKey: "k"}), stubPage{html: `<div class="h-captcha" data-sitekey="x"></div>`}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.solver.CanHandle(context.Background(), tc.page)
			if err != nil {
				t.Fatalf("CanHandle() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("CanHandle() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExtractSitekeyReadsBothQuoteStyles(t *testing.T) {
	for _, tc := range []struct{ name, html, want string }{
		{"double quotes", `<div data-sitekey="abc"></div>`, "abc"},
		{"single quotes", `<div data-sitekey='abc'></div>`, "abc"},
		{"absent", `<div class="g-recaptcha"></div>`, ""},
		{"unterminated", `<div data-sitekey="abc`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractSitekey(tc.html, "recaptcha"); got != tc.want {
				t.Errorf("extractSitekey(%q) = %q, want %q", tc.html, got, tc.want)
			}
		})
	}
}

// CanHandle must accept exactly the pages Solve goes on to submit: a detected
// type the provider solves. Anything else promises a solve that Solve refuses.
func TestCanHandleAndDetectCaptchaTypeAgreeOnEveryPage(t *testing.T) {
	var corpus []string
	for _, marker := range []string{
		"recaptcha", "g-recaptcha", "hcaptcha", "h-captcha",
		"challenges.cloudflare.com/turnstile", "turnstile", "captcha", "funcaptcha",
		"arkoselabs", "geetest", "",
	} {
		for _, shape := range []string{
			`<div class="%s"></div>`,
			`<script src="https://%s/api.js"></script>`,
			`<p>%s</p>`,
			`<DIV CLASS="%s">`,
		} {
			corpus = append(corpus, fmt.Sprintf(shape, marker), strings.ToUpper(fmt.Sprintf(shape, marker)))
		}
	}
	corpus = append(corpus, "", "<html></html>", "<p>nothing here</p>")

	for _, p := range []*provider{
		&NewCapsolver(CapsolverConfig{APIKey: "k"}).provider,
		&NewTwoCaptcha(TwoCaptchaConfig{APIKey: "k"}).provider,
	} {
		var accepted, declined int
		for _, html := range corpus {
			canHandle, err := p.CanHandle(context.Background(), stubPage{html: html})
			if err != nil {
				t.Fatalf("CanHandle(%q) error = %v", html, err)
			}
			typ := detectCaptchaType(html)
			if want := typ != "" && p.supports[typ]; canHandle != want {
				t.Errorf("%s: CanHandle(%q) = %v, but detected %q which it supports=%v", p.label, html, canHandle, typ, want)
			}
			if canHandle {
				accepted++
			} else {
				declined++
			}
		}
		if accepted == 0 || declined == 0 {
			t.Fatalf("%s: corpus exercised only one outcome (accepted=%d declined=%d); the agreement claim is vacuous unless both arms occur", p.label, accepted, declined)
		}
	}
}
