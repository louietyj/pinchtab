package external

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mockTwoCaptcha answers createTask with a numeric taskId, as 2Captcha does,
// and the first poll with solution; it records the task it was sent.
func mockTwoCaptcha(t *testing.T, solution string, gotTask *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/createTask"):
			var req struct {
				Task map[string]any `json:"task"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			*gotTask = req.Task
			_, _ = w.Write([]byte(`{"errorId":0,"taskId":83925302505}`))
		case strings.HasSuffix(r.URL.Path, "/getTaskResult"):
			_, _ = w.Write([]byte(`{"errorId":0,"status":"ready","solution":` + solution + `}`))
		default:
			http.Error(w, "not found", 404)
		}
	}))
}

func newTestTwoCaptcha(url string) *TwoCaptcha {
	return NewTwoCaptcha(TwoCaptchaConfig{APIKey: "k", BaseURL: url, PollInterval: time.Millisecond})
}

func TestTwoCaptchaSolvesHCaptchaUnderTheBrowserUserAgent(t *testing.T) {
	var task map[string]any
	srv := mockTwoCaptcha(t, `{"token":"P1_TOKEN","gRecaptchaResponse":"P1_TOKEN","respKey":"W1_KEY"}`, &task)
	defer srv.Close()

	page := &fakePage{url: "https://accounts.hcaptcha.com/demo", html: `<div class="h-captcha" data-sitekey="a5f74b19-9e45-40e0-b45d-47ff91b7a6c2"></div>`}
	exec := &fakeExecutor{userAgent: "Mozilla/5.0 (TestUA)"}
	res, err := newTestTwoCaptcha(srv.URL).Solve(context.Background(), page, exec)
	if err != nil || !res.Solved {
		t.Fatalf("Solve: err=%v solved=%v error=%q", err, res.Solved, res.Error)
	}
	if task["type"] != "HCaptchaTaskProxyless" || task["websiteKey"] != "a5f74b19-9e45-40e0-b45d-47ff91b7a6c2" {
		t.Errorf("task = %v", task)
	}
	if task["userAgent"] != "Mozilla/5.0 (TestUA)" {
		t.Errorf("browser UA not forwarded: %v", task["userAgent"])
	}
	if !strings.Contains(exec.lastInject, `"P1_TOKEN","W1_KEY"`) {
		t.Errorf("token and respKey not injected: %q", exec.lastInject)
	}
}

func TestTwoCaptchaRecaptchaV3SendsTheRequiredMinScore(t *testing.T) {
	var task map[string]any
	srv := mockTwoCaptcha(t, `{"gRecaptchaResponse":"V3"}`, &task)
	defer srv.Close()

	page := &fakePage{url: "https://ex.com", html: `<script src="https://www.google.com/recaptcha/api.js?render=6Lc_V3"></script><script>grecaptcha.execute('6Lc_V3', {action: 'login'});</script>`}
	if _, err := newTestTwoCaptcha(srv.URL).Solve(context.Background(), page, &fakeExecutor{}); err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if task["type"] != "RecaptchaV3TaskProxyless" || task["pageAction"] != "login" || task["minScore"] != twoCaptchaV3MinScore {
		t.Errorf("task = %v", task)
	}
}

func TestSolveFuncaptcha(t *testing.T) {
	var task map[string]any
	srv := mockTwoCaptcha(t, `{"token":"FC-TOKEN"}`, &task)
	defer srv.Close()

	c := newTestTwoCaptcha(srv.URL)
	page := &fakePage{
		url:  "https://www.linkedin.com/checkpoint",
		html: `<div data-pkey="0152B4EB-D2DC-460A-89A1-629838B529C9"></div><script src="https://lnkd-api.arkoselabs.com/v2/api.js"></script>`,
	}
	exec := &fakeExecutor{userAgent: "Mozilla/5.0 (TestUA)"}

	res, err := c.Solve(context.Background(), page, exec)
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if !res.Solved {
		t.Fatalf("expected solved, got error=%q", res.Error)
	}
	if task["type"] != "FunCaptchaTaskProxyless" {
		t.Errorf("task type = %v", task["type"])
	}
	if task["userAgent"] != "Mozilla/5.0 (TestUA)" {
		t.Errorf("browser UA should be forwarded; got %v", task["userAgent"])
	}
	if task["websitePublicKey"] != "0152B4EB-D2DC-460A-89A1-629838B529C9" {
		t.Errorf("websitePublicKey = %v", task["websitePublicKey"])
	}
	if _, ok := task["websiteKey"]; ok {
		t.Errorf("funcaptcha task should not set websiteKey")
	}
	if task["funcaptchaApiJSSubdomain"] != "lnkd-api.arkoselabs.com" {
		t.Errorf("funcaptchaApiJSSubdomain = %v, want the bare host", task["funcaptchaApiJSSubdomain"])
	}
	if _, ok := task["data"]; ok {
		t.Errorf("no blob captured, expected no data, got %v", task["data"])
	}
	if !strings.Contains(exec.lastInject, "FC-TOKEN") {
		t.Errorf("token not injected: %q", exec.lastInject)
	}
}

// TestSolveFuncaptchaWithBlob verifies the document-start hook's captured
// blob/pk/surl (window.__ptArkose) override static extraction and are forwarded
// as the task data.
func TestSolveFuncaptchaWithBlob(t *testing.T) {
	var task map[string]any
	srv := mockTwoCaptcha(t, `{"token":"FC-TOKEN"}`, &task)
	defer srv.Close()

	c := newTestTwoCaptcha(srv.URL)
	page := &fakePage{
		url:  "https://www.linkedin.com/checkpoint",
		html: `<div data-pkey="STATIC-PK"></div><script src="https://lnkd-api.arkoselabs.com/v2/api.js"></script>`,
	}
	exec := &fakeExecutor{arkoseJSON: `{"blob":"BDA-BLOB-XYZ","pk":"LIVE-PK","surl":"https://lnkd-api.arkoselabs.com"}`}

	res, err := c.Solve(context.Background(), page, exec)
	if err != nil || !res.Solved {
		t.Fatalf("Solve: err=%v solved=%v error=%q", err, res.Solved, res.Error)
	}
	if task["websitePublicKey"] != "LIVE-PK" {
		t.Errorf("captured pk should override static; got %v", task["websitePublicKey"])
	}
	// Assert the JSON shape, not just substring — a wrong wrapper key must fail.
	data, _ := task["data"].(string)
	var dataObj map[string]string
	if err := json.Unmarshal([]byte(data), &dataObj); err != nil {
		t.Fatalf("task data is not valid JSON: %q (%v)", data, err)
	}
	if dataObj["blob"] != "BDA-BLOB-XYZ" {
		t.Errorf("expected data.blob=BDA-BLOB-XYZ, got %+v", dataObj)
	}
}

func TestTwoCaptchaTaskCarriesEnterpriseAndTurnstileData(t *testing.T) {
	for _, tc := range []struct {
		name, html string
		want       map[string]any
	}{
		{"v2 enterprise", `<script src="https://www.google.com/recaptcha/enterprise.js"></script><div class="g-recaptcha" data-sitekey="6LfE" data-s="S"></div>`,
			map[string]any{"type": "RecaptchaV2EnterpriseTaskProxyless", "enterprisePayload": map[string]any{"s": "S"}}},
		{"v3 enterprise", `<script src="https://www.google.com/recaptcha/enterprise.js?render=6Lel"></script>`,
			map[string]any{"type": "RecaptchaV3TaskProxyless", "isEnterprise": true}},
		{"turnstile", `<div class="cf-turnstile" data-sitekey="0x4AAA" data-action="login" data-cdata="CD"></div>`,
			map[string]any{"type": "TurnstileTaskProxyless", "action": "login", "data": "CD"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var task map[string]any
			srv := mockTwoCaptcha(t, `{"token":"T"}`, &task)
			defer srv.Close()
			if _, err := newTestTwoCaptcha(srv.URL).Solve(context.Background(), &fakePage{url: "https://ex.com", html: tc.html}, &fakeExecutor{}); err != nil {
				t.Fatalf("Solve: %v", err)
			}
			for k, v := range tc.want {
				got, _ := json.Marshal(task[k])
				want, _ := json.Marshal(v)
				if string(got) != string(want) {
					t.Errorf("task[%q] = %s, want %s (task %v)", k, got, want, task)
				}
			}
		})
	}
}
