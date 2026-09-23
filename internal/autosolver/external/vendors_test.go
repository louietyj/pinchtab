package external

import (
	"context"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

// hookedExecutor answers the vendor hook reads and records the delivery.
type hookedExecutor struct {
	fakeExecutor
	tencentAppID, yidunID string
}

func (e *hookedExecutor) Evaluate(ctx context.Context, expr string, result interface{}) error {
	sp, isString := result.(*string)
	switch {
	case isString && strings.Contains(expr, "__ptTencent&&"):
		*sp = e.tencentAppID
		return nil
	case isString && strings.Contains(expr, "__ptYidun&&"):
		*sp = e.yidunID
		return nil
	case isString && strings.Contains(expr, "lemin-cropped-captcha"):
		*sp = "lemin-cropped-captcha"
		return nil
	}
	return e.fakeExecutor.Evaluate(ctx, expr, result)
}

func TestTwoCaptchaSolvesTheLongTailVendors(t *testing.T) {
	for _, tc := range []struct {
		name, html, solution string
		exec                 *hookedExecutor
		want                 map[string]any
		delivered            string
	}{
		{
			name:      "yandex",
			html:      `<div class="smart-captcha" data-sitekey="FEXfAbHQsToo97VidNVk3j4dC74nGW1DgdxjtNB9"></div><script src="https://smartcaptcha.yandexcloud.net/captcha.js"></script>`,
			solution:  `{"token":"YA-TOKEN"}`,
			exec:      &hookedExecutor{},
			want:      map[string]any{"type": "YandexSmartCaptchaTaskProxyless", "websiteKey": "FEXfAbHQsToo97VidNVk3j4dC74nGW1DgdxjtNB9"},
			delivered: `smart-token`,
		},
		{
			name:      "prosopo",
			html:      `<div class="procaptcha" data-sitekey="5E7QGuzYgLJMH6qXQiCw62AUuLxPXRs1E6jitV4w8zTjDcKr"></div><script src="https://js.prosopo.io/js/procaptcha.bundle.js"></script>`,
			solution:  `{"token":"0xPROSOPO"}`,
			exec:      &hookedExecutor{},
			want:      map[string]any{"type": "ProsopoTaskProxyless", "websiteKey": "5E7QGuzYgLJMH6qXQiCw62AUuLxPXRs1E6jitV4w8zTjDcKr"},
			delivered: `procaptcha-response`,
		},
		{
			name:      "lemin",
			html:      `<div id="lemin-cropped-captcha"></div><script src="https://api.leminnow.com/captcha/v1/cropped/CROPPED_3dfdd5c_d1872b526b794d83ba3b365eb15a200b/js"></script>`,
			solution:  `{"answer":"0xaxakx","challenge_id":"e0348984"}`,
			exec:      &hookedExecutor{},
			want:      map[string]any{"type": "LeminTaskProxyless", "captchaId": "CROPPED_3dfdd5c_d1872b526b794d83ba3b365eb15a200b", "divId": "lemin-cropped-captcha"},
			delivered: `"challenge_id":"e0348984"`,
		},
		{
			name:      "tencent",
			html:      `<div id="tcaptcha_transform_dy" class="tencent-captcha__transform"></div>`,
			solution:  `{"appid":"192962660","ret":0,"ticket":"tr034","randstr":"@KVN"}`,
			exec:      &hookedExecutor{tencentAppID: "192962660"},
			want:      map[string]any{"type": "TencentTaskProxyless", "appId": "192962660"},
			delivered: `t.callback(res)`,
		},
		{
			name:      "yidun",
			html:      `<div class="yidun yidun--light"><div class="yidun_panel"></div></div>`,
			solution:  `{"token":"YIDUN-TOKEN"}`,
			exec:      &hookedExecutor{yidunID: "07e2387ab53a4d6f930b8d9a9be71bdf"},
			want:      map[string]any{"type": "YidunTaskProxyless", "websiteKey": "07e2387ab53a4d6f930b8d9a9be71bdf"},
			delivered: `NECaptchaValidate`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var task map[string]any
			srv := mockTwoCaptcha(t, tc.solution, &task)
			defer srv.Close()

			res, err := newTestTwoCaptcha(srv.URL).Solve(context.Background(), &fakePage{url: "https://ex.com", html: tc.html}, tc.exec)
			if err != nil || !res.Solved {
				t.Fatalf("Solve: err=%v error=%q", err, res.Error)
			}
			for k, v := range tc.want {
				if task[k] != v {
					t.Errorf("task[%q] = %v, want %v (task %v)", k, task[k], v, task)
				}
			}
			if !strings.Contains(tc.exec.lastInject, tc.delivered) {
				t.Errorf("delivery lacks %q: %q", tc.delivered, tc.exec.lastInject)
			}
			if ok, _ := NewCapsolver(CapsolverConfig{APIKey: "k"}).CanHandle(context.Background(), &fakePage{html: tc.html}); ok {
				t.Error("CapSolver claimed a vendor it has no task for")
			}
			if intent := autosolver.DetectChallengeIntent("", "", tc.html); intent == nil || intent.ChallengeType != tc.name {
				t.Errorf("navigate gate = %+v, want %s", intent, tc.name)
			}
		})
	}
}

// Sites load TCaptcha.js and Yidun's script on pages that never show a captcha;
// buying a token there would bill every page load.
func TestTencentAndYidunScriptsAloneAreNotCaptchas(t *testing.T) {
	for _, html := range []string{
		`<script src="https://turing.captcha.qcloud.com/TCaptcha.js"></script><button id="login">Log in</button>`,
		`<script src="https://cstaticdun.126.net/load.min.js"></script><input id="problemInput">`,
	} {
		if typ := detectCaptchaType(html); typ != "" {
			t.Errorf("detectCaptchaType(%q) = %q", html, typ)
		}
	}
}

func TestStructuredSolutionWithoutItsAnswerIsRefused(t *testing.T) {
	err := structuredInjectors["tencent"](context.Background(), &fakeExecutor{}, &captcha{}, []byte(`{"ret":0}`))
	if err == nil {
		t.Error("a Tencent solution with no ticket was delivered")
	}
}
