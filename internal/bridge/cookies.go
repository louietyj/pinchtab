package bridge

import (
	"context"

	"github.com/chromedp/chromedp"
)

// pageContext gives browser-wide calls that CDP only accepts on a page a
// short-lived tab when attached, since an attached BrowserCtx has no page.
func (b *Bridge) pageContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == b.BrowserCtx && b.externalAttachMode() {
		return chromedp.NewContext(ctx)
	}
	return ctx, func() {}
}

// ClearCookies clears all browser cookies.
// This affects all origins and does not require an active tab.
func (b *Bridge) ClearCookies(ctx context.Context) error {
	ctx, cancel := b.pageContext(ctx)
	defer cancel()
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return chromedp.FromContext(ctx).Target.Execute(ctx, "Network.clearBrowserCookies", nil, nil)
	}))
}
