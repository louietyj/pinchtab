package cdpops

import (
	"context"
	"math"
	"math/rand"
	"testing"

	"github.com/chromedp/cdproto/input"
)

func TestHumanDragPlanEndsExactlyOnTheTarget(t *testing.T) {
	for seed := int64(0); seed < 50; seed++ {
		plan := humanDragPlan(rand.New(rand.NewSource(seed)), 100, 300, 342, 300)
		last := plan[len(plan)-1]
		if last.x != 342 || last.y != 300 {
			t.Fatalf("seed %d: plan ends at (%v,%v), want the exact target", seed, last.x, last.y)
		}
	}
}

// The straight constant-speed line is what slider captchas reject.
func TestHumanDragPlanIsNotAStraightConstantSpeedLine(t *testing.T) {
	plan := humanDragPlan(rand.New(rand.NewSource(7)), 100, 300, 342, 300)

	maxOffAxis, maxAlong := 0.0, 0.0
	for _, s := range plan {
		maxOffAxis = math.Max(maxOffAxis, math.Abs(s.y-300))
		maxAlong = math.Max(maxAlong, s.x)
	}
	if maxOffAxis < 0.3 {
		t.Errorf("max drift off the axis = %.2fpx; the path is a straight line", maxOffAxis)
	}
	if maxAlong <= 342 {
		t.Errorf("never passed the target (max x %.1f); a hand overshoots and corrects", maxAlong)
	}

	// Ease-out: the first quarter of the steps covers far more than a quarter
	// of the distance.
	quarter := plan[len(plan)/4].x - 100
	if quarter < 0.4*242 {
		t.Errorf("first quarter of steps covered %.0fpx of 242; the motion does not ease out", quarter)
	}
}

func TestHumanDragHoldsTheButtonAndReleasesOnTheTarget(t *testing.T) {
	origEvent, origMove := dispatchMouseEventFunc, dispatchRealMouseMoveFunc
	t.Cleanup(func() { dispatchMouseEventFunc, dispatchRealMouseMoveFunc = origEvent, origMove })

	var events []map[string]any
	var unheld int
	dispatchMouseEventFunc = func(_ context.Context, payload map[string]any) error {
		events = append(events, payload)
		return nil
	}
	dispatchRealMouseMoveFunc = func(_ context.Context, _, _ float64, b input.MouseButton, mask int64) error {
		if len(events) == 1 && (b != input.Left || mask == 0) {
			unheld++
		}
		return nil
	}

	if err := HumanDragBetweenPoints(context.Background(), 10, 50, 90, 50, ""); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0]["type"] != "mousePressed" || events[1]["type"] != "mouseReleased" {
		t.Fatalf("events = %v, want press then release", events)
	}
	if events[1]["x"] != 90.0 || events[1]["y"] != 50.0 {
		t.Errorf("released at (%v,%v), want the exact target", events[1]["x"], events[1]["y"])
	}
	if unheld != 0 {
		t.Errorf("%d moves between press and release did not report the held button", unheld)
	}
}
