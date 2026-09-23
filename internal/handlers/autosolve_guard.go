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

// forget drops tabID's solved challenge: a navigation loads a fresh one.
func (t *autoSolveTabs) forget(tabID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.solved, tabID)
}
