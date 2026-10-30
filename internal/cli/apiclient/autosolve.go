package apiclient

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// A step blocks while pinchtab solves a captcha on its tab. The server answers
// within its own request budget either way: a step it held back comes as 409
// solve_in_progress and is sent again once the solve is over; a step whose own
// solve outlasted the request comes with autoSolve.pending, and the outcome
// replaces it. Both wait on GET /autosolve, one bounded call after another.
var (
	// solveWaitCeiling outlasts the server's run budget, which a solve a person
	// does can fill: 2captcha's hCaptcha has 180s a try.
	solveWaitCeiling = 6 * time.Minute
	solveWaitChunk   = 40 * time.Second
)

func solveInProgressTab(body []byte) (string, bool) {
	var e struct {
		Code    string `json:"code"`
		Details struct {
			TabID string `json:"tabId"`
		} `json:"details"`
	}
	if json.Unmarshal(body, &e) != nil || e.Code != "solve_in_progress" || e.Details.TabID == "" {
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
	resp := waitForSolve(client, token, base, tab, 0, deadline)
	if resp["state"] != "done" {
		return body
	}
	m["autoSolve"] = resp["autoSolve"]
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return body
	}
	return out
}

// waitForSolve waits on tab's solve until it is over or deadline passes, and
// returns the last GET /autosolve answer, nil when there was none. With linger,
// the first call first watches that long for a widget to open a puzzle. An
// empty tab is the caller's current tab.
func waitForSolve(client *http.Client, token, base, tab string, linger time.Duration, deadline time.Time) map[string]any {
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
		status, body, err := sendOnce(client, token, newRequest(http.MethodGet, base, "/autosolve", WithQuery(q)))
		if err != nil || status >= http.StatusBadRequest {
			return last
		}
		var m map[string]any
		if json.Unmarshal(body, &m) != nil {
			return last
		}
		last = m
		if m["state"] != "solving" {
			return m
		}
	}
}

// AwaitCaptcha gives a widget on the current tab linger to open a puzzle after
// a batch's last step, then waits out the solve it starts, or one already under
// way. It returns the solve's outcome, nil when there was none.
func AwaitCaptcha(client *http.Client, base, token string, linger time.Duration) map[string]any {
	resp := waitForSolve(client, token, base, "", linger, time.Now().Add(solveWaitCeiling))
	switch resp["state"] {
	case "done":
		out, _ := resp["autoSolve"].(map[string]any)
		return out
	case "solving":
		return map[string]any{"solved": false, "pending": true, "challengeType": resp["challengeType"]}
	}
	return nil
}
