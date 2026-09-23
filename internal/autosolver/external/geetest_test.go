package external

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

// geetestExecutor answers the hook read and records the delivery.
type geetestExecutor struct {
	fakeExecutor
	hooked string
}

func (e *geetestExecutor) Evaluate(ctx context.Context, expr string, result interface{}) error {
	if strings.Contains(expr, "window.__ptGeetest?") {
		if sp, ok := result.(*string); ok {
			*sp = e.hooked
		}
		return nil
	}
	return e.fakeExecutor.Evaluate(ctx, expr, result)
}

// mockGeetestCapsolver records the task and answers with solution.
func mockGeetestCapsolver(t *testing.T, solution string, gotTask *capsolverTask) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req capsolverCreateRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		*gotTask = req.Task
		_, _ = w.Write([]byte(`{"errorId":0,"status":"ready","taskId":"t","solution":` + solution + `}`))
	}))
}

// 2Captcha's v3 demo, as the DOM serves it: a refreshed widget leaves two
// get.php requests, and only the later challenge is still valid.
const geetestV3Page = `<script src="https://static.geetest.com/static/js/fullpage.9.2.0.js"></script>
<script src="https://api.geetest.com/get.php?gt=81388ea1fc187e0c335c0a8907ff2625&amp;challenge=00000000000000000000000000000000&amp;lang=en"></script>
<script src="https://api.geetest.com/get.php?gt=81388ea1fc187e0c335c0a8907ff2625&amp;challenge=85d6caba0b788a1614e58ede40fc74cd&amp;lang=en"></script>`

// Live, both providers refuse a v3 challenge the widget has already spent
// ("challenge cannot be submitted more than once"), so none is bought.
func TestSolveGeetestV3RefusesTheSpentChallengeWithoutCallingTheAPI(t *testing.T) {
	var task capsolverTask
	srv := mockGeetestCapsolver(t, `{}`, &task)
	defer srv.Close()

	_, err := NewCapsolver(CapsolverConfig{APIKey: "k", BaseURL: srv.URL}).Solve(context.Background(), &fakePage{url: "https://2captcha.com/demo/geetest", html: geetestV3Page}, &geetestExecutor{})
	if !errors.Is(err, autosolver.ErrPermanent) {
		t.Errorf("err = %v, want a permanent refusal", err)
	}
	if task.Type != "" {
		t.Errorf("submitted %+v for a spent challenge", task)
	}
	if p := readGeetest(context.Background(), &geetestExecutor{}, geetestV3Page); p.challenge != "85d6caba0b788a1614e58ede40fc74cd" {
		t.Errorf("challenge = %q, want the most recent", p.challenge)
	}
}

func TestGeetestV3DeliversThroughTheWidgetAndTheForm(t *testing.T) {
	exec := &geetestExecutor{}
	if err := injectGeetest(context.Background(), exec, 3, json.RawMessage(`{"challenge":"c","validate":"VAL","seccode":"VAL|jordan"}`)); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"geetest_validate":"VAL"`, "o.getValidate=", "__ptGeetestSuccess", `input[name="'+k+'"]`} {
		if !strings.Contains(exec.lastInject, want) {
			t.Errorf("delivery lacks %s: %q", want, exec.lastInject)
		}
	}
}

func TestTwoCaptchaGeetestV4PrefersTheHookedCaptchaID(t *testing.T) {
	var task map[string]any
	srv := mockTwoCaptcha(t, `{"captcha_id":"54088bb07d2df3c46b79f80300b0abbe","lot_number":"L","pass_token":"P","gen_time":"1","captcha_output":"O"}`, &task)
	defer srv.Close()

	exec := &geetestExecutor{hooked: `{"version":4,"captchaId":"54088bb07d2df3c46b79f80300b0abbe"}`}
	page := &fakePage{url: "https://nopecha.com/captcha/geetest", html: `<script src="https://static.geetest.com/v4/gt4.js"></script>`}
	res, err := newTestTwoCaptcha(srv.URL).Solve(context.Background(), page, exec)
	if err != nil || !res.Solved {
		t.Fatalf("Solve: err=%v error=%q", err, res.Error)
	}
	init, _ := task["initParameters"].(map[string]any)
	if task["type"] != "GeeTestTaskProxyless" || task["version"] != float64(4) || init["captcha_id"] != "54088bb07d2df3c46b79f80300b0abbe" {
		t.Errorf("task = %v", task)
	}
	if !strings.Contains(exec.lastInject, `"pass_token":"P"`) {
		t.Errorf("v4 validate not delivered: %q", exec.lastInject)
	}
}

func TestGeetestSolutionWithoutValidateIsSpent(t *testing.T) {
	err := injectGeetest(context.Background(), &fakeExecutor{}, 3, json.RawMessage(`{"challenge":"c"}`))
	if err == nil {
		t.Fatal("a v3 solution with no validate was delivered")
	}
}

func TestDetectGeetestAtTheNavigateGate(t *testing.T) {
	if intent := autosolver.DetectChallengeIntent("Login", "https://example.com", geetestV3Page); intent == nil || intent.ChallengeType != "geetest" {
		t.Errorf("DetectChallengeIntent = %+v, want geetest", intent)
	}
	if typ := detectCaptchaType(`<a href="/demo/geetest">GeeTest demo</a>`); typ != "" {
		t.Errorf("a link naming GeeTest was classified %q", typ)
	}
}
