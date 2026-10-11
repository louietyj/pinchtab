package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/pinchtab/pinchtab/internal/config"
)

// normalizeAttachURL applies the attach allowlist (loopback always allowed;
// non-loopback hosts must be in security.attach.allowHosts) and host-pins the
// URL to the resolved IP so the dial can't be re-routed by a second DNS answer.
func normalizeAttachURL(rawURL string, cfg *config.RuntimeConfig) (string, error) {
	return NormalizeCDPURLWithAllowlist(rawURL, cfg.AttachAllowHosts, cfg.AttachAllowSchemes)
}

// newRemoteCDPContexts creates a chromedp remote allocator + browser context for
// an already-normalized (pinned) CDP URL. ctx==nil defaults to Background.
func newRemoteCDPContexts(ctx context.Context, normalizedURL string) (
	allocCtx context.Context, allocCancel context.CancelFunc,
	browserCtx context.Context, browserCancel context.CancelFunc,
) {
	if ctx == nil {
		ctx = context.Background()
	}
	allocCtx, allocCancel = chromedp.NewRemoteAllocator(ctx, normalizedURL)
	browserCtx, browserCancel = chromedp.NewContext(allocCtx)
	return
}

// Chrome holds each DevTools connection until its "Allow remote debugging?"
// prompt is answered; chromedp's 10s default re-dialed, re-prompting each time.
const attachDialTimeout = 2 * time.Minute

// ConnectBrowser connects browserCtx without the about:blank tab a first
// chromedp.Run would open, which nothing ever closes in a browser we don't own.
func ConnectBrowser(browserCtx context.Context) error {
	c := chromedp.FromContext(browserCtx)
	if c == nil || c.Allocator == nil {
		return chromedp.ErrInvalidContext
	}
	if c.Browser != nil {
		return nil
	}
	b, err := c.Allocator.Allocate(browserCtx, chromedp.WithDialTimeout(attachDialTimeout))
	if err != nil {
		return fmt.Errorf("connect to browser: %w", err)
	}
	c.Browser = b
	return nil
}
