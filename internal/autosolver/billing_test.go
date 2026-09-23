package autosolver

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func billingTestSolver(solvers ...*mockSolver) *AutoSolver {
	cfg := DefaultConfig()
	cfg.MaxAttempts = 3
	cfg.Solvers = nil
	cfg.RetryBaseDelay = time.Millisecond
	cfg.RetryMaxDelay = time.Millisecond
	as := New(cfg, nil, nil)
	for _, s := range solvers {
		as.Registry().MustRegister(s)
	}
	return as
}

var billingTestPage = &mockPage{url: "https://example.com", title: "Sign up", html: `<div class="captcha">verify you are human</div>`}

func TestSolveStopsAfterASolverSpendsOnAFailedSolve(t *testing.T) {
	paid := &mockSolver{name: "paid", priority: 200, canHandle: true, err: Spent(errors.New("inject token: boom"))}
	fallback := &mockSolver{name: "fallback", priority: 210, canHandle: true}
	as := billingTestSolver(paid, fallback)

	result, err := as.Solve(context.Background(), billingTestPage, &mockExecutor{})
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if paid.solveCalls != 1 {
		t.Errorf("paid solver ran %d times; a spent solve must not be bought again", paid.solveCalls)
	}
	if fallback.solveCalls != 0 {
		t.Errorf("fallback ran %d times after a spent solve", fallback.solveCalls)
	}
	if result.Solved || !strings.Contains(result.Error, "inject token: boom") {
		t.Errorf("result = solved:%v error:%q, want the spent failure reported", result.Solved, result.Error)
	}
}

func TestSolveSkipsAPermanentlyRefusingSolverButTriesTheNext(t *testing.T) {
	refusing := &mockSolver{name: "refusing", priority: 200, canHandle: true, err: Permanent(errors.New("ERROR_ZERO_BALANCE"))}
	next := &mockSolver{name: "next", priority: 210, canHandle: true}
	as := billingTestSolver(refusing, next)

	if _, err := as.Solve(context.Background(), billingTestPage, &mockExecutor{}); err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if refusing.solveCalls != 1 {
		t.Errorf("refusing solver ran %d times across attempts, want 1", refusing.solveCalls)
	}
	if next.solveCalls != 3 {
		t.Errorf("next solver ran %d times, want one per attempt (3)", next.solveCalls)
	}
}

type hintedSolver struct {
	mockSolver
	hint        time.Duration
	gotDeadline time.Duration
}

func (h *hintedSolver) SolveTimeout() time.Duration { return h.hint }
func (h *hintedSolver) Solve(ctx context.Context, p Page, e ActionExecutor) (*Result, error) {
	if dl, ok := ctx.Deadline(); ok {
		h.gotDeadline = time.Until(dl)
	}
	return h.mockSolver.Solve(ctx, p, e)
}

func TestSolverGetsTheLongerTimeoutItAsksFor(t *testing.T) {
	slow := &hintedSolver{mockSolver: mockSolver{name: "people", canHandle: true, solved: true}, hint: time.Hour}
	as := billingTestSolver()
	as.Registry().MustRegister(slow)

	if _, err := as.Solve(context.Background(), billingTestPage, &mockExecutor{}); err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if slow.gotDeadline < 59*time.Minute {
		t.Errorf("solver got %s, want the hour it asked for", slow.gotDeadline)
	}
	if got := SolveTimeoutFor(&mockSolver{}, time.Minute); got != time.Minute {
		t.Errorf("unhinted solver timeout = %s, want the configured minute", got)
	}
}

func TestClassifiedErrorsKeepTheirMessage(t *testing.T) {
	base := errors.New("capsolver createTask error ERROR_KEY_DENIED_ACCESS: bad key")
	for _, err := range []error{Spent(base), Permanent(base)} {
		if err.Error() != base.Error() {
			t.Errorf("classified message = %q, want %q", err.Error(), base.Error())
		}
		if !errors.Is(err, base) {
			t.Error("classified error no longer matches its cause")
		}
	}
	if Spent(nil) != nil || Permanent(nil) != nil {
		t.Error("classifying nil must stay nil")
	}
}
