package bridge

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func TestResourceBlockingFor(t *testing.T) {
	for _, tc := range []struct {
		ads, images, media bool
		wantTypes          []network.ResourceType
		wantMedia          bool
	}{
		{},
		{images: true, wantTypes: []network.ResourceType{network.ResourceTypeImage}},
		{media: true, wantTypes: []network.ResourceType{network.ResourceTypeImage, network.ResourceTypeMedia}, wantMedia: true},
		{images: true, media: true, wantTypes: []network.ResourceType{network.ResourceTypeImage, network.ResourceTypeMedia}, wantMedia: true},
		{ads: true},
	} {
		rb := ResourceBlockingFor(tc.ads, tc.images, tc.media)
		if got := rb.resourceTypes(); !reflect.DeepEqual(got, tc.wantTypes) {
			t.Errorf("%+v: types = %v, want %v", tc, got, tc.wantTypes)
		}
		if rb.Media != tc.wantMedia {
			t.Errorf("%+v: Media = %v, want %v", tc, rb.Media, tc.wantMedia)
		}
		if (len(rb.URLPatterns) > 0) != tc.ads {
			t.Errorf("%+v: URLPatterns = %d entries", tc, len(rb.URLPatterns))
		}
		if rb.IsZero() != (!tc.ads && !tc.images && !tc.media) {
			t.Errorf("%+v: IsZero = %v", tc, rb.IsZero())
		}
	}
}

func TestBlockedTypePatterns(t *testing.T) {
	types := []network.ResourceType{network.ResourceTypeImage, network.ResourceTypeMedia}
	got := blockedTypePatterns(types, false)
	want := []*fetch.RequestPattern{
		{URLPattern: "*", ResourceType: network.ResourceTypeImage},
		{URLPattern: "*", ResourceType: network.ResourceTypeMedia},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("without proxy auth: %+v, want %+v", got, want)
	}
	// Chrome raises proxy auth challenges only for intercepted requests.
	if got := blockedTypePatterns(types, true); len(got) != 1 || got[0].URLPattern != "*" || got[0].ResourceType != "" {
		t.Errorf("with proxy auth: %+v, want one catch-all pattern", got)
	}
}

func TestRouteManager_Match_BlockedTypeAbortsAheadOfRules(t *testing.T) {
	rm := NewRouteManager(nil)
	rm.mu.Lock()
	rm.perTab["tab1"] = &tabRouteState{
		rules:      []RouteRule{{Pattern: "*", Action: RouteActionContinue}},
		blockTypes: []network.ResourceType{network.ResourceTypeImage},
	}
	rm.mu.Unlock()

	// Event resource types arrive lowercased from the listener.
	rule, matched, _ := rm.match("tab1", "https://cdn.example/img?w=800&fm=webp", "image", "GET")
	if !matched || rule.Action != RouteActionAbort {
		t.Errorf("image: got %+v matched=%v, want abort", rule, matched)
	}
	rule, matched, _ = rm.match("tab1", "https://cdn.example/app.js", "script", "GET")
	if !matched || rule.Action != RouteActionContinue {
		t.Errorf("script: got %+v matched=%v, want the continue rule", rule, matched)
	}
}

func TestRouteManager_Match_BlockedTypesWithoutRulesStillDispatch(t *testing.T) {
	rm := NewRouteManager(nil)
	rm.mu.Lock()
	rm.perTab["tab1"] = &tabRouteState{blockTypes: []network.ResourceType{network.ResourceTypeMedia}}
	rm.mu.Unlock()

	if rule, matched, has := rm.match("tab1", "https://v.example/stream", "media", "GET"); !has || !matched || rule.Action != RouteActionAbort {
		t.Errorf("media: got %+v matched=%v has=%v, want abort", rule, matched, has)
	}
	if _, matched, has := rm.match("tab1", "https://v.example/doc", "document", "GET"); !has || matched {
		t.Errorf("document: matched=%v has=%v, want an unmatched continue", matched, has)
	}
}

// Clearing user routes must not clear blocking: it is not a route.
func TestRouteManager_RemoveAllRulesKeepsBlockedTypes(t *testing.T) {
	rm := NewRouteManager(nil)
	rm.mu.Lock()
	rm.perTab["tab1"] = &tabRouteState{
		rules:      []RouteRule{{Pattern: "tracker.io", Action: RouteActionAbort}},
		blockTypes: []network.ResourceType{network.ResourceTypeImage},
	}
	rm.mu.Unlock()

	if _, err := rm.Remove(t.Context(), "tab1", ""); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(rm.List("tab1")) != 0 {
		t.Errorf("rules should be gone")
	}
	if _, matched, _ := rm.match("tab1", "https://x/y.png", "image", "GET"); !matched {
		t.Errorf("image blocking should survive removing every rule")
	}
}

func TestRouteManager_SetResourceBlockingOffOnUnblockedTabIsANoOp(t *testing.T) {
	rm := NewRouteManager(nil)
	// No browser here, so clearing the blocked URLs errors; the tab state is the point.
	_ = rm.SetResourceBlocking(t.Context(), "tab1", ResourceBlocking{})
	rm.mu.Lock()
	_, ok := rm.perTab["tab1"]
	rm.mu.Unlock()
	if ok {
		t.Error("turning blocking off must not create tab state")
	}
}

// A failed enable must leave no state, listener or pause suppression behind:
// a suppressed proxy-auth listener would leave every paused request hanging.
func TestRouteManager_SetResourceBlockingFailedEnableRollsBack(t *testing.T) {
	rm := NewRouteManager(nil)
	var calls []string
	rm.SetFetchAuthCoordination(
		func() bool { return false },
		func(tabID string, v bool) { calls = append(calls, fmt.Sprintf("%s=%v", tabID, v)) },
	)
	ctx, cancel := chromedp.NewContext(context.Background())
	cancel()

	if err := rm.SetResourceBlocking(ctx, "tab1", ResourceBlockingFor(false, true, false)); err == nil {
		t.Fatal("expected failure on a dead chromedp context")
	}
	rm.mu.Lock()
	_, ok := rm.perTab["tab1"]
	rm.mu.Unlock()
	if ok {
		t.Error("failed blocking should leave no tab state")
	}
	if len(calls) != 2 || calls[0] != "tab1=true" || calls[1] != "tab1=false" {
		t.Fatalf("expected suppress-then-rollback, got %v", calls)
	}
}
