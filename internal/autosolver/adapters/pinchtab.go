// Package adapters provides runtime-specific implementations of the
// autosolver interfaces. The pinchtab adapter is the ONLY place that
// imports chromedp and connects to the Pinchtab bridge runtime.
package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	cdppage "github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/pinchtab/pinchtab/internal/autosolver"
	"github.com/pinchtab/pinchtab/internal/bridge"
)

var (
	_ autosolver.Page           = (*PinchtabPage)(nil)
	_ autosolver.ActionExecutor = (*PinchtabExecutor)(nil)
	_ autosolver.Dragger        = (*PinchtabExecutor)(nil)
	_ autosolver.FrameEvaluator = (*PinchtabExecutor)(nil)
)

// PinchtabPage implements autosolver.Page by wrapping a chromedp tab context.
type PinchtabPage struct {
	ctx   context.Context
	tabID string
	b     bridge.BridgeAPI
}

// NewPinchtabPage creates a Page backed by a Pinchtab bridge tab.
func NewPinchtabPage(ctx context.Context, tabID string, b bridge.BridgeAPI) *PinchtabPage {
	return &PinchtabPage{ctx: ctx, tabID: tabID, b: b}
}

func (p *PinchtabPage) URL() string {
	var url string
	_ = chromedp.Run(p.ctx, chromedp.Location(&url))
	return url
}

func (p *PinchtabPage) Title() string {
	var title string
	_ = chromedp.Run(p.ctx, chromedp.Title(&title))
	return title
}

func (p *PinchtabPage) HTML() (string, error) {
	return p.HTMLWithin(0)
}

// HTMLWithin fetches the outer HTML bounded by timeout (0 = no bound). The
// deadline is derived from the tab context so chromedp keeps the page target
// AND cancels the in-flight CDP command when it fires — a stalled fetch returns
// promptly without leaking a worker goroutine.
func (p *PinchtabPage) HTMLWithin(timeout time.Duration) (string, error) {
	ctx := p.ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(p.ctx, timeout)
		defer cancel()
	}
	var html string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.documentElement.outerHTML`, &html)); err != nil {
		return "", fmt.Errorf("get HTML: %w", err)
	}
	return html, nil
}

func (p *PinchtabPage) Screenshot() ([]byte, error) {
	var buf []byte
	err := chromedp.Run(p.ctx, chromedp.CaptureScreenshot(&buf))
	if err != nil {
		return nil, fmt.Errorf("screenshot: %w", err)
	}
	return buf, nil
}

// PinchtabExecutor implements autosolver.ActionExecutor by delegating
// to the Pinchtab bridge's human-like input system.
type PinchtabExecutor struct {
	ctx   context.Context
	tabID string
	b     bridge.BridgeAPI
}

// NewPinchtabExecutor creates an ActionExecutor backed by a Pinchtab bridge.
func NewPinchtabExecutor(ctx context.Context, tabID string, b bridge.BridgeAPI) *PinchtabExecutor {
	return &PinchtabExecutor{ctx: ctx, tabID: tabID, b: b}
}

func (e *PinchtabExecutor) Click(ctx context.Context, x, y float64) error {
	return bridge.Click(ctx, x, y)
}

func (e *PinchtabExecutor) Drag(ctx context.Context, x, y, endX, endY float64) error {
	return bridge.HumanDragBetweenPoints(ctx, x, y, endX, endY, "")
}

// EvaluateInFrame runs expr in the main world of the first frame, depth-first,
// whose URL match accepts. It calls a function on the frame's document node,
// which runs in that document's own globals. That reaches frames in this page's
// process (same-site ones); a cross-site frame is a separate target, and its
// document is not visible from here.
func (e *PinchtabExecutor) EvaluateInFrame(ctx context.Context, match func(string) bool, expr string, result any) error {
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		tree, err := cdppage.GetFrameTree().Do(ctx)
		if err != nil {
			return err
		}
		frameID := findFrame(tree.ChildFrames, match)
		if frameID == "" {
			return errors.New("no matching frame")
		}
		owner, _, err := dom.GetFrameOwner(frameID).Do(ctx)
		if err != nil {
			return err
		}
		node, err := dom.DescribeNode().WithBackendNodeID(owner).WithDepth(1).WithPierce(true).Do(ctx)
		if err != nil {
			return err
		}
		if node.ContentDocument == nil {
			return errors.New("frame document is out of process")
		}
		doc, err := dom.ResolveNode().WithBackendNodeID(node.ContentDocument.BackendNodeID).Do(ctx)
		if err != nil {
			return err
		}
		res, exc, err := cdpruntime.CallFunctionOn("function(){ return (" + expr + "); }").
			WithObjectID(doc.ObjectID).WithReturnByValue(true).WithAwaitPromise(true).Do(ctx)
		if err != nil {
			return err
		}
		if exc != nil {
			return fmt.Errorf("frame script: %s", exc.Text)
		}
		if result == nil || len(res.Value) == 0 {
			return nil
		}
		return json.Unmarshal(res.Value, result)
	}))
}

func findFrame(frames []*cdppage.FrameTree, match func(string) bool) cdp.FrameID {
	for _, f := range frames {
		if match(f.Frame.URL) {
			return f.Frame.ID
		}
		if id := findFrame(f.ChildFrames, match); id != "" {
			return id
		}
	}
	return ""
}

func (e *PinchtabExecutor) Type(ctx context.Context, text string) error {
	actions := bridge.Type(text, false)
	return chromedp.Run(ctx, actions...)
}

func (e *PinchtabExecutor) WaitFor(ctx context.Context, selector string, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return chromedp.Run(waitCtx, chromedp.WaitVisible(selector))
}

func (e *PinchtabExecutor) Evaluate(ctx context.Context, expr string, result interface{}) error {
	return chromedp.Run(ctx, chromedp.Evaluate(expr, result))
}

func (e *PinchtabExecutor) Navigate(ctx context.Context, url string) error {
	return chromedp.Run(ctx, chromedp.Navigate(url))
}

// NewFromBridge creates both a Page and ActionExecutor from a Bridge
// and tab ID. This is the primary factory for integration with Pinchtab.
func NewFromBridge(b bridge.BridgeAPI, tabID string) (autosolver.Page, autosolver.ActionExecutor, error) {
	tabCtx, resolvedID, err := b.TabContext(tabID)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve tab %q: %w", tabID, err)
	}

	page := NewPinchtabPage(tabCtx, resolvedID, b)
	executor := NewPinchtabExecutor(tabCtx, resolvedID, b)
	return page, executor, nil
}
