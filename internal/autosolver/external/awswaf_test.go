package external

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

const awsCaptchaPage = `<html><head><title>Human Verification</title>
<script type="text/javascript">
window.awsWafCookieDomainList = ['hiring.amazon.ca','auth.hiring.amazon.com'];
window.gokuProps = {
"key":"AQIDAHjcYu",
"iv":"D570ZgDdSQ",
"context":"9bLoUNN+iK"
};
</script>
<script src="https://00480ef49626.c1439e2f.us-west-1.token.awswaf.com/00480ef49626/2d23d0e4897d/07353deb5ad1/challenge.js"></script>
<script src="https://00480ef49626.c1439e2f.us-west-1.captcha.awswaf.com/00480ef49626/2d23d0e4897d/07353deb5ad1/captcha.js"></script>
</head><body><div id="captcha-container"></div></body></html>`

// reloadPage serves pages in turn, advancing on each Navigate: the captcha page
// first, then whatever the reload returns.
type reloadPage struct {
	fakeExecutor
	pages     []string
	navigated []string
	cookie    string
}

func (p *reloadPage) URL() string   { return "https://hiring.amazon.ca/application/api" }
func (p *reloadPage) Title() string { return "" }
func (p *reloadPage) HTML() (string, error) {
	return p.pages[min(len(p.navigated), len(p.pages)-1)], nil
}
func (p *reloadPage) HTMLWithin(time.Duration) (string, error) { return p.HTML() }
func (p *reloadPage) Screenshot() ([]byte, error)              { return nil, nil }
func (p *reloadPage) Navigate(_ context.Context, url string) error {
	p.navigated = append(p.navigated, url)
	return nil
}
func (p *reloadPage) Evaluate(ctx context.Context, expr string, result interface{}) error {
	if strings.Contains(expr, "document.cookie=") {
		p.cookie = expr
	}
	return p.fakeExecutor.Evaluate(ctx, expr, result)
}

func TestSolveAWSWAFSetsTheTokenCookieAndReloads(t *testing.T) {
	var task capsolverTask
	srv := mockCapsolver(t, "unused", &task)
	defer srv.Close()
	// The mock answers in gRecaptchaResponse; AWS WAF's real field is cookie,
	// which tokenSolution reads as a fallback, so either proves the path.
	page := &reloadPage{pages: []string{awsCaptchaPage, `{"message":"Method Not Allowed"}`}}

	res, err := NewCapsolver(CapsolverConfig{APIKey: "k", BaseURL: srv.URL}).Solve(context.Background(), page, page)
	if err != nil || !res.Solved {
		t.Fatalf("Solve: err=%v error=%q", err, res.Error)
	}
	if task.Type != "AntiAwsWafTaskProxyLess" || task.AwsKey != "AQIDAHjcYu" || task.AwsIv != "D570ZgDdSQ" || task.AwsContext != "9bLoUNN+iK" {
		t.Errorf("task = %+v", task)
	}
	if !strings.HasSuffix(task.AwsChallengeJS, "/challenge.js") || task.WebsiteKey != "" {
		t.Errorf("task = %+v", task)
	}
	if !strings.Contains(page.cookie, "aws-waf-token=unused") || !strings.Contains(page.cookie, "domain=hiring.amazon.ca") {
		t.Errorf("cookie write = %q, want the token scoped like the SDK scopes it", page.cookie)
	}
	if len(page.navigated) != 1 {
		t.Errorf("navigated %v, want one reload", page.navigated)
	}
}

func TestSolveAWSWAFReportsARejectedTokenAsSpent(t *testing.T) {
	var task capsolverTask
	srv := mockCapsolver(t, "tok", &task)
	defer srv.Close()
	page := &reloadPage{pages: []string{awsCaptchaPage}}

	_, err := NewCapsolver(CapsolverConfig{APIKey: "k", BaseURL: srv.URL}).Solve(context.Background(), page, page)
	if !errors.Is(err, autosolver.ErrSpent) || !strings.Contains(err.Error(), "still serves the captcha") {
		t.Errorf("err = %v, want a spent rejection", err)
	}
}

func TestAWSWAFChallengeOnlyPageIsNotACaptcha(t *testing.T) {
	html := `<script>window.gokuProps = {"key":"k"};</script><script src="https://x.token.awswaf.com/a/b/challenge.js"></script>`
	if typ := detectCaptchaType(html); typ != "" {
		t.Errorf("detectCaptchaType = %q; a silent challenge clears itself and must not be bought", typ)
	}
	if intent := autosolver.DetectChallengeIntent("", "", awsCaptchaPage); intent == nil || intent.ChallengeType != "awswaf" {
		t.Errorf("DetectChallengeIntent(captcha page) = %+v, want awswaf", intent)
	}
}
