package bridge

import (
	"context"
	"fmt"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// ResourceContent returns the bytes of a resource the page has already loaded,
// read from Chrome's cache in whichever frame loaded it. Nothing is fetched, so
// a cross-origin image without CORS headers, or a one-time URL a captcha serves
// its puzzle from, still reads back as the page shows it.
func ResourceContent(ctx context.Context, url string) ([]byte, error) {
	var content []byte
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		tree, err := page.GetFrameTree().Do(ctx)
		if err != nil {
			return err
		}
		var lastErr error
		for _, id := range allFrameIDs(tree) {
			if content, lastErr = page.GetResourceContent(id, url).Do(ctx); lastErr == nil {
				return nil
			}
		}
		return fmt.Errorf("no frame holds %s: %w", url, lastErr)
	}))
	return content, err
}

func allFrameIDs(tree *page.FrameTree) []cdp.FrameID {
	if tree == nil || tree.Frame == nil {
		return nil
	}
	ids := []cdp.FrameID{tree.Frame.ID}
	for _, child := range tree.ChildFrames {
		ids = append(ids, allFrameIDs(child)...)
	}
	return ids
}
