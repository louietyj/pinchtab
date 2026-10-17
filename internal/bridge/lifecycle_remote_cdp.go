package bridge

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/chromedp/cdproto/target"
	bridgeruntime "github.com/pinchtab/pinchtab/internal/bridge/runtime"
	"github.com/pinchtab/pinchtab/internal/config"
	internalurls "github.com/pinchtab/pinchtab/internal/urls"
)

// Caller must hold b.initMu. No profile lock, no launched process; Cleanup
// cancels PinchTab-owned contexts only.
func (b *Bridge) ensureRemoteCDPLocked(cfg *config.RuntimeConfig) error {
	slog.Info("attaching bridge to remote CDP endpoint",
		"provider", config.NormalizeBrowser(cfg.DefaultBrowser),
		"remoteBrowserName", cfg.RemoteBrowserName,
		"cdpUrl", internalurls.RedactForLog(cfg.RemoteCDPURL),
	)

	allocCtx, allocCancel, browserCtx, browserCancel, launchMode, err := bridgeruntime.InitRemoteCDP(context.Background(), cfg, cfg.RemoteCDPURL)
	if err != nil {
		return fmt.Errorf("init remote CDP: %w", err)
	}

	b.AllocCtx = allocCtx
	b.AllocCancel = allocCancel
	b.BrowserCtx = browserCtx
	b.BrowserCancel = browserCancel
	b.initialized = true
	b.stealthLaunchMode = launchMode

	b.reinitWiring(browserCtx, reinitWiringOpts{initActionRegistry: true})

	if err := b.ensureAtLeastOnePageTarget(browserCtx); err != nil {
		slog.Warn("failed to ensure a page target on remote CDP", "err", err)
	}

	return nil
}

func (b *Bridge) ensureAtLeastOnePageTarget(browserCtx context.Context) error {
	// Target.getTargets is browser-level; issuing it on browserCtx's own CDP
	// session ties it to whichever tab chromedp adopted from the remote browser.
	execCtx, err := browserExecutorContext(browserCtx)
	if err != nil {
		return err
	}
	targets, err := target.GetTargets().Do(execCtx)
	if err != nil {
		return fmt.Errorf("list targets: %w", err)
	}
	for _, t := range targets {
		if t.Type == "page" {
			return nil
		}
	}
	if _, _, _, err = b.CreateTab("about:blank"); err != nil {
		return fmt.Errorf("create fallback tab: %w", err)
	}
	return nil
}
