package bridge

import (
	"context"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/chromedp"
	"github.com/pinchtab/pinchtab/internal/selector"
)

// SetFileInputFiles hands paths on this machine to the input. A cloud browser
// cannot read them, so they reach it through its downloads instead.
func (b *Bridge) SetFileInputFiles(ctx context.Context, backendNodeID int64, paths []string) error {
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return dom.SetFileInputFiles(paths).WithBackendNodeID(cdp.BackendNodeID(backendNodeID)).Do(ctx)
	}))
	if browserRefusesPath(err) {
		return b.setFilesViaDownloads(ctx, backendNodeID, paths)
	}
	return err
}

func (b *Bridge) ResolveSelectorToNodeID(ctx context.Context, raw string, refCache *RefCache, frameID string) (int64, error) {
	return ResolveUnifiedSelectorInFrame(ctx, selector.Parse(raw), refCache, frameID)
}
