package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/pinchtab/pinchtab/internal/activity"
	coreautosolver "github.com/pinchtab/pinchtab/internal/autosolver"
	"github.com/pinchtab/pinchtab/internal/autosolver/adapters"
	"github.com/pinchtab/pinchtab/internal/httpx"
)

// HandleSolve attempts to solve a browser challenge on the current page.
//
// When "solver" is omitted from the request body, all registered solvers are
// tried in order via auto-detection.  When "solver" is set (e.g. "cloudflare"),
// only that solver is invoked.
//
// @Endpoint POST /solve
// @Description Auto-detect and solve browser challenges (Cloudflare, etc.)
//
// @Param tabId       string  body  Tab ID (optional — uses default tab)
// @Param solver      string  body  Solver name (optional — auto-detect)
// @Param maxAttempts int     body  Max solve attempts (optional, default: 3)
// @Param timeout     float64 body  Timeout in ms for the whole solve (optional, default: the solvers' estimate, at most 5 minutes)
//
// @Response 200 application/json Returns {tabId, solver, solved, challengeType, attempts, title}, or {tabId, pending: true, autoSolve} for a solve still running after 20s: it goes on in the background, and GET /autosolve waits for it
// @Response 400 application/json Invalid request body or unknown solver
// @Response 423 application/json Tab is locked by another owner
// @Response 500 application/json Chrome/CDP error
//
// @Example curl:
//
//	curl -X POST http://localhost:9867/solve \
//	  -H "Content-Type: application/json" \
//	  -d '{"maxAttempts": 3, "timeout": 30000}'
//
// @Example curl (specific solver):
//
//	curl -X POST http://localhost:9867/solve \
//	  -H "Content-Type: application/json" \
//	  -d '{"solver": "cloudflare", "maxAttempts": 3}'
func (h *Handlers) HandleSolve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TabID       string  `json:"tabId"`
		Solver      string  `json:"solver"`
		MaxAttempts int     `json:"maxAttempts"`
		Timeout     float64 `json:"timeout"`
	}

	if !decodeOptionalJSON(w, r, &req) {
		return
	}

	if name := r.PathValue("name"); name != "" {
		req.Solver = name
	}

	if req.Solver != "" && !h.isAvailableAutoSolver(req.Solver) {
		h.rejectUnavailableSolver(w, req.Solver)
		return
	}

	ctx, resolvedTabID, err := h.tabContext(r, req.TabID)
	if err != nil {
		WriteTabContextError(w, err, 404)
		return
	}

	owner := resolveOwner(r, "")
	if err := h.enforceTabLease(resolvedTabID, owner); err != nil {
		httpx.ErrorCode(w, 423, "tab_locked", err.Error(), false, nil)
		return
	}

	if _, ok := h.applyTabGuards(w, r, ctx, resolvedTabID, guardDialogBlocked|guardDomainPolicy|guardHandoffPause); !ok {
		return
	}

	action := "solve"
	if req.Solver != "" {
		action = "solve:" + req.Solver
	}
	h.recordActivity(r, activity.Update{Action: action, TabID: resolvedTabID})

	page, executor, err := adapters.NewFromBridge(h.Bridge, resolvedTabID)
	if err != nil {
		httpx.Error(w, 500, fmt.Errorf("resolve solve tab: %w", err))
		return
	}

	cfg := h.normalizedAutoSolverConfig()
	cfg.Enabled = true
	if req.MaxAttempts > 0 {
		cfg.MaxAttempts = req.MaxAttempts
	}
	if req.Solver != "" {
		cfg.Solvers = []string{req.Solver}
	}

	includeSemantic := req.Solver == "" || req.Solver == coreautosolver.SemanticSolverName
	as := h.buildAutoSolver(cfg, includeSemantic)

	timeout := min(estimateAutoSolverRunTimeout(cfg), solveRunCap)
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Millisecond
	}

	outcome, done := h.runTrackedSolve(ctx, resolvedTabID, page.URL(), timeout, func(ctx context.Context) autoSolveOutcome {
		return h.explicitSolve(ctx, resolvedTabID, req.Solver, as, page, executor)
	})
	if done {
		if _, ok := h.applyTabGuards(w, r, ctx, resolvedTabID, guardDomainPolicy); !ok {
			return
		}
	}
	httpx.JSON(w, 200, outcome)
}

// runTrackedSolve runs solve on tabID like an auto-triggered solve: tracked on
// the tab, parented on tabCtx rather than the request (a token bought after the
// caller left would be lost), and waited on by steps and GET /autosolve. It
// waits one step's share and reports done with the outcome, or a pending answer
// the CLI waits on through autoSolve.pending as it does for a step's.
func (h *Handlers) runTrackedSolve(tabCtx context.Context, tabID, url string, timeout time.Duration, solve func(context.Context) autoSolveOutcome) (map[string]any, bool) {
	run, ok := h.autoSolve.begin(tabID, url, "")
	if !ok {
		return pendingSolveResponse(tabID, h.autoSolve.running(tabID)), false
	}
	run.doc = h.tabDocumentKey(tabID)
	go func() {
		ctx, cancel := context.WithTimeout(tabCtx, timeout)
		defer cancel()
		h.autoSolve.end(tabID, run, solve(ctx))
	}()
	if !h.waitStep(run, stepSolveWait) {
		return pendingSolveResponse(tabID, run), false
	}
	h.autoSolve.takeFinished(tabID)
	return run.outcome, true
}

func pendingSolveResponse(tabID string, run *solveRun) map[string]any {
	solve := autoSolveOutcome{"solved": false, "pending": true, "tabId": tabID}
	if run != nil && run.challengeType != "" {
		solve["challengeType"] = run.challengeType
	}
	return map[string]any{"tabId": tabID, "solved": false, "pending": true, "autoSolve": solve}
}

// explicitSolve runs as on tabID and records the outcome as an auto-triggered
// solve's would be: solved, or given up and handed off.
func (h *Handlers) explicitSolve(ctx context.Context, tabID, solver string, as *coreautosolver.AutoSolver, page coreautosolver.Page, executor coreautosolver.ActionExecutor) autoSolveOutcome {
	url := page.URL()
	result, err := as.Solve(ctx, page, executor)
	if err == nil && result == nil {
		err = fmt.Errorf("empty result")
	}
	if err != nil {
		return autoSolveOutcome{"tabId": tabID, "solved": false, "error": fmt.Sprintf("solve: %v", err)}
	}

	solverName := result.SolverUsed
	if solverName == "" && solver != "" {
		solverName = solver
	}

	title := result.FinalTitle
	if title == "" {
		title = page.Title()
	}

	challengeType := deriveChallengeType(result, page)
	resp := autoSolveOutcome{
		"tabId":         tabID,
		"solver":        solverName,
		"solved":        result.Solved,
		"challengeType": challengeType,
		"attempts":      result.Attempts,
		"title":         title,
	}
	if result.Solved {
		h.autoSolve.markSolved(tabID, url, challengeType)
		// An earlier auto-trigger may have given up on this challenge.
		h.autoSolve.clearGaveUp(tabID)
	}

	// Without this the caller sees only solved:false, and cannot tell an
	// unconfigured provider from a rejected sitekey from a solver that never ran.
	if !result.Solved && result.Error != "" {
		resp["error"] = result.Error
		// result.Error is the chain's verdict ("all N attempts exhausted"); the
		// per-attempt reasons are what actually says which solver refused and why.
		if len(result.History) > 0 {
			resp["history"] = result.History
		}
		slog.Warn("solve did not resolve challenge",
			"solver", solverName,
			"challenge_type", challengeType,
			"attempts", result.Attempts,
			"error", result.Error,
			"history", attemptSummaries(result.History))
	}

	// If a challenge was detected but the solver couldn't resolve it, flip the
	// tab into paused_handoff so subsequent actions block and the caller can
	// escalate to a human.
	if !result.Solved && result.Attempts > 0 && challengeType != "" {
		h.autoSolve.markGaveUp(tabID, challengeType, resp)
		h.autoHandoffAfterFailure(tabID, challengeType)
		resp["handoff"] = "paused_handoff"
		resp["hint"] = handoffHintMessage
	}
	return resp
}

// HandleTabSolve handles POST /tabs/{id}/solve and /tabs/{id}/solve/{name}.
//
// @Endpoint POST /tabs/{id}/solve
// @Description Solve a browser challenge on a specific tab
func (h *Handlers) HandleTabSolve(w http.ResponseWriter, r *http.Request) {
	// Reject (not silently overwrite) a body tabId that disagrees with the
	// path id; the path id is canonical and forwarded to HandleSolve.
	h.withPathTabIDBody(w, r, h.HandleSolve)
}

// HandleListSolvers returns the list of registered solver names.
//
// @Endpoint GET /solvers
// @Description List available challenge solvers
// @Response 200 application/json Returns {solvers: ["cloudflare", ...]}
func (h *Handlers) HandleListSolvers(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, 200, map[string]any{
		"solvers": h.availableAutoSolverNames(),
	})
}

// HandleAutoSolverConfig returns effective autosolver runtime settings.
//
// @Endpoint GET /config/autosolver
// @Description Return effective autosolver configuration and available solver names
// @Response 200 application/json Returns autosolver runtime config
func (h *Handlers) HandleAutoSolverConfig(w http.ResponseWriter, r *http.Request) {
	cfg := h.normalizedAutoSolverConfig()

	autoTrigger := true
	triggerOnNavigate := true
	triggerOnAction := true
	llmProvider := ""

	if h != nil && h.Config != nil {
		autoTrigger = h.Config.AutoSolver.AutoTrigger
		triggerOnNavigate = h.Config.AutoSolver.TriggerOnNavigate
		triggerOnAction = h.Config.AutoSolver.TriggerOnAction
		llmProvider = h.Config.AutoSolver.LLMProvider
	}

	httpx.JSON(w, 200, map[string]any{
		"enabled":           cfg.Enabled,
		"autoTrigger":       autoTrigger,
		"triggerOnNavigate": triggerOnNavigate,
		"triggerOnAction":   triggerOnAction,
		"maxAttempts":       cfg.MaxAttempts,
		"solverTimeoutSec":  int(cfg.SolverTimeout / time.Second),
		"retryBaseDelayMs":  int(cfg.RetryBaseDelay / time.Millisecond),
		"retryMaxDelayMs":   int(cfg.RetryMaxDelay / time.Millisecond),
		"solvers":           h.availableAutoSolverNames(),
		"llmProvider":       llmProvider,
		"llmFallback":       cfg.LLMFallback,
	})
}

// rejectUnavailableSolver distinguishes the two reasons a named solver cannot
// run. A key-gated solver reaching here is always a keyless one — with its key
// set it would be available — so it gets its own code and names the config key,
// rather than being reported as a typo. Config validation deliberately accepts
// the same name without its key; see docs/endpoints.md.
func (h *Handlers) rejectUnavailableSolver(w http.ResponseWriter, name string) {
	if gated, ok := coreautosolver.KeyGatedSolverNamed(name); ok {
		httpx.ErrorCode(w, 400, "solver_key_missing",
			fmt.Sprintf("solver %q is configured but its API key is not set; set %s to use it", gated.Name, gated.ConfigKey),
			false, nil)
		return
	}
	httpx.ErrorCode(w, 400, "unknown_solver",
		fmt.Sprintf("unknown solver %q (available: %v)", name, h.availableAutoSolverNames()),
		false, nil)
}

// attemptSummaries flattens the per-solver outcomes into one log field.
func attemptSummaries(history []coreautosolver.AttemptEntry) []string {
	out := make([]string, 0, len(history))
	for _, e := range history {
		summary := fmt.Sprintf("%s:%s", e.Solver, e.Status)
		if e.Error != "" {
			summary += ":" + e.Error
		}
		out = append(out, summary)
	}
	return out
}

func deriveChallengeType(result *coreautosolver.Result, page coreautosolver.Page) string {
	if result == nil || page == nil {
		return ""
	}

	finalTitle := result.FinalTitle
	if finalTitle == "" {
		finalTitle = page.Title()
	}
	finalURL := result.FinalURL
	if finalURL == "" {
		finalURL = page.URL()
	}

	html, err := page.HTML()
	if err == nil {
		if detected := coreautosolver.DetectChallengeIntent(finalTitle, finalURL, html); detected != nil {
			if detected.ChallengeType != "" {
				return detected.ChallengeType
			}
			if detected.Type != "" && detected.Type != coreautosolver.IntentNormal {
				return string(detected.Type)
			}
		}
	}

	if result.Intent != "" && result.Intent != coreautosolver.IntentNormal {
		return string(result.Intent)
	}

	return ""
}
