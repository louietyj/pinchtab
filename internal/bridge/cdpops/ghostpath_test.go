package cdpops

import (
	"context"
	"math"
	"math/rand"
	"testing"

	"github.com/chromedp/cdproto/input"
)

func TestGhostPathRunsFromStartToEndAndBowsOneWay(t *testing.T) {
	for seed := int64(0); seed < 50; seed++ {
		rng := rand.New(rand.NewSource(seed))
		start, end := pathPoint{100, 400}, pathPoint{700, 300}
		pts := ghostPath(rng, start, end, 0, 0)
		if pts[0] != start || pts[len(pts)-1] != end {
			t.Fatalf("seed %d: path runs %v..%v, want %v..%v", seed, pts[0], pts[len(pts)-1], start, end)
		}
		if len(pts) < 5 {
			t.Fatalf("seed %d: only %d points", seed, len(pts))
		}
		// Signed distance from the straight line: a one-sided curve never
		// crosses it, which is what separates it from jitter around a line.
		dx, dy := end.x-start.x, end.y-start.y
		var left, right bool
		for _, p := range pts[1 : len(pts)-1] {
			cross := dx*(p.y-start.y) - dy*(p.x-start.x)
			left = left || cross > 1e-6*math.Hypot(dx, dy)
			right = right || cross < -1e-6*math.Hypot(dx, dy)
		}
		if left && right {
			t.Errorf("seed %d: path crosses the straight line", seed)
		}
	}
}

func TestGhostMoveOvershootsOnlyLongMoves(t *testing.T) {
	near := ghostMove(rand.New(rand.NewSource(1)), pathPoint{100, 100}, pathPoint{400, 100}, 0)
	if near[len(near)-1] != (pathPoint{400, 100}) {
		t.Errorf("short move ends at %v", near[len(near)-1])
	}
	far := ghostMove(rand.New(rand.NewSource(1)), pathPoint{0, 0}, pathPoint{900, 500}, 0)
	if far[len(far)-1] != (pathPoint{900, 500}) {
		t.Errorf("long move ends at %v, want the exact target", far[len(far)-1])
	}
	// Replay the seed to find the overshoot point; the path must pass through
	// it and then correct.
	replay := rand.New(rand.NewSource(1))
	a, r := replay.Float64()*2*math.Pi, ghostOvershootRadius*math.Sqrt(replay.Float64())
	over := pathPoint{900 + r*math.Cos(a), 500 + r*math.Sin(a)}
	for i, p := range far[:len(far)-1] {
		if p == over {
			if len(far)-i < 3 {
				t.Errorf("only %d points correct from the overshoot", len(far)-i-1)
			}
			return
		}
	}
	t.Errorf("long move never reaches its overshoot point %v", over)
}

func TestHumanMoveBetweenDispatchesThePathAndEndsOnTheTarget(t *testing.T) {
	orig := dispatchRealMouseMoveFunc
	t.Cleanup(func() { dispatchRealMouseMoveFunc = orig })
	var moves []pathPoint
	dispatchRealMouseMoveFunc = func(_ context.Context, x, y float64, b input.MouseButton, mask int64) error {
		if b != input.None || mask != 0 {
			t.Errorf("approach move reports button %v mask %d", b, mask)
		}
		moves = append(moves, pathPoint{x, y})
		return nil
	}
	if err := HumanMoveBetween(context.Background(), 50, 60, 300, 200, 42); err != nil {
		t.Fatal(err)
	}
	if len(moves) < 5 || moves[0] != (pathPoint{50, 60}) || moves[len(moves)-1] != (pathPoint{300, 200}) {
		t.Fatalf("moves = %v", moves)
	}
}
