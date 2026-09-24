package cdpops

import (
	"context"
	"math"
	"math/rand"
	"time"
)

// dragStep is one pointer position on a humanized drag and the pause after it.
type dragStep struct {
	x, y  float64
	pause time.Duration
}

// humanDragPlan is the trajectory of one humanized drag. Slider captchas score
// the path, not just the endpoint: a constant-speed straight line is the bot
// signature. This one eases out (fast start, slow finish), drifts slightly off
// the axis, overshoots and corrects, and ends exactly on the target, where the
// release lands. Pure, so it can be checked without a browser.
func humanDragPlan(rng *rand.Rand, x, y, endX, endY float64) []dragStep {
	dx, dy := endX-x, endY-y
	dist := math.Hypot(dx, dy)
	ux, uy := 1.0, 0.0
	if dist > 0 {
		ux, uy = dx/dist, dy/dist
	}

	durationMs := 450 + dist*1.6 + float64(rng.Intn(250))
	steps := min(max(int(durationMs/16), 12), 90)
	overshoot := 0.0
	if dist > 40 {
		overshoot = 3 + rng.Float64()*5
	}
	driftAmp := 0.6 + rng.Float64()*1.4
	driftPhase := rng.Float64() * math.Pi

	plan := make([]dragStep, 0, steps+8)
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		along := (1 - math.Pow(1-t, 3)) * (dist + overshoot)
		drift := driftAmp * math.Sin(math.Pi*t+driftPhase)
		plan = append(plan, dragStep{
			x:     x + ux*along - uy*drift + (rng.Float64()-0.5)*0.8,
			y:     y + uy*along + ux*drift + (rng.Float64()-0.5)*0.8,
			pause: time.Duration(12+rng.Intn(10)) * time.Millisecond,
		})
	}
	if overshoot > 0 {
		from := plan[len(plan)-1]
		n := 3 + rng.Intn(3)
		for i := 1; i <= n; i++ {
			t := float64(i) / float64(n)
			plan = append(plan, dragStep{
				x:     from.x + (endX-from.x)*t,
				y:     from.y + (endY-from.y)*t,
				pause: time.Duration(25+rng.Intn(25)) * time.Millisecond,
			})
		}
	}
	// Settle on the exact target and hold before letting go.
	return append(plan, dragStep{x: endX, y: endY, pause: time.Duration(60+rng.Intn(140)) * time.Millisecond})
}

// HumanDragBetweenPoints approaches (x, y) along a ghost-cursor path, presses,
// moves along humanDragPlan with the button held, and releases exactly at
// (endX, endY). Slider scorers see the approach too: a pointer that appears on
// the handle and presses at once is the bot signature.
func HumanDragBetweenPoints(ctx context.Context, x, y, endX, endY float64, button string) error {
	held, err := heldButton(button)
	if err != nil {
		return err
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	if err := HumanApproach(ctx, x, y, 0); err != nil {
		return err
	}
	// The reaction time between reaching the handle and pressing it.
	if err := sleepCtx(ctx, time.Duration(80+rng.Intn(170))*time.Millisecond); err != nil {
		return err
	}
	if err := dispatchMouseEventFunc(ctx, map[string]any{
		"type": "mousePressed", "button": held.name, "clickCount": 1, "x": x, "y": y,
	}); err != nil {
		return err
	}
	if err := sleepCtx(ctx, time.Duration(40+rng.Intn(70))*time.Millisecond); err != nil {
		return err
	}
	for _, step := range humanDragPlan(rng, x, y, endX, endY) {
		if err := dispatchMouseMove(ctx, step.x, step.y, held.enum, held.held); err != nil {
			return err
		}
		if err := sleepCtx(ctx, step.pause); err != nil {
			return err
		}
	}
	return dispatchMouseEventFunc(ctx, map[string]any{
		"type": "mouseReleased", "button": held.name, "clickCount": 1, "x": endX, "y": endY,
	})
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
