package cdpops

import (
	"context"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// The pointer paths below are ported from ghost-cursor 1.4.2
// (https://github.com/Xetera/ghost-cursor, MIT License, Copyright (c) 2020
// Xetera): bezierCurve, generateBezierAnchors and overshoot from src/math.ts,
// path() and GhostCursor.move() from src/spoof.ts. It is the most widely
// copied human-cursor generator, so it is borrowed rather than reinvented.
// ghost-cursor sends a path's points back to back; the pacing here is ours.

type pathPoint struct{ x, y float64 }

const (
	ghostMinSpread          = 2.0
	ghostMaxSpread          = 200.0
	ghostDefaultWidth       = 100.0
	ghostMinSteps           = 25
	ghostOvershootThreshold = 500.0
	ghostOvershootRadius    = 120.0
	ghostOvershootSpread    = 10.0
)

// ghostAnchors picks the curve's two control points, both on the same side of
// the line from a to b, so the path bows one way the way a wrist does.
func ghostAnchors(rng *rand.Rand, a, b pathPoint, spread float64) (pathPoint, pathPoint) {
	side := 1.0
	if rng.Intn(2) == 0 {
		side = -1
	}
	calc := func() pathPoint {
		m := rng.Float64()
		mid := pathPoint{a.x + (b.x-a.x)*m, a.y + (b.y-a.y)*m}
		nx, ny := mid.y-a.y, -(mid.x - a.x)
		if l := math.Hypot(nx, ny); l > 0 {
			nx, ny = nx/l*spread*side, ny/l*spread*side
		}
		k := rng.Float64()
		return pathPoint{mid.x + nx*k, mid.y + ny*k}
	}
	p1, p2 := calc(), calc()
	if p2.x < p1.x {
		p1, p2 = p2, p1
	}
	return p1, p2
}

func cubicAt(t float64, p0, p1, p2, p3 pathPoint) pathPoint {
	u := 1 - t
	a, b, c, d := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
	return pathPoint{a*p0.x + b*p1.x + c*p2.x + d*p3.x, a*p0.y + b*p1.y + c*p2.y + d*p3.y}
}

// ghostPath is ghost-cursor's path(): a one-sided cubic Bezier sampled at a
// step count set by Fitts's law and a random base speed. width is the target's
// width (0 for ghost-cursor's default); spread 0 means the start-to-end
// distance, clamped.
func ghostPath(rng *rand.Rand, start, end pathPoint, width, spread float64) []pathPoint {
	if width <= 0 {
		width = ghostDefaultWidth
	}
	if spread <= 0 {
		spread = min(max(math.Hypot(end.x-start.x, end.y-start.y), ghostMinSpread), ghostMaxSpread)
	}
	p1, p2 := ghostAnchors(rng, start, end, spread)

	length, prev := 0.0, start
	for i := 1; i <= 64; i++ {
		p := cubicAt(float64(i)/64, start, p1, p2, end)
		length += math.Hypot(p.x-prev.x, p.y-prev.y)
		prev = p
	}
	length *= 0.8
	fitts := 2 * math.Log2(length/width+1)
	baseTime := rng.Float64() * ghostMinSteps
	steps := int(math.Ceil((math.Log2(fitts+1) + baseTime) * 3))

	pts := make([]pathPoint, 0, steps+1)
	for i := 0; i <= steps; i++ {
		p := cubicAt(float64(i)/float64(steps), start, p1, p2, end)
		pts = append(pts, pathPoint{max(0, p.x), max(0, p.y)})
	}
	return pts
}

// ghostMove is GhostCursor.move(): a long move lands somewhere within 120px of
// the target first, then corrects along a much flatter curve.
func ghostMove(rng *rand.Rand, from, to pathPoint, width float64) []pathPoint {
	if math.Hypot(to.x-from.x, to.y-from.y) <= ghostOvershootThreshold {
		return ghostPath(rng, from, to, width, 0)
	}
	a := rng.Float64() * 2 * math.Pi
	r := ghostOvershootRadius * math.Sqrt(rng.Float64())
	over := pathPoint{to.x + r*math.Cos(a), to.y + r*math.Sin(a)}
	pts := ghostPath(rng, from, over, width, 0)
	return append(pts, ghostPath(rng, over, to, width, ghostOvershootSpread)[1:]...)
}

// lastPointer is where each target's pointer was last sent, so a humanized
// move starts from there instead of appearing out of nowhere. Entries outlive
// their tabs; each is two floats.
var lastPointer sync.Map

func pointerKey(ctx context.Context) string {
	c := chromedp.FromContext(ctx)
	if c == nil || c.Target == nil {
		return ""
	}
	return string(c.Target.TargetID)
}

func notePointer(ctx context.Context, x, y float64) {
	if k := pointerKey(ctx); k != "" {
		lastPointer.Store(k, pathPoint{x, y})
	}
}

func pointerOf(ctx context.Context) (pathPoint, bool) {
	if k := pointerKey(ctx); k != "" {
		if v, ok := lastPointer.Load(k); ok {
			return v.(pathPoint), true
		}
	}
	return pathPoint{}, false
}

// entryPoint is where a pointer with no known position starts: a random spot
// in the viewport, or 200-500px from the target when the viewport is unknown.
func entryPoint(ctx context.Context, rng *rand.Rand, x, y float64) pathPoint {
	var vp struct{ W, H float64 }
	evalCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := chromedp.Run(evalCtx, chromedp.Evaluate(`({W: innerWidth, H: innerHeight})`, &vp)); err == nil && vp.W > 0 && vp.H > 0 {
		return pathPoint{rng.Float64() * vp.W, rng.Float64() * vp.H}
	}
	a, r := rng.Float64()*2*math.Pi, 200+rng.Float64()*300
	return pathPoint{max(0, x+r*math.Cos(a)), max(0, y+r*math.Sin(a))}
}

// mouseReportInterval is a 125Hz mouse: points due sooner than this after the
// last dispatched one are merged into the next, as the hardware would.
const mouseReportInterval = 8 * time.Millisecond

// HumanMoveBetween moves the pointer from (fromX, fromY) to (toX, toY) along
// a ghost-cursor path at 0.5-1 px/ms (ghost-cursor's timestamp speed). width
// is the target's width, or 0.
func HumanMoveBetween(ctx context.Context, fromX, fromY, toX, toY, width float64) error {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	pts := ghostMove(rng, pathPoint{fromX, fromY}, pathPoint{toX, toY}, width)
	pxPerMs := 0.5 + rng.Float64()*0.5

	start := time.Now()
	var due, sent time.Duration
	for i, p := range pts {
		if i > 0 {
			q := pts[i-1]
			due += time.Duration(math.Hypot(p.x-q.x, p.y-q.y) / pxPerMs * float64(time.Millisecond))
		}
		if i > 0 && i < len(pts)-1 && due-sent < mouseReportInterval {
			continue
		}
		if err := sleepCtx(ctx, time.Until(start.Add(due))); err != nil {
			return err
		}
		if err := dispatchMouseMove(ctx, p.x, p.y, input.None, 0); err != nil {
			return err
		}
		sent = due
	}
	return nil
}

// HumanApproach moves the pointer to (x, y) from wherever it last was on this
// target, or from a random point in the viewport when that is unknown.
func HumanApproach(ctx context.Context, x, y, width float64) error {
	from, ok := pointerOf(ctx)
	if !ok {
		from = entryPoint(ctx, rand.New(rand.NewSource(time.Now().UnixNano())), x, y)
	}
	return HumanMoveBetween(ctx, from.x, from.y, x, y, width)
}
