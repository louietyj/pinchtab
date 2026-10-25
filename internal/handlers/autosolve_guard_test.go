package handlers

import "testing"

func TestAutoSolveTabsRunsOneSolvePerTab(t *testing.T) {
	var tabs autoSolveTabs
	if !tabs.begin("t1") {
		t.Fatal("first solve on a tab was refused")
	}
	if tabs.begin("t1") {
		t.Error("a second solve started while the first was running")
	}
	if !tabs.begin("t2") {
		t.Error("another tab's solve was blocked")
	}
	tabs.end("t1")
	if !tabs.begin("t1") {
		t.Error("tab stayed claimed after its solve ended")
	}
}

func TestAutoSolveTabsRemembersASolvedChallengeUntilNavigation(t *testing.T) {
	var tabs autoSolveTabs
	if tabs.recentlySolved("t1", "https://a.test/form", "recaptcha-v2") {
		t.Fatal("unsolved challenge reported solved")
	}
	tabs.markSolved("t1", "https://a.test/form", "recaptcha-v2")
	if !tabs.recentlySolved("t1", "https://a.test/form", "recaptcha-v2") {
		t.Error("solved challenge not remembered: the next action would buy another token")
	}
	if tabs.recentlySolved("t1", "https://a.test/other", "recaptcha-v2") {
		t.Error("a challenge on another page was treated as already solved")
	}
	if tabs.recentlySolved("t1", "https://a.test/form", "hcaptcha") {
		t.Error("a different challenge type was treated as already solved")
	}
	tabs.forget("t1")
	if tabs.recentlySolved("t1", "https://a.test/form", "recaptcha-v2") {
		t.Error("navigation did not clear the solved challenge")
	}
}

// A v3 page is reported once: every fill and click re-detects it.
func TestLeaveV3ToBrowserReportsOncePerPage(t *testing.T) {
	h := &Handlers{}
	out := h.leaveV3ToBrowser("t1", "https://a.test/login")
	if out == nil || out["leftToBrowser"] != true || out["solved"] != false {
		t.Fatalf("first report = %v, want leftToBrowser and not solved", out)
	}
	if out := h.leaveV3ToBrowser("t1", "https://a.test/login"); out != nil {
		t.Errorf("reported again on the same page: %v", out)
	}
	if h.leaveV3ToBrowser("t1", "https://a.test/signup") == nil {
		t.Error("a v3 on another page was not reported")
	}
	h.autoSolve.forget("t1")
	if h.leaveV3ToBrowser("t1", "https://a.test/signup") == nil {
		t.Error("navigating back to a v3 page did not report it again")
	}
}
