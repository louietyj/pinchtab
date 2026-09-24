package autosolver

import (
	"context"
	"strings"
	"testing"
)

// refusingExecutor answers the challenge-click check with a fixed refusal.
type refusingExecutor struct {
	mockExecutor
	refusal string
}

func (e *refusingExecutor) Evaluate(_ context.Context, expr string, result interface{}) error {
	if strings.Contains(expr, "is in the page navigation") {
		*result.(*string) = e.refusal
	}
	return nil
}

func TestSemanticClickOnAChallengePageStaysInsideTheChallenge(t *testing.T) {
	click := &SuggestedAction{Action: ActionClick, Selector: "#nav-captcha-solver"}
	captcha := &Intent{Type: IntentCaptcha}

	exec := &refusingExecutor{refusal: "it is in the page navigation"}
	err := challengeClickAllowed(context.Background(), exec, captcha, click)
	if err == nil || !strings.Contains(err.Error(), "#nav-captcha-solver") || !strings.Contains(err.Error(), "navigation") {
		t.Errorf("menu click allowed: %v", err)
	}

	if err := challengeClickAllowed(context.Background(), &refusingExecutor{}, captcha, click); err != nil {
		t.Errorf("click inside the challenge refused: %v", err)
	}

	// Login and signup flows click their own buttons; the guard is not theirs.
	if err := challengeClickAllowed(context.Background(), exec, &Intent{Type: IntentLogin}, click); err != nil {
		t.Errorf("login click refused: %v", err)
	}
}
