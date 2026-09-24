package bridge

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"

	bridgecdpops "github.com/pinchtab/pinchtab/internal/bridge/cdpops"
)

var humanRand = rand.New(rand.NewSource(time.Now().UnixNano()))

func SetHumanRandSeed(seed int64) {
	humanRand = rand.New(rand.NewSource(seed))
}

type Config struct {
	Rand *rand.Rand
}

func (c *Config) getRand() *rand.Rand {
	if c != nil && c.Rand != nil {
		return c.Rand
	}
	return humanRand
}

// MouseMove moves the pointer from (fromX, fromY) to (toX, toY) along a
// ghost-cursor path (see cdpops.HumanMoveBetween).
func MouseMove(ctx context.Context, fromX, fromY, toX, toY float64) error {
	return bridgecdpops.HumanMoveBetween(ctx, fromX, fromY, toX, toY, 0)
}

// approachTarget walks the pointer to (x, y) from where it last was on this
// tab. Best-effort: the trail is only there for human-trail realism, so a
// failure is logged and swallowed and the caller proceeds — its own dispatch
// at (x, y) is what has to land. Only a cancelled outer context is reported
// back.
func approachTarget(ctx context.Context, x, y float64) error {
	if err := bridgecdpops.HumanApproach(ctx, x, y, 0); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		slog.Debug("humanized approach failed, proceeding to action", "err", err)
	}
	return nil
}

var approachTargetAction = approachTarget

func Click(ctx context.Context, x, y float64) error {
	if err := approachTargetAction(ctx, x, y); err != nil {
		return err
	}

	time.Sleep(time.Duration(50+humanRand.Intn(150)) * time.Millisecond)

	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return input.DispatchMouseEvent(input.MousePressed, x, y).
				WithButton(input.Left).
				WithClickCount(1).
				Do(ctx)
		}),
	); err != nil {
		return err
	}

	time.Sleep(time.Duration(30+humanRand.Intn(90)) * time.Millisecond)

	releaseX := x + (humanRand.Float64()-0.5)*2
	releaseY := y + (humanRand.Float64()-0.5)*2

	return chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return input.DispatchMouseEvent(input.MouseReleased, releaseX, releaseY).
				WithButton(input.Left).
				WithClickCount(1).
				Do(ctx)
		}),
	)
}

var scrollIntoViewIfNeededAction = func(ctx context.Context, backendNodeID cdp.BackendNodeID) error {
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return dom.ScrollIntoViewIfNeeded().WithBackendNodeID(backendNodeID).Do(ctx)
	}))
}

var boxModelForBackendNodeAction = func(ctx context.Context, backendNodeID cdp.BackendNodeID) (*dom.BoxModel, error) {
	var box *dom.BoxModel
	err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			box, err = dom.GetBoxModel().WithBackendNodeID(backendNodeID).Do(ctx)
			return err
		}),
	)
	return box, err
}

var (
	clickCoordinateHumanAction = Click
	hoverCoordinateHumanAction = Hover
	settleHoverAction          = HoverByCoordinate
)

// Hover is the humanized sibling of Click: the same random-start bezier
// approach, then the plain hover dispatch instead of press/release.
func Hover(ctx context.Context, x, y float64) error {
	if err := approachTargetAction(ctx, x, y); err != nil {
		return err
	}
	return settleHoverAction(ctx, x, y)
}

// humanPointerPoint resolves the coordinate a humanized pointer action aims at.
// It scrolls the target into the visual viewport first: the humanized path is
// coordinate-based (Input.dispatchMouseEvent at the box-model center), so an
// element below the fold or freshly revealed (lazy-loaded list item,
// modal-triggered button) would otherwise produce coordinates that land on
// whatever is currently visible there — the dispatch "succeeds" but the
// intended handler never fires. Best-effort: not every node scrolls.
func humanPointerPoint(ctx context.Context, backendNodeID cdp.BackendNodeID) (float64, float64, error) {
	_ = scrollIntoViewIfNeededAction(ctx, backendNodeID)

	box, err := boxModelForBackendNodeAction(ctx, backendNodeID)
	if err != nil {
		return 0, 0, err
	}

	if len(box.Content) < 8 {
		return 0, 0, fmt.Errorf("invalid box model")
	}

	centerX := (box.Content[0] + box.Content[2]) / 2
	centerY := (box.Content[1] + box.Content[5]) / 2

	offsetX := (humanRand.Float64() - 0.5) * 10
	offsetY := (humanRand.Float64() - 0.5) * 10

	return centerX + offsetX, centerY + offsetY, nil
}

// The ID is the backendDOMNodeId from the accessibility tree, NOT a regular DOM nodeId.
func ClickElement(ctx context.Context, backendNodeID cdp.BackendNodeID) error {
	x, y, err := humanPointerPoint(ctx, backendNodeID)
	if err != nil {
		return err
	}
	return clickCoordinateHumanAction(ctx, x, y)
}

// The ID is the backendDOMNodeId from the accessibility tree, NOT a regular DOM nodeId.
func HoverElement(ctx context.Context, backendNodeID cdp.BackendNodeID) error {
	x, y, err := humanPointerPoint(ctx, backendNodeID)
	if err != nil {
		return err
	}
	return hoverCoordinateHumanAction(ctx, x, y)
}

func Type(text string, fast bool) []chromedp.Action {
	return TypeWithConfig(text, fast, nil)
}

func TypeWithConfig(text string, fast bool, cfg *Config) []chromedp.Action {
	rng := cfg.getRand()
	actions := []chromedp.Action{}

	baseDelay := 80
	if fast {
		baseDelay = 40
	}

	chars := []rune(text)
	for i, char := range chars {
		actions = append(actions, chromedp.KeyEvent(string(char)))
		delay := baseDelay + rng.Intn(baseDelay/2)
		if rng.Float64() < 0.05 {
			delay += rng.Intn(500)
		}
		if i > 0 && chars[i-1] == char {
			delay = delay / 2
		}
		actions = append(actions, chromedp.Sleep(time.Duration(delay)*time.Millisecond))

		if rng.Float64() < 0.03 && i < len(chars)-1 {
			wrongChar := rune('a' + rng.Intn(26))
			actions = append(actions,
				chromedp.KeyEvent(string(wrongChar)),
				chromedp.Sleep(time.Duration(50+rng.Intn(100))*time.Millisecond),
				chromedp.KeyEvent("\b"),
				chromedp.Sleep(time.Duration(30+rng.Intn(70))*time.Millisecond),
			)
		}
	}
	return actions
}
