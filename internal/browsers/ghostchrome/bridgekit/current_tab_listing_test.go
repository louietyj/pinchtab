package bridgekit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/config"
	"github.com/pinchtab/pinchtab/internal/handlers"
)

type listedChromeBridge struct {
	*bridge.Bridge
	targets []bridge.TabTarget
}

func (b listedChromeBridge) ListTargets() ([]bridge.TabTarget, error) { return b.targets, nil }

func (b listedChromeBridge) EnsureBrowser(*config.RuntimeConfig) error { return nil }

func TestTheTabListingThroughTheAdapterLeadsWithTheCurrentTab(t *testing.T) {
	ctx, cancel := chromedp.NewContext(context.Background())
	t.Cleanup(cancel)
	cfg := &config.RuntimeConfig{StateDir: t.TempDir()}
	chrome := listedChromeBridge{Bridge: bridge.New(context.Background(), ctx, cfg)}
	for _, id := range []string{"tabA", "tabB", "tabC"} {
		chrome.RegisterTab(id, ctx)
		chrome.targets = append(chrome.targets, bridge.TabTarget{TargetID: id, URL: "https://example.com/" + id, Type: "page"})
	}
	chrome.RegisterTab("tabB", ctx)
	adapter := NewBridgeAdapter(chrome, cfg)

	_, resolved, err := adapter.TabContext("")
	if err != nil || resolved != "tabB" {
		t.Fatalf("adapter TabContext(\"\") = %q, %v; the reader must agree with it", resolved, err)
	}

	w := httptest.NewRecorder()
	handlers.New(adapter, cfg, nil, nil, nil).HandleTabs(w, httptest.NewRequest(http.MethodGet, "/tabs", nil))

	var resp struct {
		Tabs []struct {
			ID string `json:"id"`
		} `json:"tabs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || len(resp.Tabs) != 3 {
		t.Fatalf("GET /tabs = %d %s", w.Code, w.Body.String())
	}
	if resp.Tabs[0].ID != "tabB" {
		t.Fatalf("GET /tabs through the ghost-chrome adapter leads with %s, want the current tab tabB", resp.Tabs[0].ID)
	}
}
