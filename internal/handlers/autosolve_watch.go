package handlers

import (
	"context"
	"log/slog"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"time"

	coreautosolver "github.com/pinchtab/pinchtab/internal/autosolver"
	"github.com/pinchtab/pinchtab/internal/autosolver/adapters"
	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/httpx"
)

// A widget that asks nothing yet (invisible hCaptcha or reCAPTCHA, a puzzle
// that opens on submit) is not solved on sight: the gate drops it. The tab's
// watcher looks again every watchPoll and starts the solve the moment a puzzle
// shows, and every step on the tab waits for a solve under way before it runs.
var (
	watchPoll     = time.Second
	watchLifetime = 30 * time.Minute
	// watchGoneAfter polls with no vendor frame end the watch: the widget left.
	watchGoneAfter = 3
	// lingerPoll is how often GET /autosolve?lingerMs looks while a batch waits
	// for a puzzle its last step may have opened.
	lingerPoll = 250 * time.Millisecond
	// stepSolveWait is how long a step waits on a solve before answering
	// solve_in_progress; the CLI then waits on GET /autosolve and sends the step
	// again. It stays well under the CLI's 60s request timeout.
	stepSolveWait = 20 * time.Second
	// autoSolveWaitMax caps one GET /autosolve wait for the same reason.
	autoSolveWaitMax = 40 * time.Second
	maxLinger        = 10 * time.Second
)

const widgetCheckTimeout = 2 * time.Second

// watchDormantWidgets starts tabID's watcher when the tab carries a gated
// widget's frames.
func (h *Handlers) watchDormantWidgets(tabID string) {
	if h.Bridge == nil || !h.shouldAutoSolve(autoSolverTriggerAction) || h.autoSolve.isWatching(tabID) {
		return
	}
	showing, frames, err := h.widgetsOnTab(tabID)
	if err != nil || frames == 0 {
		return
	}
	if !h.autoSolve.startWatching(tabID) {
		return
	}
	go h.watchTab(tabID, showing != nil)
}

func (h *Handlers) widgetsOnTab(tabID string) (showing *coreautosolver.WidgetFrame, frames int, err error) {
	page, _, err := adapters.NewFromBridge(h.Bridge, tabID)
	if err != nil {
		return nil, 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), widgetCheckTimeout)
	defer cancel()
	showing, _, frames, err = coreautosolver.WidgetsOnPage(ctx, page)
	return showing, frames, err
}

// watchTab starts a solve each time a widget on tabID goes from asking nothing
// to showing something, until the widget leaves or the tab closes.
func (h *Handlers) watchTab(tabID string, wasShowing bool) {
	defer h.autoSolve.stopWatching(tabID)
	deadline := time.Now().Add(watchLifetime)
	gone := 0
	for time.Now().Before(deadline) {
		time.Sleep(watchPoll)
		if _, _, err := h.Bridge.TabContext(tabID); err != nil {
			return
		}
		showing, frames, err := h.widgetsOnTab(tabID)
		if err != nil {
			continue
		}
		if frames == 0 {
			if gone++; gone >= watchGoneAfter {
				return
			}
			continue
		}
		gone = 0
		if showing != nil && !wasShowing && h.autoSolve.running(tabID) == nil {
			slog.Info("autosolver: a widget on the tab opened a puzzle", "tab_id", tabID, "frame", showing.URL)
			h.startWatchedSolve(tabID)
		}
		wasShowing = showing != nil
	}
}

// startWatchedSolve runs a solve on tabID with no caller waiting; the next step
// on the tab, or GET /autosolve, reports it. The channel closes when it is over.
func (h *Handlers) startWatchedSolve(tabID string) <-chan struct{} {
	over := make(chan struct{})
	go func() {
		defer close(over)
		ctx, cancel := context.WithTimeout(context.Background(), h.autoTriggerBudget())
		defer cancel()
		if _, err := h.autoSolverRunner(ctx, tabID); err != nil {
			slog.Warn("autosolver: watched solve failed", "tab_id", tabID, "error", err)
		}
	}()
	return over
}

// solveBeforeStep holds a step on tabID back while a solve runs there, and
// hands back a finished solve no caller has been told about. A step that would
// act on a page the solve changed (the site submitted its form once it had the
// token) is refused: it was aimed at the page before.
func (h *Handlers) solveBeforeStep(ctx context.Context, w http.ResponseWriter, tabID string, refuseIfChanged bool) (autoSolveOutcome, bool) {
	run := h.autoSolve.running(tabID)
	// A puzzle can open between two watcher polls, right before this step:
	// look now rather than let the step land on it.
	if run == nil && h.autoSolve.isWatching(tabID) {
		run = h.solveShowingPuzzle(ctx, tabID, time.Now().Add(stepSolveWait))
	}
	if run != nil {
		select {
		case <-run.done:
		case <-time.After(stepSolveWait):
			httpx.ErrorCode(w, http.StatusConflict, "solve_in_progress",
				"pinchtab is solving a "+challengeName(run.challengeType)+" challenge on this tab; the step waits for it",
				true, map[string]any{"tabId": tabID, "elapsedMs": time.Since(run.started).Milliseconds()})
			return nil, false
		}
	}
	fin := h.autoSolve.takeFinished(tabID)
	if fin == nil {
		return nil, true
	}
	report := maps.Clone(fin.outcome)
	report["beforeStep"] = true
	if refuseIfChanged && h.pageChangedSince(ctx, fin) {
		httpx.ErrorCode(w, http.StatusConflict, "page_changed_during_solve",
			"the page changed while pinchtab solved a captcha, so this step was not run: it was aimed at the page before. Look at the page again",
			false, map[string]any{"tabId": tabID, "autoSolve": report})
		return nil, false
	}
	return report, true
}

func challengeName(t string) string {
	if t == "" {
		return "captcha"
	}
	return t
}

// pageChangedSince reports whether the tab ctx is on shows another document
// than the one run started on. The document key sees a form posted back to its
// own address (reCAPTCHA's demo does that once it has the token); the URL is
// the fallback when the key cannot be read.
func (h *Handlers) pageChangedSince(ctx context.Context, run *solveRun) bool {
	ctx, cancel := context.WithTimeout(ctx, widgetCheckTimeout)
	defer cancel()
	if run.doc != "" {
		if now := bridge.DocumentKey(ctx); now != "" {
			return now != run.doc
		}
	}
	now, err := h.Bridge.CurrentURL(ctx)
	if err != nil || now == "" {
		return false
	}
	return withoutFragment(now) != withoutFragment(run.url)
}

func (h *Handlers) tabDocumentKey(tabID string) string {
	tab, _, err := h.Bridge.TabContext(tabID)
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(tab, widgetCheckTimeout)
	defer cancel()
	return bridge.DocumentKey(ctx)
}

func withoutFragment(u string) string {
	if i := strings.IndexByte(u, '#'); i >= 0 {
		return u[:i]
	}
	return u
}

// @Endpoint GET /autosolve
// @Description Wait for a captcha solve on a tab. With lingerMs, first watch that long for a widget on the page to open a puzzle, and solve it.
//
// @Param tabId    string query Tab ID (optional — uses the current tab)
// @Param waitMs   int    query How long to wait for a solve under way (optional, default and max 40000)
// @Param lingerMs int    query How long to watch for a puzzle to open (optional, default 0)
//
// @Response 200 application/json Returns {tabId, state: idle|solving|done, autoSolve?, pageChanged?, elapsedMs?}
func (h *Handlers) HandleAutoSolveWait(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tabCtx, tabID, err := h.tabContext(r, q.Get("tabId"))
	if err != nil {
		WriteTabContextError(w, err, 404)
		return
	}
	wait := queryMillis(q.Get("waitMs"), autoSolveWaitMax, autoSolveWaitMax)
	linger := queryMillis(q.Get("lingerMs"), 0, maxLinger)
	deadline := time.Now().Add(wait)
	ctx := r.Context()

	run := h.autoSolve.running(tabID)
	if run == nil && linger > 0 && h.autoSolve.isWatching(tabID) {
		run = h.lingerForPuzzle(ctx, tabID, linger, deadline)
	}
	if run != nil {
		select {
		case <-run.done:
		case <-time.After(time.Until(deadline)):
		case <-ctx.Done():
		}
	}
	resp := map[string]any{"tabId": tabID, "state": "idle"}
	if run := h.autoSolve.running(tabID); run != nil {
		resp["state"] = "solving"
		resp["challengeType"] = run.challengeType
		resp["elapsedMs"] = time.Since(run.started).Milliseconds()
	} else if fin := h.autoSolve.takeFinished(tabID); fin != nil {
		resp["state"] = "done"
		resp["autoSolve"] = fin.outcome
		resp["pageChanged"] = h.pageChangedSince(tabCtx, fin)
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *Handlers) HandleTabAutoSolveWait(w http.ResponseWriter, r *http.Request) {
	h.withPathTabID(w, r, h.HandleAutoSolveWait)
}

// lingerForPuzzle watches tabID for up to linger for a widget to open a puzzle,
// starting the solve itself rather than waiting a watcher poll, and returns the
// solve once it is under way.
func (h *Handlers) lingerForPuzzle(ctx context.Context, tabID string, linger time.Duration, deadline time.Time) *solveRun {
	until := time.Now().Add(linger)
	for time.Now().Before(until) {
		if run := h.solveShowingPuzzle(ctx, tabID, deadline); run != nil {
			return run
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(lingerPoll):
		}
	}
	return nil
}

// solveShowingPuzzle starts the solve of a puzzle showing on tabID now, and
// returns it once under way; a solve already under way is returned as it is.
func (h *Handlers) solveShowingPuzzle(ctx context.Context, tabID string, deadline time.Time) *solveRun {
	if run := h.autoSolve.running(tabID); run != nil {
		return run
	}
	if showing, _, err := h.widgetsOnTab(tabID); err != nil || showing == nil {
		return nil
	}
	over := h.startWatchedSolve(tabID)
	for {
		if run := h.autoSolve.running(tabID); run != nil {
			return run
		}
		select {
		case <-over:
			return nil
		case <-ctx.Done():
			return nil
		case <-time.After(time.Until(deadline)):
			return nil
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func queryMillis(v string, def, max time.Duration) time.Duration {
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	return min(time.Duration(n)*time.Millisecond, max)
}
