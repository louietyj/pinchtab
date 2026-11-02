package external

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

func newTestNoneCap(url string) *NoneCap {
	return NewNoneCap(NoneCapConfig{APIKey: "k", BaseURL: url, PollInterval: time.Millisecond})
}

// NoneCap's compat endpoint answers with every vendor's token field at once.
func TestNoneCapSolvesHCaptchaThroughItsCompatEndpoint(t *testing.T) {
	var task map[string]any
	srv := mockTwoCaptcha(t, `{"gRecaptchaResponse":"P1_TOKEN","token":"P1_TOKEN","respKey":"E0_KEY","userAgent":"UA"}`, &task)
	defer srv.Close()

	page := &fakePage{url: "https://www.ebay.com/splashui/captcha", html: `<iframe src="https://newassets.hcaptcha.com/captcha/v1/x/static/hcaptcha.html#frame=checkbox&amp;sitekey=195eeb9f-8f50-4a9c-abfc-a78ceaa3cdde"></iframe>`}
	exec := &fakeExecutor{userAgent: "Mozilla/5.0 (TestUA)"}
	res, err := newTestNoneCap(srv.URL).Solve(context.Background(), page, exec)
	if err != nil || !res.Solved {
		t.Fatalf("Solve: err=%v solved=%v error=%q", err, res.Solved, res.Error)
	}
	if task["type"] != "HCaptchaTaskProxyless" || task["websiteKey"] != "195eeb9f-8f50-4a9c-abfc-a78ceaa3cdde" || task["userAgent"] != "Mozilla/5.0 (TestUA)" {
		t.Errorf("task = %v", task)
	}
	if !strings.Contains(exec.lastInject, `"P1_TOKEN","E0_KEY"`) {
		t.Errorf("token and respKey not injected: %q", exec.lastInject)
	}
}

func TestNoneCapTakesOnlyHCaptcha(t *testing.T) {
	page := &fakePage{url: "https://ex.com", html: `<div class="g-recaptcha" data-sitekey="6LcABC"></div>`}
	ok, err := newTestNoneCap("http://unused").CanHandle(context.Background(), page)
	if err != nil || ok {
		t.Errorf("CanHandle(reCAPTCHA) = %v, %v", ok, err)
	}
}

// A solve NoneCap gives up on is not billed, so the chain must hand on to the
// next solver rather than stop as after a paid failure.
func TestNoneCapFailureHandsOn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/createTask") {
			_, _ = w.Write([]byte(`{"errorId":0,"taskId":"solve_1","status":"idle"}`))
			return
		}
		_, _ = w.Write([]byte(`{"errorId":1,"errorCode":"ERROR_CAPTCHA_UNSOLVABLE","errorDescription":"token_not_granted"}`))
	}))
	defer srv.Close()

	page := &fakePage{url: "https://accounts.hcaptcha.com/demo", html: `<div class="h-captcha" data-sitekey="a5f74b19-9e45-40e0-b45d-47ff91b7a6c2"></div>`}
	_, err := newTestNoneCap(srv.URL).Solve(context.Background(), page, &fakeExecutor{})
	if err == nil || errors.Is(err, autosolver.ErrSpent) || errors.Is(err, autosolver.ErrPermanent) {
		t.Errorf("err = %v, want a plain failure the chain moves past", err)
	}
}
