package handlers

import (
	"testing"
	"time"
)

// A refused challenge gets one retry, reported pending until it runs, and
// dropped when the tab navigates.
func TestScheduledRetryIsOnceAndHoldsTheTab(t *testing.T) {
	var g autoSolveTabs
	url := "https://www.aliexpress.us//item/1.html/_____tmd_____/punish?x5step=1"
	if !g.scheduleRetry("tab", url, time.Now().Add(time.Minute)) {
		t.Fatal("first retry refused")
	}
	if left, ok := g.retryPending("tab"); !ok || left <= 0 {
		t.Errorf("retry not pending: %v %v", left, ok)
	}
	if g.scheduleRetry("tab", url, time.Now().Add(time.Minute)) {
		t.Error("a second retry was scheduled for the same challenge")
	}
	if !g.retryDue("tab", url) {
		t.Error("retry not due before any navigation")
	}
	g.forget("tab")
	if g.retryDue("tab", url) {
		t.Error("retry still due after the tab navigated")
	}
}

// Once the solver has given up, later actions report that instead of dragging
// again; a navigation starts afresh.
func TestGivingUpSticksUntilNavigation(t *testing.T) {
	var g autoSolveTabs
	final := autoSolveOutcome{"solved": false, "error": "slider refused the drag (error MOCK1)"}
	g.markGaveUp("tab", "nocaptcha", final)
	if out, ok := g.gaveUpOn("tab", "nocaptcha"); !ok || out["error"] != final["error"] {
		t.Errorf("gave-up outcome not kept: %v %v", out, ok)
	}
	if _, ok := g.gaveUpOn("tab", "punish-frame"); ok {
		t.Error("a different challenge on the tab counts as given up")
	}
	g.forget("tab")
	if _, ok := g.gaveUpOn("tab", "nocaptcha"); ok {
		t.Error("still given up after the tab navigated")
	}
}
