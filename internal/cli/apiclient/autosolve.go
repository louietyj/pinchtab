package apiclient

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// A step blocks while pinchtab solves a captcha on its tab, up to the 2 minutes
// the server allows steps per solve. The server answers within its own request
// budget either way: a step it held back comes as 409 solve_in_progress and is
// sent again once the solve is over; a step whose own solve outlasted the
// request comes with autoSolve.pending, and the outcome replaces it. Both wait
// on GET /autosolve, one bounded call after another. Once the server says the
// budget is spent (waitedOut), the step returns still solving, and the agent
// decides whether to `pinchtab wait --captcha`.
var (
	// solveWaitCeiling backs up the server's step wait budget.
	solveWaitCeiling = 150 * time.Second
	solveWaitChunk   = 40 * time.Second
)

// solveInProgressTab is the tab of a step held back for a solve that is worth
// waiting on: not one whose wait budget is spent.
func solveInProgressTab(body []byte) (string, bool) {
	var e struct {
		Code    string `json:"code"`
		Details struct {
			TabID     string `json:"tabId"`
			WaitedOut bool   `json:"waitedOut"`
		} `json:"details"`
	}
	if json.Unmarshal(body, &e) != nil || e.Code != "solve_in_progress" || e.Details.TabID == "" || e.Details.WaitedOut {
		return "", false
	}
	return e.Details.TabID, true
}

func awaitPendingSolve(client *http.Client, token, base string, body []byte, deadline time.Time) []byte {
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return body
	}
	solve, _ := m["autoSolve"].(map[string]any)
	if pending, _ := solve["pending"].(bool); !pending {
		return body
	}
	// A retry the solver scheduled is told to the caller as such.
	if _, retry := solve["retryInSec"]; retry {
		return body
	}
	tab, _ := solve["tabId"].(string)
	if tab == "" {
		return body
	}
	resp := waitForSolve(client, token, base, tab, 0, deadline, false)
	switch resp["state"] {
	case "done":
		m["autoSolve"] = resp["autoSolve"]
	case "solving":
		if t, _ := resp["challengeType"].(string); t != "" {
			solve["challengeType"] = t
		}
	default:
		return body
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return body
	}
	return out
}

// waitForSolve waits on tab's solve until it is over or deadline passes, and
// returns the last GET /autosolve answer, nil when there was none. With linger,
// the first call first watches that long for a widget to open a puzzle. An
// empty tab is the caller's current tab. Unless explicit, it stops when the
// solve's step wait budget is spent.
func waitForSolve(client *http.Client, token, base, tab string, linger time.Duration, deadline time.Time, explicit bool) map[string]any {
	var last map[string]any
	for {
		wait := time.Until(deadline)
		if wait > solveWaitChunk {
			wait = solveWaitChunk
		}
		if wait <= 0 {
			return last
		}
		q := url.Values{"waitMs": {strconv.FormatInt(wait.Milliseconds(), 10)}}
		if tab != "" {
			q.Set("tabId", tab)
		}
		if linger > 0 {
			q.Set("lingerMs", strconv.FormatInt(linger.Milliseconds(), 10))
			linger = 0
		}
		if explicit {
			q.Set("explicit", "1")
		}
		status, body, err := sendOnce(client, token, newRequest(http.MethodGet, base, "/autosolve", WithQuery(q)))
		if err != nil || status >= http.StatusBadRequest {
			return last
		}
		var m map[string]any
		if json.Unmarshal(body, &m) != nil {
			return last
		}
		last = m
		if waitedOut, _ := m["waitedOut"].(bool); m["state"] != "solving" || (waitedOut && !explicit) {
			return m
		}
	}
}

// AwaitCaptcha gives a widget on the current tab linger to open a puzzle after
// a batch's last step, then waits out the solve it starts, or one already under
// way, within the step wait budget. It returns the solve's outcome, nil when
// there was none.
func AwaitCaptcha(client *http.Client, base, token string, linger time.Duration) map[string]any {
	return solveOutcome(waitForSolve(client, token, base, "", linger, time.Now().Add(solveWaitCeiling), false))
}

// WaitForCaptcha is `pinchtab wait --captcha`: it waits for the solve on tab
// (the current tab when empty) for up to timeout, past the step budget.
func WaitForCaptcha(client *http.Client, base, token, tab string, timeout time.Duration) map[string]any {
	return solveOutcome(waitForSolve(client, token, base, tab, 0, time.Now().Add(timeout), true))
}

func solveOutcome(resp map[string]any) map[string]any {
	switch resp["state"] {
	case "done":
		out, _ := resp["autoSolve"].(map[string]any)
		return out
	case "solving":
		return map[string]any{"solved": false, "pending": true, "challengeType": resp["challengeType"], "elapsedMs": resp["elapsedMs"]}
	}
	return nil
}
