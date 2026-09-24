package handlers

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	coreautosolver "github.com/pinchtab/pinchtab/internal/autosolver"
	"github.com/pinchtab/pinchtab/internal/autosolver/adapters"
	"github.com/pinchtab/pinchtab/internal/autosolver/catalog"
	autosolverllm "github.com/pinchtab/pinchtab/internal/autosolver/llm"
	autosolversemantic "github.com/pinchtab/pinchtab/internal/autosolver/semantic"
)

const (
	autoSolverTriggerNavigate = "navigate"
	autoSolverTriggerAction   = "action"

	// autoTriggerMaxAttempts caps retries on nav/action auto-triggers so a
	// missed challenge doesn't block the request path. Explicit POST /solve
	// still uses the fully configured MaxAttempts.
	autoTriggerMaxAttempts = 2

	// autoDetectHTMLTimeout bounds the detection HTML fetch on every nav/action
	// so a stuck CDP call is cancelled rather than leaking a worker.
	autoDetectHTMLTimeout = 5 * time.Second

	// autoTriggerRunBudget floors the total time an auto-trigger run can take
	// end-to-end (detection + retries). Safety valve for slow pages.
	autoTriggerRunBudget = 45 * time.Second
)

var (
	// autoSolveReplyMargin leaves an awaiting request time to read the page and
	// answer after it stops waiting on a solve.
	autoSolveReplyMargin = 10 * time.Second
	// autoSolvePendingFloor outlasts detection's HTML timeout, so a runner still
	// going at the cutoff has found a challenge.
	autoSolvePendingFloor = autoDetectHTMLTimeout + time.Second
)

// autoSolveOutcome is what a caller can report back: enough to say a solve
// happened and how it went, without exposing solver internals. A map rather
// than a struct because challenge_detection.go is the only permitted producer
// of a ChallengeType field, and HandleSolve carries its own copy the same way.
type autoSolveOutcome map[string]any

// maybeAutoSolve runs the autosolver pipeline for tabID. It returns an outcome
// only when it awaited one; in background mode it returns nil immediately and
// the solve completes with no signal to the caller at all.
func (h *Handlers) maybeAutoSolve(ctx context.Context, tabID, trigger string) autoSolveOutcome {
	if tabID == "" || h.autoSolverRunner == nil || !h.shouldAutoSolve(trigger) {
		return nil
	}
	if trigger == autoSolverTriggerNavigate {
		h.autoSolve.forget(tabID)
	}

	// Awaiting costs the caller the solve time, but only on a page that carries a
	// challenge and is unusable until it is solved. Returning early there hands
	// back a page the caller cannot act on and no way to learn why.
	if h.Config != nil && h.Config.AutoSolver.AwaitOnNavigate {
		done := make(chan autoSolveOutcome, 1)
		go func() {
			runCtx, cancel := context.WithTimeout(context.Background(), h.autoTriggerBudget())
			defer cancel()

			outcome, err := h.autoSolverRunner(runCtx, tabID)
			if err != nil {
				slog.Warn("autosolver auto-trigger failed",
					"trigger", trigger,
					"tab_id", tabID,
					"error", err)
			}
			done <- outcome
		}()

		select {
		case outcome := <-done:
			return outcome
		case <-autoSolveReplyCutoff(ctx):
		}
		// A solve can outlast the request. It keeps running; the caller learns one is
		// under way instead of timing out with nothing.
		slog.Info("autosolver auto-trigger still running at reply cutoff",
			"trigger", trigger,
			"tab_id", tabID)
		return autoSolveOutcome{"solved": false, "pending": true}
	}

	go func() {
		runCtx, cancel := context.WithTimeout(context.Background(), h.autoTriggerBudget())
		defer cancel()

		if _, err := h.autoSolverRunner(runCtx, tabID); err != nil &&
			!errors.Is(err, context.Canceled) &&
			!errors.Is(err, context.DeadlineExceeded) {
			slog.Warn("autosolver auto-trigger failed",
				"trigger", trigger,
				"tab_id", tabID,
				"error", err)
		}
	}()
	return nil
}

// autoSolveReplyCutoff fires autoSolveReplyMargin before ctx's deadline, but never
// before autoSolvePendingFloor; with no deadline it never fires.
func autoSolveReplyCutoff(ctx context.Context) <-chan time.Time {
	deadline, ok := ctx.Deadline()
	if !ok {
		return nil
	}
	return time.After(max(time.Until(deadline)-autoSolveReplyMargin, autoSolvePendingFloor))
}

// autoTriggerBudget sizes the run from the same estimate runAutoSolver gives its
// own context, so the outer deadline can never cancel a solve the inner one
// still considers live. The fixed floor predates external solvers: a CapSolver
// reCAPTCHA solve alone takes 20-60s, so a hardcoded 45s cap cut every solve
// short and then flipped the tab to paused_handoff -- an auto-solve that could
// not succeed on the challenges it exists to handle.
func (h *Handlers) autoTriggerBudget() time.Duration {
	budget := estimateAutoSolverRunTimeout(h.normalizedAutoSolverConfig())
	if budget < autoTriggerRunBudget {
		return autoTriggerRunBudget
	}
	return budget
}

func (h *Handlers) shouldAutoSolve(trigger string) bool {
	if h == nil || h.Config == nil {
		return false
	}

	cfg := h.Config.AutoSolver
	if !cfg.Enabled || !cfg.AutoTrigger {
		return false
	}

	switch trigger {
	case autoSolverTriggerNavigate:
		return cfg.TriggerOnNavigate
	case autoSolverTriggerAction:
		return cfg.TriggerOnAction
	default:
		return false
	}
}

func (h *Handlers) runAutoSolver(ctx context.Context, tabID string) (autoSolveOutcome, error) {
	if h == nil || h.Config == nil || h.Bridge == nil {
		return nil, nil
	}

	page, executor, err := adapters.NewFromBridge(h.Bridge, tabID)
	if err != nil {
		return nil, err
	}

	// Detection is the cheap path that runs on every nav/action — bound the HTML
	// fetch so a stuck CDP call is cancelled (no leaked worker), keeping normal
	// pages fast.
	html, err := page.HTMLWithin(autoDetectHTMLTimeout)
	if err != nil {
		return nil, err
	}

	challenge := coreautosolver.DetectChallengeIntent(page.Title(), page.URL(), html)
	if challenge == nil {
		return nil, nil
	}
	challengeURL := page.URL()
	if h.autoSolve.recentlySolved(tabID, challengeURL, challenge.ChallengeType) {
		return nil, nil
	}
	if !h.autoSolve.begin(tabID) {
		return autoSolveOutcome{"solved": false, "pending": true}, nil
	}
	defer h.autoSolve.end(tabID)

	cfg := h.normalizedAutoSolverConfig()
	if cfg.MaxAttempts > autoTriggerMaxAttempts {
		cfg.MaxAttempts = autoTriggerMaxAttempts
	}
	as := h.buildAutoSolver(cfg, true)

	// Parent the solve on the tab's CDP context, not the caller's. Evaluate runs
	// chromedp against whatever context it is handed, and the auto-trigger path
	// arrives here from a background goroutine whose context carries no chromedp
	// target -- so every injection failed with "invalid context" after the token
	// had been bought and paid for. HandleSolve parents on the tab for the same
	// reason, which is why the explicit endpoint never hit this.
	tabCtx, _, err := h.Bridge.TabContext(tabID)
	if err != nil {
		return nil, err
	}

	var types, solvers []string
	for round := 1; ; round++ {
		result, err := h.autoSolveRound(ctx, tabCtx, tabID, as, cfg, page, executor)
		if err != nil {
			return nil, err
		}
		// Attempts==0 means detection fired but no solver ran, which is not something
		// the caller needs told about — report only runs that actually did work.
		if result == nil || result.Attempts == 0 {
			if round == 1 {
				return nil, nil
			}
			break
		}

		// The type seen when the round started: a solved page no longer shows it.
		challengeType := challenge.ChallengeType
		if challengeType == "" {
			challengeType = deriveChallengeType(result, page)
		}
		types = append(types, challengeType)
		if result.SolverUsed != "" {
			solvers = append(solvers, result.SolverUsed)
		}
		outcome := autoSolveOutcome{
			"solved":        result.Solved,
			"challengeType": strings.Join(types, ", then "),
			"attempts":      result.Attempts,
		}
		if len(solvers) > 0 {
			outcome["solver"] = strings.Join(solvers, ", then ")
		}
		if result.Error != "" {
			outcome["error"] = result.Error
		}

		if !result.Solved {
			slog.Warn("autosolver auto-trigger did not solve challenge",
				"tab_id", tabID,
				"attempts", result.Attempts,
				"error", result.Error)
			h.autoHandoffAfterFailure(tabID, challengeType)
			return outcome, nil
		}
		h.autoSolve.markSolved(tabID, challengeURL, challenge.ChallengeType)
		slog.Info("autosolver auto-trigger solved challenge",
			"tab_id", tabID,
			"solver", result.SolverUsed,
			"attempts", result.Attempts)

		// A solve can land on a page that challenges again at once: AliExpress
		// lets a slider pass through to an item page whose data request it then
		// punishes with reCAPTCHA. Nobody else would look before the next call.
		next := h.challengeAfterSolve(ctx, page)
		if round == autoSolveMaxRounds || next == nil ||
			h.autoSolve.recentlySolved(tabID, page.URL(), next.ChallengeType) {
			return outcome, nil
		}
		challenge, challengeURL = next, page.URL()
	}
	return nil, nil
}

// autoSolveMaxRounds caps how many challenges one navigation solves in a row.
const autoSolveMaxRounds = 3

// challengeAfterSolveDelay lets a solved page load what it loads next, which
// is when a follow-up challenge appears.
const challengeAfterSolveDelay = 2 * time.Second

func (h *Handlers) challengeAfterSolve(ctx context.Context, page coreautosolver.Page) *coreautosolver.Intent {
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(challengeAfterSolveDelay):
	}
	html, err := page.HTMLWithin(autoDetectHTMLTimeout)
	if err != nil {
		return nil
	}
	return coreautosolver.DetectChallengeIntent(page.Title(), page.URL(), html)
}

// autoSolveRound runs one solve of the challenge now on the tab.
func (h *Handlers) autoSolveRound(ctx, tabCtx context.Context, tabID string, as *coreautosolver.AutoSolver, cfg coreautosolver.Config, page coreautosolver.Page, executor coreautosolver.ActionExecutor) (*coreautosolver.Result, error) {
	// The caller's budget still cancels the run; both are sized from the same
	// estimate, so neither can cut short a solve the other considers live.
	solveCtx, cancel := context.WithTimeout(tabCtx, estimateAutoSolverRunTimeout(cfg))
	defer cancel()
	defer context.AfterFunc(ctx, cancel)()
	return as.Solve(solveCtx, page, executor)
}

// autoHandoffAfterFailure flips the tab into paused_handoff so action routes
// block and the dashboard/agent can escalate to a human. No-op if the tab is
// already paused or the bridge does not support handoff state.
func (h *Handlers) autoHandoffAfterFailure(tabID, challengeType string) {
	// Unattended there is nobody to hand off to, and parking the tab only makes
	// every later action 409 -- the caller is stuck on a page it could still
	// read, with no way back except an explicit resume it has no reason to call.
	if h != nil && h.Config != nil && !h.Config.AutoSolver.HandoffOnFailure {
		slog.Debug("autosolver: handoff disabled; leaving tab active",
			"tab_id", tabID,
			"challenge_type", challengeType)
		return
	}
	if tabID == "" {
		return
	}
	ctrl, ok := h.handoffController()
	if !ok {
		return
	}
	if state, exists := ctrl.TabHandoffState(tabID); exists && state.Status == "paused_handoff" {
		return
	}
	reason := "autosolver_unsolved"
	if trimmed := strings.TrimSpace(challengeType); trimmed != "" {
		reason = "autosolver_unsolved:" + trimmed
	}
	if _, err := h.pauseTabForHandoff(tabID, reason, "autosolver", 0); err != nil {
		slog.Warn("autosolver: auto-handoff failed",
			"tab_id", tabID,
			"reason", reason,
			"error", err)
	}
}

func (h *Handlers) normalizedAutoSolverConfig() coreautosolver.Config {
	cfg := coreautosolver.DefaultConfig()
	if h == nil || h.Config == nil {
		return cfg
	}

	cfg.Enabled = h.Config.AutoSolver.Enabled
	cfg.HandoffOnFailure = h.Config.AutoSolver.HandoffOnFailure
	if h.Config.AutoSolver.MaxAttempts > 0 {
		cfg.MaxAttempts = h.Config.AutoSolver.MaxAttempts
	}
	if h.Config.AutoSolver.SolverTimeoutSec > 0 {
		cfg.SolverTimeout = time.Duration(h.Config.AutoSolver.SolverTimeoutSec) * time.Second
	}
	if h.Config.AutoSolver.RetryBaseDelayMs >= 0 {
		cfg.RetryBaseDelay = time.Duration(h.Config.AutoSolver.RetryBaseDelayMs) * time.Millisecond
	}
	if h.Config.AutoSolver.RetryMaxDelayMs >= 0 {
		cfg.RetryMaxDelay = time.Duration(h.Config.AutoSolver.RetryMaxDelayMs) * time.Millisecond
	}
	if len(h.Config.AutoSolver.Solvers) > 0 {
		configured := make([]string, 0, len(h.Config.AutoSolver.Solvers))
		for _, name := range h.Config.AutoSolver.Solvers {
			trimmed := strings.TrimSpace(name)
			if trimmed != "" {
				configured = append(configured, trimmed)
			}
		}
		if len(configured) > 0 {
			cfg.Solvers = configured
		}
	}
	cfg.LLMFallback = h.Config.AutoSolver.LLMFallback
	creds := h.Config.AutoSolver.Credentials
	cfg.Credentials = coreautosolver.Credentials{
		Login: coreautosolver.LoginCredentials{
			User:     creds.Login.User,
			Password: creds.Login.Password,
		},
		Signup: coreautosolver.SignupCredentials{
			Name:     creds.Signup.Name,
			Email:    creds.Signup.Email,
			Password: creds.Signup.Password,
		},
		Form: coreautosolver.FormCredentials{
			Field1: creds.Form.Field1,
			Field2: creds.Form.Field2,
			Email:  creds.Form.Email,
		},
	}

	cfg.APIKeys = h.autoSolverAPIKeys()

	return cfg
}

// autoSolverAPIKeys pairs each key-gated solver's name with the runtime field that
// carries its key. It is DATA, not the availability rule: which names are gated, and
// what a blank key means, are answered by internal/autosolver and the catalog. The
// pairing has to live here because internal/config cannot be imported from either —
// config imports the catalog, so the arrow only runs this way.
func (h *Handlers) autoSolverAPIKeys() map[string]string {
	if h == nil || h.Config == nil {
		return nil
	}
	return map[string]string{
		coreautosolver.CapsolverSolverName:  h.Config.AutoSolver.CapsolverKey,
		coreautosolver.TwoCaptchaSolverName: h.Config.AutoSolver.TwoCaptchaKey,
	}
}

// llmProviderForAutoSolver returns the configured LLM provider, or nil when no
// provider is configured. The provider is a skeleton today (returns
// "not yet implemented"); wiring it gated on LLMProvider makes the llmFallback
// switch live so it lights up automatically once a real client is implemented.
func (h *Handlers) llmProviderForAutoSolver() coreautosolver.LLMProvider {
	if h == nil || h.Config == nil {
		return nil
	}
	provider := strings.TrimSpace(h.Config.AutoSolver.LLMProvider)
	if provider == "" {
		return nil
	}
	return autosolverllm.NewProvider(autosolverllm.ProviderConfig{Provider: provider})
}

func (h *Handlers) buildAutoSolver(cfg coreautosolver.Config, includeSemantic bool) *coreautosolver.AutoSolver {
	var semanticEngine coreautosolver.SemanticEngine
	if includeSemantic {
		semanticEngine = autosolversemantic.NewAdapter(h.Matcher)
	}

	as := coreautosolver.New(cfg, semanticEngine, h.llmProviderForAutoSolver())
	for _, solver := range catalog.Registrable(cfg) {
		as.Registry().MustRegister(solver)
	}

	return as
}

func (h *Handlers) availableAutoSolverNames() []string {
	cfg := h.normalizedAutoSolverConfig()
	runnable := catalog.Available(cfg)
	available := make(map[string]bool, len(runnable))
	for _, name := range runnable {
		available[name] = true
	}

	names := make([]string, 0, len(available))
	seen := make(map[string]struct{}, len(available))
	for _, configured := range cfg.Solvers {
		if !available[configured] {
			continue
		}
		if _, ok := seen[configured]; ok {
			continue
		}
		names = append(names, configured)
		seen[configured] = struct{}{}
	}

	for _, fallback := range runnable {
		if !available[fallback] {
			continue
		}
		if _, ok := seen[fallback]; ok {
			continue
		}
		names = append(names, fallback)
		seen[fallback] = struct{}{}
	}

	return names
}

func (h *Handlers) isAvailableAutoSolver(name string) bool {
	for _, n := range h.availableAutoSolverNames() {
		if n == name {
			return true
		}
	}
	return false
}

func estimateAutoSolverRunTimeout(cfg coreautosolver.Config) time.Duration {
	attempts := cfg.MaxAttempts
	if attempts < 1 {
		attempts = 1
	}

	// The slowest registered solver bounds an attempt: a solver that times out
	// has spent its task, which ends the run rather than handing on to the next.
	perAttempt := cfg.SolverTimeout
	for _, s := range catalog.Registrable(cfg) {
		perAttempt = max(perAttempt, coreautosolver.SolveTimeoutFor(s, cfg.SolverTimeout))
	}

	timeout := time.Duration(attempts) * perAttempt
	if attempts > 1 {
		timeout += time.Duration(attempts-1) * cfg.RetryMaxDelay
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return timeout + 2*time.Second
}
