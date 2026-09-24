package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/pinchtab/pinchtab/internal/bridge"
)

// visionPuzzle is a no-sitekey puzzle nav hands to CapSolver's Vision Engine by
// itself, because no token task solves it. Adding one is a row here, found by
// looking at the puzzle once with `pinchtab vision`.
type visionPuzzle struct {
	name string
	// marker is in the page's HTML when the puzzle's widget is.
	marker string
	// open is clicked to show the puzzle when shown is not visible yet.
	open, shown string
	req         visionRequest
	// passed is a JS expression, true once the puzzle accepted the answer.
	passed string
}

var visionPuzzles = []visionPuzzle{
	{
		// GeeTest v3's slide. Its token task is useless on a live page: the
		// widget has already spent the one-time challenge the task needs.
		name:   "geetest-v3-slide",
		marker: "geetest_radar_tip",
		open:   ".geetest_radar_tip",
		shown:  ".geetest_canvas_bg",
		req: visionRequest{
			Module:     "slider_1",
			Image:      ".geetest_canvas_slice",
			Background: ".geetest_canvas_bg",
			Handle:     ".geetest_slider_button",
		},
		passed: `(function(){ var h = document.querySelector('.geetest_holder'); return !!h && h.className.indexOf('geetest_radar_success') >= 0; })()`,
	},
}

// visionAutoAttempts caps paid Vision calls per puzzle; each is ~$0.002.
const visionAutoAttempts = 3

// matchVisionPuzzle is the puzzle this page carries, if any.
func matchVisionPuzzle(html string) *visionPuzzle {
	for i := range visionPuzzles {
		if strings.Contains(html, visionPuzzles[i].marker) {
			return &visionPuzzles[i]
		}
	}
	return nil
}

// solveVisionPuzzle opens the puzzle if needed, then drags it with Vision's
// answer until it passes, taking a fresh puzzle after each miss.
func (h *Handlers) solveVisionPuzzle(ctx context.Context, tabID string, p *visionPuzzle) autoSolveOutcome {
	outcome := autoSolveOutcome{"solved": false, "challengeType": p.name, "solver": "vision"}
	if h.Config.AutoSolver.CapsolverKey == "" {
		outcome["error"] = "Vision needs a CapSolver key"
		return outcome
	}
	// The clicks, reads and drags run on the tab's CDP context, which the
	// auto-trigger's caller does not carry; the caller still cancels them.
	tab, _, err := h.Bridge.TabContext(tabID)
	if err != nil {
		outcome["error"] = err.Error()
		return outcome
	}
	tabCtx, cancel := context.WithTimeout(tab, 2*time.Minute)
	defer cancel()
	defer context.AfterFunc(ctx, cancel)()
	ctx = tabCtx

	var errs []string
	for attempt := 1; attempt <= visionAutoAttempts; attempt++ {
		outcome["attempts"] = attempt
		if err := h.showVisionPuzzle(ctx, tabID, p); err != nil {
			errs = append(errs, err.Error())
			break
		}
		if _, _, err := h.runVision(ctx, tabID, p.req); err != nil {
			errs = append(errs, err.Error())
		}
		if h.visionPuzzlePassed(ctx, p, 5*time.Second) {
			outcome["solved"] = true
			delete(outcome, "error")
			return outcome
		}
		if len(errs) < attempt {
			errs = append(errs, "the puzzle did not accept the drag")
		}
		// A missed puzzle loads a new one by itself.
		if !sleepCtx(ctx, 2*time.Second) {
			break
		}
	}
	outcome["error"] = strings.Join(errs, "; ")
	slog.Warn("vision puzzle not solved", "puzzle", p.name, "errors", outcome["error"])
	return outcome
}

func (h *Handlers) showVisionPuzzle(ctx context.Context, tabID string, p *visionPuzzle) error {
	visible := func() bool {
		box, err := h.getElementBox(ctx, tabID, p.shown)
		return err == nil && box.Width > 0 && box.Height > 0
	}
	if visible() {
		return nil
	}
	// The opener is often below the fold, and the click is by coordinate.
	_ = h.Bridge.Evaluate(ctx, fmt.Sprintf(`(function(){ var e = document.querySelector(%q); if (e) e.scrollIntoView({block: 'center'}); })()`, p.open), nil, bridge.EvalOpts{})
	box, err := h.getElementBox(ctx, tabID, p.open)
	if err != nil || box.Width == 0 {
		return fmt.Errorf("puzzle opener %s not found", p.open)
	}
	if err := bridge.Click(ctx, box.Left+box.Width/2, box.Top+box.Height/2); err != nil {
		return fmt.Errorf("open puzzle: %w", err)
	}
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		if visible() {
			// The pieces animate into place.
			sleepCtx(ctx, 800*time.Millisecond)
			return nil
		}
		if !sleepCtx(ctx, 300*time.Millisecond) {
			break
		}
	}
	return fmt.Errorf("puzzle %s never appeared", p.shown)
}

func (h *Handlers) visionPuzzlePassed(ctx context.Context, p *visionPuzzle, wait time.Duration) bool {
	for deadline := time.Now().Add(wait); time.Now().Before(deadline); {
		var ok bool
		if err := h.Bridge.Evaluate(ctx, p.passed, &ok, bridge.EvalOpts{}); err == nil && ok {
			return true
		}
		if !sleepCtx(ctx, 300*time.Millisecond) {
			return false
		}
	}
	return false
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
