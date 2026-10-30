package handlers

import (
	"sync"
	"time"
)

// solvedChallengeTTL outlasts a solve token: reCAPTCHA's expire after two minutes.
const solvedChallengeTTL = 2 * time.Minute

// autoSolveTabs keeps auto-triggers from paying twice for one challenge. A solved
// widget stays in the DOM, so every later action on the page re-detected it and
// bought, and waited on, a fresh token; and an action during a solve started a
// second one alongside it.
type autoSolveTabs struct {
	mu       sync.Mutex
	inflight map[string]*solveRun
	// finished is a tab's last solve that no caller has been told about yet:
	// one the watcher started between steps, or one a request stopped waiting on.
	finished map[string]*solveRun
	// watching marks tabs whose watcher is running.
	watching map[string]bool
	solved   map[string]solvedChallenge
	// retry is when a scheduled retry of a tab's challenge runs. Until then
	// the challenge is still the solver's, and the tab reports it pending.
	retry map[string]scheduledRetry
	// gaveUp is the final outcome of a challenge the solver is done with, per
	// tab, so later actions report it instead of trying again. The caller has
	// been told the challenge is theirs now.
	gaveUp map[string]gaveUpChallenge
	// noted is the page URL per tab whose reCAPTCHA v3 the caller was told is
	// left to the browser, so the HINT comes once rather than on every action.
	noted map[string]string
}

type gaveUpChallenge struct {
	challengeType string
	outcome       autoSolveOutcome
}

type scheduledRetry struct {
	url string
	at  time.Time
}

type solvedChallenge struct {
	url, challengeType string
	until              time.Time
}

// solveRun is one solve on a tab. done closes once outcome is set.
type solveRun struct {
	url string
	// doc is the document the solve started on (bridge.DocumentKey), set
	// before done closes; "" when it could not be read.
	doc           string
	challengeType string
	started       time.Time
	done          chan struct{}
	outcome       autoSolveOutcome
	// stepWaited is how long steps have waited on this solve in all.
	stepWaited time.Duration
}

// stepWaitBudget is how long steps wait on one solve in all. Past it, a step
// that meets the solve is refused at once rather than stalled again: a solve
// that never finishes must not cost minutes per step. `pinchtab wait
// --captcha` waits on longer when the agent chooses to.
var stepWaitBudget = 2 * time.Minute

// stepWaitLeft is what is left of run's step wait budget.
func (t *autoSolveTabs) stepWaitLeft(run *solveRun) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	return max(stepWaitBudget-run.stepWaited, 0)
}

func (t *autoSolveTabs) addStepWait(run *solveRun, d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	run.stepWaited += d
}

// begin claims tabID for a solve of the challenge at url, reporting false when
// one is already running.
func (t *autoSolveTabs) begin(tabID, url, challengeType string) (*solveRun, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inflight[tabID] != nil {
		return nil, false
	}
	if t.inflight == nil {
		t.inflight = map[string]*solveRun{}
	}
	run := &solveRun{url: url, challengeType: challengeType, started: time.Now(), done: make(chan struct{})}
	t.inflight[tabID] = run
	return run, true
}

// end records run's outcome as tabID's unreported solve and releases the tab.
func (t *autoSolveTabs) end(tabID string, run *solveRun, outcome autoSolveOutcome) {
	t.mu.Lock()
	defer t.mu.Unlock()
	run.outcome = outcome
	close(run.done)
	delete(t.inflight, tabID)
	// A scheduled retry reports itself when it runs.
	if pending, _ := outcome["pending"].(bool); outcome == nil || pending {
		return
	}
	if t.finished == nil {
		t.finished = map[string]*solveRun{}
	}
	t.finished[tabID] = run
}

// running is the solve under way on tabID, if any.
func (t *autoSolveTabs) running(tabID string) *solveRun {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.inflight[tabID]
}

// takeFinished hands over tabID's unreported solve, once.
func (t *autoSolveTabs) takeFinished(tabID string) *solveRun {
	t.mu.Lock()
	defer t.mu.Unlock()
	run := t.finished[tabID]
	delete(t.finished, tabID)
	return run
}

// startWatching claims tabID for a watcher, reporting false when one runs.
func (t *autoSolveTabs) startWatching(tabID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.watching[tabID] {
		return false
	}
	if t.watching == nil {
		t.watching = map[string]bool{}
	}
	t.watching[tabID] = true
	return true
}

func (t *autoSolveTabs) stopWatching(tabID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.watching, tabID)
}

func (t *autoSolveTabs) isWatching(tabID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.watching[tabID]
}

func (t *autoSolveTabs) markSolved(tabID, url, challengeType string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.solved == nil {
		t.solved = map[string]solvedChallenge{}
	}
	t.solved[tabID] = solvedChallenge{url: url, challengeType: challengeType, until: time.Now().Add(solvedChallengeTTL)}
}

func (t *autoSolveTabs) recentlySolved(tabID, url, challengeType string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.solved[tabID]
	return ok && s.url == url && s.challengeType == challengeType && time.Now().Before(s.until)
}

// forget drops tabID's solved challenge and any scheduled retry: a navigation
// loads a fresh page.
func (t *autoSolveTabs) forget(tabID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.solved, tabID)
	delete(t.retry, tabID)
	delete(t.gaveUp, tabID)
	delete(t.noted, tabID)
	delete(t.finished, tabID)
}

// noteOnce reports whether tabID's page at url is being noted for the first time.
func (t *autoSolveTabs) noteOnce(tabID, url string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.noted[tabID] == url {
		return false
	}
	if t.noted == nil {
		t.noted = map[string]string{}
	}
	t.noted[tabID] = url
	return true
}

// markGaveUp records the final outcome for tabID's challenge of this type.
func (t *autoSolveTabs) markGaveUp(tabID, challengeType string, outcome autoSolveOutcome) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.gaveUp == nil {
		t.gaveUp = map[string]gaveUpChallenge{}
	}
	t.gaveUp[tabID] = gaveUpChallenge{challengeType: challengeType, outcome: outcome}
}

// gaveUpOn is the final outcome recorded for tabID's challenge of this type.
func (t *autoSolveTabs) gaveUpOn(tabID, challengeType string) (autoSolveOutcome, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	g, ok := t.gaveUp[tabID]
	if !ok || g.challengeType != challengeType {
		return nil, false
	}
	return g.outcome, true
}

// scheduleRetry claims tabID's challenge at url for one retry at `at`,
// reporting false when it already had its retry.
func (t *autoSolveTabs) scheduleRetry(tabID, url string, at time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if r, ok := t.retry[tabID]; ok && r.url == url {
		return false
	}
	if t.retry == nil {
		t.retry = map[string]scheduledRetry{}
	}
	t.retry[tabID] = scheduledRetry{url: url, at: at}
	return true
}

// retryPending is the time left before tabID's scheduled retry, if one is due.
func (t *autoSolveTabs) retryPending(tabID string) (time.Duration, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	r, ok := t.retry[tabID]
	if !ok || !time.Now().Before(r.at) {
		return 0, false
	}
	return time.Until(r.at), true
}

// retryDue reports whether tabID's retry for url is still wanted: the tab has
// not navigated since (forget drops it).
func (t *autoSolveTabs) retryDue(tabID, url string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	r, ok := t.retry[tabID]
	return ok && r.url == url
}
