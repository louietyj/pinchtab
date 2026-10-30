package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// urlBridge reports a fixed current URL.
type urlBridge struct {
	mockBridge
	url string
}

func (b *urlBridge) CurrentURL(context.Context) (string, error) { return b.url, nil }

func shortStepSolveWait(t *testing.T, d time.Duration) {
	t.Helper()
	old := stepSolveWait
	stepSolveWait = d
	t.Cleanup(func() { stepSolveWait = old })
}

func TestStepWaitsForASolveThenReportsIt(t *testing.T) {
	h := &Handlers{Bridge: &urlBridge{url: "https://a.test/form"}}
	run, _ := h.autoSolve.begin("t1", "https://a.test/form", "hcaptcha")
	go func() {
		time.Sleep(50 * time.Millisecond)
		h.autoSolve.end("t1", run, autoSolveOutcome{"solved": true, "challengeType": "hcaptcha"})
	}()
	w := httptest.NewRecorder()
	report, ok := h.solveBeforeStep(context.Background(), w, "t1", true)
	if !ok || report["solved"] != true || report["beforeStep"] != true {
		t.Fatalf("report = %v, ok = %v (%s)", report, ok, w.Body)
	}
	if again, ok := h.solveBeforeStep(context.Background(), httptest.NewRecorder(), "t1", true); !ok || again != nil {
		t.Errorf("the solve was reported twice: %v", again)
	}
}

// The site submitted its form once it had the token: a click aimed at the
// form must not land on whatever the next page has in that place.
func TestStepAimedAtAPageTheSolveChangedIsRefused(t *testing.T) {
	h := &Handlers{Bridge: &urlBridge{url: "https://a.test/welcome"}}
	run, _ := h.autoSolve.begin("t1", "https://a.test/form", "hcaptcha")
	h.autoSolve.end("t1", run, autoSolveOutcome{"solved": true})

	w := httptest.NewRecorder()
	if _, ok := h.solveBeforeStep(context.Background(), w, "t1", true); ok {
		t.Fatal("step ran on the changed page")
	}
	if w.Code != http.StatusConflict || !json.Valid(w.Body.Bytes()) {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Code != "page_changed_during_solve" {
		t.Errorf("code = %q", body.Code)
	}

	// A nav goes elsewhere anyway; it runs, and carries the report.
	run, _ = h.autoSolve.begin("t1", "https://a.test/form", "hcaptcha")
	h.autoSolve.end("t1", run, autoSolveOutcome{"solved": true})
	if report, ok := h.solveBeforeStep(context.Background(), httptest.NewRecorder(), "t1", false); !ok || report == nil {
		t.Errorf("nav refused or unreported: %v %v", report, ok)
	}
}

func TestStepAnswersSolveInProgressRatherThanOutwaitTheCLI(t *testing.T) {
	shortStepSolveWait(t, 30*time.Millisecond)
	h := &Handlers{Bridge: &urlBridge{url: "https://a.test/form"}}
	h.autoSolve.begin("t1", "https://a.test/form", "hcaptcha")

	w := httptest.NewRecorder()
	if _, ok := h.solveBeforeStep(context.Background(), w, "t1", true); ok {
		t.Fatal("step ran during the solve")
	}
	var body struct {
		Code    string         `json:"code"`
		Details map[string]any `json:"details"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusConflict || body.Code != "solve_in_progress" || body.Details["tabId"] != "t1" {
		t.Errorf("got %d %s", w.Code, w.Body)
	}
}

// A solve that never finishes stalls steps once, for the budget, not on every
// step; the agent's own `wait --captcha` is not held to it.
func TestStepsStopWaitingOnASolveOnceTheBudgetIsSpent(t *testing.T) {
	shortStepSolveWait(t, time.Second)
	old := stepWaitBudget
	stepWaitBudget = 60 * time.Millisecond
	t.Cleanup(func() { stepWaitBudget = old })

	h := &Handlers{Bridge: &urlBridge{url: "https://a.test/form"}}
	run, _ := h.autoSolve.begin("tab1", "https://a.test/form", "hcaptcha")

	start := time.Now()
	w := httptest.NewRecorder()
	if _, ok := h.solveBeforeStep(context.Background(), w, "tab1", true); ok {
		t.Fatal("step ran during the solve")
	}
	if d := time.Since(start); d < 50*time.Millisecond || d > 500*time.Millisecond {
		t.Errorf("first step waited %v, want about the budget", d)
	}
	var body struct {
		Details map[string]any `json:"details"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Details["waitedOut"] != true {
		t.Errorf("budget not reported spent: %s", w.Body)
	}

	start = time.Now()
	if _, ok := h.solveBeforeStep(context.Background(), httptest.NewRecorder(), "tab1", true); ok {
		t.Fatal("step ran during the solve")
	}
	if d := time.Since(start); d > 20*time.Millisecond {
		t.Errorf("a step waited %v again after the budget was spent", d)
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		h.autoSolve.end("tab1", run, autoSolveOutcome{"solved": true})
	}()
	rec := httptest.NewRecorder()
	h.HandleAutoSolveWait(rec, httptest.NewRequest(http.MethodGet, "/autosolve?tabId=tab1&waitMs=2000&explicit=1", nil))
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["state"] != "done" {
		t.Errorf("explicit wait gave up with the budget: %v", resp)
	}
}

func TestAutoSolveWaitReportsTheSolve(t *testing.T) {
	h := &Handlers{Bridge: &urlBridge{url: "https://a.test/form"}}
	get := func() map[string]any {
		w := httptest.NewRecorder()
		h.HandleAutoSolveWait(w, httptest.NewRequest(http.MethodGet, "/autosolve?tabId=tab1&waitMs=2000", nil))
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		return resp
	}
	if resp := get(); resp["state"] != "idle" {
		t.Fatalf("no solve: %v", resp)
	}
	run, _ := h.autoSolve.begin("tab1", "https://a.test/form", "hcaptcha")
	go func() {
		time.Sleep(50 * time.Millisecond)
		h.autoSolve.end("tab1", run, autoSolveOutcome{"solved": true})
	}()
	resp := get()
	if resp["state"] != "done" || resp["pageChanged"] != false {
		t.Fatalf("after the solve: %v", resp)
	}
	if solve, _ := resp["autoSolve"].(map[string]any); solve["solved"] != true {
		t.Errorf("outcome missing: %v", resp)
	}
	if resp := get(); resp["state"] != "idle" {
		t.Errorf("reported twice: %v", resp)
	}
}
