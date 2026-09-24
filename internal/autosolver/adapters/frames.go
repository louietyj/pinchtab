package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	cdppage "github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/pinchtab/pinchtab/internal/autosolver"
)

func (p *PinchtabPage) Frames(ctx context.Context) ([]autosolver.FrameRef, error) {
	ctx, cancel := onTab(p.ctx, ctx)
	defer cancel()
	return tabFrames(ctx)
}

func (p *PinchtabPage) EvaluateInFrame(ctx context.Context, frameID, expr string, result any) error {
	ctx, cancel := onTab(p.ctx, ctx)
	defer cancel()
	return evaluateInFrame(ctx, frameID, expr, result)
}

func (e *PinchtabExecutor) Frames(ctx context.Context) ([]autosolver.FrameRef, error) {
	ctx, cancel := onTab(e.ctx, ctx)
	defer cancel()
	return tabFrames(ctx)
}

func (e *PinchtabExecutor) EvaluateInFrame(ctx context.Context, frameID, expr string, result any) error {
	ctx, cancel := onTab(e.ctx, ctx)
	defer cancel()
	return evaluateInFrame(ctx, frameID, expr, result)
}

// onTab runs on the tab's CDP context, which callers such as a solver's
// CanHandle do not carry, cancelled with the caller's context.
func onTab(tab, caller context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(tab)
	if deadline, ok := caller.Deadline(); ok {
		ctx, cancel = context.WithDeadline(tab, deadline)
	}
	stop := context.AfterFunc(caller, cancel)
	return ctx, func() { stop(); cancel() }
}

// tabFrames lists the tab's frames. The page's own frame tree holds only the
// frames in its process; a cross-site frame is a separate "iframe" target,
// found in the browser's target list by following parent targets back to
// this tab. Its frame ID is its target ID.
func tabFrames(ctx context.Context) ([]autosolver.FrameRef, error) {
	var tree *cdppage.FrameTree
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		tree, err = cdppage.GetFrameTree().Do(ctx)
		return err
	})); err != nil {
		return nil, err
	}
	var frames []autosolver.FrameRef
	var walk func(t *cdppage.FrameTree)
	walk = func(t *cdppage.FrameTree) {
		frames = append(frames, autosolver.FrameRef{ID: string(t.Frame.ID), ParentID: string(t.Frame.ParentID), URL: t.Frame.URL + t.Frame.URLFragment})
		for _, child := range t.ChildFrames {
			walk(child)
		}
	}
	walk(tree)
	frames[0].ParentID = ""

	infos, err := chromedp.Targets(ctx)
	if err != nil {
		return frames, nil
	}
	// Chrome fills in parentFrameId on iframe targets, not parentId, so a frame
	// is this tab's when its parent frame already is.
	known := make(map[string]bool, len(frames))
	for _, f := range frames {
		known[f.ID] = true
	}
	for grew := true; grew; {
		grew = false
		for _, info := range infos {
			id := string(info.TargetID)
			if info.Type == "iframe" && !known[id] && known[string(info.ParentFrameID)] {
				known[id] = true
				frames = append(frames, autosolver.FrameRef{ID: id, ParentID: string(info.ParentFrameID), URL: info.URL})
				grew = true
			}
		}
	}
	return frames, nil
}

// evaluateInFrame runs expr in the frame's main world, where the page's own
// globals are: a captcha callback is one. (Bridge.EvaluateInFrame uses an
// isolated world on purpose, so page script cannot interfere with reads.)
func evaluateInFrame(ctx context.Context, frameID, expr string, result any) error {
	var tree *cdppage.FrameTree
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		tree, err = cdppage.GetFrameTree().Do(ctx)
		return err
	})); err != nil {
		return err
	}
	switch {
	case string(tree.Frame.ID) == frameID:
		return chromedp.Run(ctx, chromedp.Evaluate(expr, result))
	case inTree(tree.ChildFrames, cdp.FrameID(frameID)):
		return evaluateInProcessFrame(ctx, cdp.FrameID(frameID), expr, result)
	default:
		return evaluateInFrameTarget(ctx, target.ID(frameID), expr, result)
	}
}

func inTree(frames []*cdppage.FrameTree, id cdp.FrameID) bool {
	for _, f := range frames {
		if f.Frame.ID == id || inTree(f.ChildFrames, id) {
			return true
		}
	}
	return false
}

// evaluateInProcessFrame calls a function on the frame's document node, which
// runs in that document's globals.
func evaluateInProcessFrame(ctx context.Context, frameID cdp.FrameID, expr string, result any) error {
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		owner, _, err := dom.GetFrameOwner(frameID).Do(ctx)
		if err != nil {
			return err
		}
		node, err := dom.DescribeNode().WithBackendNodeID(owner).WithDepth(1).WithPierce(true).Do(ctx)
		if err != nil {
			return err
		}
		if node.ContentDocument == nil {
			return errors.New("frame has no document")
		}
		doc, err := dom.ResolveNode().WithBackendNodeID(node.ContentDocument.BackendNodeID).Do(ctx)
		if err != nil {
			return err
		}
		body := "function(){ return (" + strings.TrimRight(strings.TrimSpace(expr), ";") + "); }"
		res, exc, err := cdpruntime.CallFunctionOn(body).
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

// evaluateInFrameTarget attaches to a cross-site frame's own target.
func evaluateInFrameTarget(ctx context.Context, id target.ID, expr string, result any) error {
	fctx, cancel := chromedp.NewContext(ctx, chromedp.WithTargetID(id))
	defer func() {
		// Cancelling closes the target chromedp was attached to, and closing an
		// iframe target closes the whole tab. With no target ID it only detaches.
		if c := chromedp.FromContext(fctx); c != nil && c.Target != nil {
			c.Target.TargetID = ""
		}
		cancel()
	}()
	return chromedp.Run(fctx, chromedp.Evaluate(expr, result))
}
