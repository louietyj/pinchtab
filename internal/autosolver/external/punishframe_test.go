package external

import (
	"context"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

// punishFrameExecutor is an item page under AliExpress's punish iframe: frame
// reads answer from the inner reCAPTCHA frame, and delivering the token lifts
// the overlay.
type punishFrameExecutor struct {
	fakeExecutor
	frameURL, anchor string
	hasCallback      bool
	delivered        string
	lifted           bool
}

func (e *punishFrameExecutor) Frames(context.Context) ([]autosolver.FrameRef, error) {
	return []autosolver.FrameRef{{ID: "top", URL: "https://www.aliexpress.us/item/1.html"}, {ID: "inner", ParentID: "outer", URL: e.frameURL}}, nil
}

func (e *punishFrameExecutor) EvaluateInFrame(_ context.Context, frameID, expr string, result any) error {
	if frameID != "inner" {
		return errNoFrameAccess
	}
	switch r := result.(type) {
	case *struct{ Href, Anchor string }:
		r.Href, r.Anchor = e.frameURL, e.anchor
	case *bool:
		*r = e.hasCallback
		if e.hasCallback {
			e.delivered = expr
			e.lifted = true
		}
	}
	return nil
}

func (e *punishFrameExecutor) Evaluate(ctx context.Context, expr string, result interface{}) error {
	if expr == punishFrameGoneJS {
		*result.(*bool) = e.lifted
		return nil
	}
	return e.fakeExecutor.Evaluate(ctx, expr, result)
}

const (
	punishItemHTML = `<div id="root"></div><iframe src="https://recom-acs.aliexpress.us:443//h5/mtop.relationrecommend.aliexpressrecommend.recommend/1.0/_____tmd_____/punish?x5secdata=abc"></iframe>`
	punishInnerURL = "https://recom-acs.aliexpress.us/h5/mtop.relationrecommend.aliexpressrecommend.recommend/1.0/_____tmd_____/punish?recaptcha=1&iframe=1&x5step=3&x5secdata=abc"
	punishAnchor   = "https://www.google.com/recaptcha/enterprise/anchor?ar=1&k=6LcsZOwpAAAAAFfDsdu7pUv7GeN-Asc1Lzeo5LYP&co=aHR0cHM6Ly9yZWNvbS1hY3MuYWxpZXhwcmVzcy51czo0NDM.&hl=en&v=x&size=normal&sa=Ab8FoKfjyPupWZQXwvYp9oBmXvHSYGNDvN2ghbwpv0ak&cb=1"
)

func TestPunishFrameIsSolvedAsEnterpriseInsideTheFrame(t *testing.T) {
	var task map[string]any
	srv := mockTwoCaptcha(t, `{"gRecaptchaResponse":"03AFcWeA-TOKEN"}`, &task)
	defer srv.Close()

	exec := &punishFrameExecutor{frameURL: punishInnerURL, anchor: punishAnchor, hasCallback: true}
	page := &fakePage{url: "https://www.aliexpress.us/item/3256812469467260.html", html: punishItemHTML}
	res, err := newTestTwoCaptcha(srv.URL).Solve(context.Background(), page, exec)
	if err != nil || !res.Solved {
		t.Fatalf("Solve = %+v, %v", res, err)
	}
	for k, want := range map[string]any{
		"type":       "RecaptchaV2EnterpriseTaskProxyless",
		"websiteKey": "6LcsZOwpAAAAAFfDsdu7pUv7GeN-Asc1Lzeo5LYP",
		"websiteURL": punishInnerURL,
		"pageAction": "Ab8FoKfjyPupWZQXwvYp9oBmXvHSYGNDvN2ghbwpv0ak",
	} {
		if task[k] != want {
			t.Errorf("task[%s] = %v, want %v", k, task[k], want)
		}
	}
	if !strings.Contains(exec.delivered, "__recaptchaValidateCB__") || !strings.Contains(exec.delivered, `"03AFcWeA-TOKEN"`) {
		t.Errorf("delivered %q", exec.delivered)
	}
}

func TestPunishFrameCapsolverTaskIsEnterpriseWithTheAction(t *testing.T) {
	task := capsolverTaskFor(&captcha{typ: "punish-frame", key: "k", url: punishInnerURL, enterprise: true, pageAction: "sa"}).(capsolverTask)
	if task.Type != "ReCaptchaV2EnterpriseTaskProxyLess" || task.PageAction != "sa" || task.WebsiteURL != punishInnerURL {
		t.Errorf("task = %+v", task)
	}
}

func TestPunishFrameWithoutItsCallbackIsNotReportedSolved(t *testing.T) {
	var task map[string]any
	srv := mockTwoCaptcha(t, `{"gRecaptchaResponse":"T"}`, &task)
	defer srv.Close()
	exec := &punishFrameExecutor{frameURL: punishInnerURL, anchor: punishAnchor}
	page := &fakePage{url: "https://www.aliexpress.us/item/1.html", html: punishItemHTML}
	if res, _ := newTestTwoCaptcha(srv.URL).Solve(context.Background(), page, exec); res.Solved {
		t.Error("reported solved with nowhere to deliver the token")
	}
}
