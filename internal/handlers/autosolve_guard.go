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
	inflight map[string]bool
	solved   map[string]solvedChallenge
	// retry is when a scheduled retry of a tab's challenge runs. Until then
	// the challenge is still the solver's, and the tab reports it pending.
	retry map[string]scheduledRetry
	// gaveUp is the final outcome of a challenge the solver is done with, per
	// tab, so later actions report it instead of trying again. The caller has
	// been told the challenge is theirs now.
	gaveUp map[string]gaveUpChallenge
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

// begin claims tabID for a solve, reporting false when one is already running.
func (t *autoSolveTabs) begin(tabID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inflight[tabID] {
		return false
	}
	if t.inflight == nil {
		t.inflight = map[string]bool{}
	}
	t.inflight[tabID] = true
	return true
}

func (t *autoSolveTabs) end(tabID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.inflight, tabID)
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
