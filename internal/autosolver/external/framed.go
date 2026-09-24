package external

import (
	"context"
	"time"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

// A captcha on a page that is itself framed (a payment or signup form in an
// iframe) is invisible to the top document. When the tab can see its frames,
// the providers run their usual read, solve and deliver against the frame
// hosting the widget instead, through these two views of it.

type framedPage struct {
	autosolver.Page
	fe   autosolver.FrameEvaluator
	host autosolver.FrameRef
}

func (p *framedPage) URL() string { return p.host.URL }

func (p *framedPage) HTML() (string, error) { return p.HTMLWithin(0) }

func (p *framedPage) HTMLWithin(timeout time.Duration) (string, error) {
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	var html string
	err := p.fe.EvaluateInFrame(ctx, p.host.ID, "document.documentElement.outerHTML", &html)
	return html, err
}

type framedExecutor struct {
	autosolver.ActionExecutor
	fe     autosolver.FrameEvaluator
	hostID string
}

func (e *framedExecutor) Evaluate(ctx context.Context, expr string, result interface{}) error {
	return e.fe.EvaluateInFrame(ctx, e.hostID, expr, result)
}

// widgetFrame returns the page and executor scoped to the child frame hosting
// a captcha widget, when there is one. executor may be nil (CanHandle).
func widgetFrame(ctx context.Context, page autosolver.Page, executor autosolver.ActionExecutor) (autosolver.Page, autosolver.ActionExecutor, bool) {
	fe, ok := page.(autosolver.FrameEvaluator)
	if !ok {
		return nil, nil, false
	}
	frames, err := fe.Frames(ctx)
	if err != nil {
		return nil, nil, false
	}
	host, _, ok := autosolver.ChildFrameWidget(frames)
	if !ok {
		return nil, nil, false
	}
	fp := &framedPage{Page: page, fe: fe, host: host}
	if executor == nil {
		return fp, nil, true
	}
	xfe, ok := executor.(autosolver.FrameEvaluator)
	if !ok {
		return nil, nil, false
	}
	return fp, &framedExecutor{ActionExecutor: executor, fe: xfe, hostID: host.ID}, true
}
