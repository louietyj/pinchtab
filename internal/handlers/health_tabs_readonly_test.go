package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/config"
	"github.com/pinchtab/pinchtab/internal/session"
)

type listedBridge struct {
	*bridge.Bridge
	targets []bridge.TabTarget
}

func (b listedBridge) ListTargets() ([]bridge.TabTarget, error) { return b.targets, nil }

func (b listedBridge) EnsureBrowser(*config.RuntimeConfig) error { return nil }

type tabsListingFixture struct {
	b *bridge.Bridge
	h *Handlers
}

const listingSession = "ses_listing"

func newTabsListingFixture(t *testing.T) *tabsListingFixture {
	t.Helper()
	ctx, cancel := chromedp.NewContext(context.Background())
	t.Cleanup(cancel)
	cfg := &config.RuntimeConfig{StateDir: t.TempDir()}
	b := bridge.New(context.Background(), ctx, cfg)
	for _, id := range []string{"tabA", "tabB", "tabC"} {
		b.RegisterTab(id, ctx)
		time.Sleep(2 * time.Millisecond)
	}
	b.RecordTabScope("tabB", sessionScope().key, true)
	lb := listedBridge{Bridge: b}
	for _, id := range []string{"tabA", "tabB", "tabC"} {
		lb.targets = append(lb.targets, bridge.TabTarget{TargetID: id, URL: "https://example.com/" + id, Type: "page"})
	}
	h := New(lb, cfg, nil, nil, nil)
	h.CurrentTabs.Set(sessionScope(), "tabB")
	return &tabsListingFixture{b: b, h: h}
}

func sessionScope() currentTabScope {
	return scopedCurrentTab(currentTabScopeSession, listingSession)
}

func (f *tabsListingFixture) list(t *testing.T, scoped bool) []string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/tabs", nil)
	if scoped {
		req = session.WithSession(req, &session.Session{ID: listingSession})
	}
	w := httptest.NewRecorder()
	f.h.HandleTabs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /tabs = %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Tabs []struct {
			ID string `json:"id"`
		} `json:"tabs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(resp.Tabs))
	for _, tab := range resp.Tabs {
		ids = append(ids, tab.ID)
	}
	return ids
}

type tabsListingState struct {
	LastUsed      map[string]time.Time
	CurrentTab    string
	Accessed      map[string]bool
	OnlyByCreator []string
	CurrentTabs   map[string]string
}

func (f *tabsListingFixture) state() tabsListingState {
	st := tabsListingState{LastUsed: map[string]time.Time{}, CurrentTabs: map[string]string{}}
	for _, id := range []string{"tabA", "tabB", "tabC"} {
		st.LastUsed[id], _ = f.b.TabLastUsed(id)
	}
	st.CurrentTab = f.b.CurrentTabID()
	st.Accessed = f.b.AccessedTabIDs()
	st.OnlyByCreator = f.b.TabsOnlyUsedByCreator(sessionScope().key)
	f.h.CurrentTabs.mu.Lock()
	for key, entry := range f.h.CurrentTabs.entries {
		st.CurrentTabs[key] = entry.tabID
	}
	f.h.CurrentTabs.mu.Unlock()
	return st
}

func TestHandleTabsListingChangesNoTabState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		scoped bool
		first  string
	}{
		{"unidentified caller", false, "tabC"},
		{"session-scoped caller", true, "tabB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTabsListingFixture(t)
			before := f.state()

			ids := f.list(t, tc.scoped)

			if after := f.state(); !reflect.DeepEqual(before, after) {
				t.Fatalf("GET /tabs changed tab state:\nbefore %+v\nafter  %+v", before, after)
			}
			if len(ids) != 3 || ids[0] != tc.first {
				t.Fatalf("listing = %v, want %s first", ids, tc.first)
			}
		})
	}
}

func TestHandleTabsUnidentifiedListingKeepsASessionTabOnlyUsedByItsCreator(t *testing.T) {
	f := newTabsListingFixture(t)
	f.b.RecordTabScope("tabC", sessionScope().key, true)

	f.list(t, false)

	got := f.b.TabsOnlyUsedByCreator(sessionScope().key)
	if !reflect.DeepEqual(got, []string{"tabB", "tabC"}) {
		t.Fatalf("tabs only used by the session = %v, want [tabB tabC]", got)
	}
}

func TestHandleTabsSessionWhoseTabClosedGetsNoCurrentAndKeepsItsPointer(t *testing.T) {
	f := newTabsListingFixture(t)
	f.h.CurrentTabs.Set(sessionScope(), "gone")

	ids := f.list(t, true)

	if !reflect.DeepEqual(ids, []string{"tabA", "tabB", "tabC"}) {
		t.Fatalf("listing = %v, want the unmarked target order", ids)
	}
	if got, ok := f.h.CurrentTabs.Get(sessionScope()); !ok || got != "gone" {
		t.Fatalf("session pointer = %q %v, want gone kept", got, ok)
	}
}
