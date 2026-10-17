package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/config"
	"github.com/pinchtab/pinchtab/internal/httpx/httpxtest"
)

type routeStubBridge struct{ *mockBridge }

func (routeStubBridge) RemoveRouteRule(string, string) (int, error) { return 0, nil }

func (routeStubBridge) ListRouteRules(string) ([]bridge.RouteRule, error) { return nil, nil }

func TestEveryOptionalBodySiteDecodesTheSameWay(t *testing.T) {
	sites := []struct {
		name    string
		method  string
		target  string
		payload string
		strict  bool
	}{
		{"tab handoff", http.MethodPost, "/tabs/tab1/handoff", `{"reason":"captcha"}`, false},
		{"tab resume", http.MethodPost, "/tabs/tab1/resume", `{"status":"done"}`, false},
		{"solve", http.MethodPost, "/solve", `{"maxAttempts":1}`, false},
		{"tab close", http.MethodPost, "/tabs/tab1/close", `{"tabId":"tab1"}`, false},
		{"close", http.MethodPost, "/close", `{"tabId":"tab1"}`, false},
		{"network unroute", http.MethodDelete, "/network/route", `{"pattern":"*.png"}`, false},
		{"path tab id body adapter", http.MethodPost, "/tabs/tab1/action", `{"kind":"click","ref":"e1"}`, false},
		{"tab pdf", http.MethodPost, "/tabs/tab1/pdf", `{"landscape":true}`, false},
		{"record stop", http.MethodPost, "/record/stop", `{"discard":true}`, true},
	}
	for _, site := range sites {
		for _, c := range httpxtest.OptionalBodyCases() {
			t.Run(site.name+"/"+c.Name, func(t *testing.T) {
				h := New(routeStubBridge{&mockBridge{}}, &config.RuntimeConfig{AllowScreencast: true, AllowNetworkIntercept: true}, nil, nil, nil)
				mux := http.NewServeMux()
				h.RegisterRoutes(mux, nil)
				w := httptest.NewRecorder()

				mux.ServeHTTP(w, c.Request(site.method, site.target, site.payload))

				c.Check(t, site.strict, w)
			})
		}
	}
}
