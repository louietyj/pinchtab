package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/config"
)

func unknownLengthRequest(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.ContentLength = -1
	req.SetPathValue("id", "tab1")
	return req
}

func TestAnUnknownLengthUnrouteBodyRemovesOnlyItsPattern(t *testing.T) {
	b := newRouteMockBridge()
	h := newRouteHandler(b)
	for _, pattern := range []string{"api", "img"} {
		if err := b.AddRouteRule("tab1", bridge.RouteRule{Pattern: pattern, Action: "abort"}); err != nil {
			t.Fatal(err)
		}
	}
	w := httptest.NewRecorder()
	h.HandleTabNetworkUnroute(w, unknownLengthRequest(http.MethodDelete, "/tabs/tab1/network/route", `{"pattern":"api"}`))
	var resp struct {
		Removed int `json:"removed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || w.Code != http.StatusOK || resp.Removed != 1 {
		t.Fatalf("%d %s, want one rule removed", w.Code, w.Body.String())
	}
	if rules := b.rules["tab1"]; len(rules) != 1 || rules[0].Pattern != "img" {
		t.Fatalf("remaining rules = %+v, want only img", rules)
	}
}

func TestAnEmptyUnknownLengthUnrouteBodyIsAbsent(t *testing.T) {
	b := newRouteMockBridge()
	h := newRouteHandler(b)
	if err := b.AddRouteRule("tab1", bridge.RouteRule{Pattern: "api", Action: "abort"}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.HandleTabNetworkUnroute(w, unknownLengthRequest(http.MethodDelete, "/tabs/tab1/network/route", ""))
	requireClearedRoutes(t, w, 1)
}

func TestAnUnknownLengthBodyTabIDIsCheckedAgainstThePath(t *testing.T) {
	for name, serve := range map[string]func(*Handlers, http.ResponseWriter, *http.Request){
		"pdf":   (*Handlers).HandleTabPDF,
		"close": (*Handlers).HandleTabClose,
	} {
		t.Run(name, func(t *testing.T) {
			h := New(&mockBridge{failTab: true}, &config.RuntimeConfig{}, nil, nil, nil)
			w := httptest.NewRecorder()
			serve(h, w, unknownLengthRequest(http.MethodPost, "/tabs/tab1/"+name, `{"tabId":"OTHER"}`))
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "does not match") {
				t.Fatalf("%d %s, want 400 for a body tabId that does not match the path", w.Code, w.Body.String())
			}

			w = httptest.NewRecorder()
			serve(h, w, unknownLengthRequest(http.MethodPost, "/tabs/tab1/"+name, ""))
			if w.Code == http.StatusBadRequest {
				t.Fatalf("an empty unknown-length body answered 400: %s", w.Body.String())
			}
		})
	}
}
