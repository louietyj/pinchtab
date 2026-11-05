package bridge

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Regression guard for the lost-deadline bug: a URL download must fail fast on
// a dead browser context instead of blocking (it once navigated on an
// unbounded tab context and could hang until Chrome's own timeouts).
func TestStartURLDownload_FailsFastOnDeadBrowserContext(t *testing.T) {
	parent, cancel := chromedp.NewContext(context.Background())
	cancel()

	b := &Bridge{BrowserCtx: parent}
	b.pageDownloads = newPageDownloads()
	start := time.Now()
	_, err := b.StartURLDownload(context.Background(), "https://example.com/file", DownloadOpts{})
	if err == nil {
		t.Fatal("expected error on dead browser context")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("StartURLDownload blocked for %v on a dead context", elapsed)
	}
}

func TestStartURLDownloadNeedsTheDownloadManager(t *testing.T) {
	_, err := (&Bridge{}).StartURLDownload(context.Background(), "https://example.com/file", DownloadOpts{})
	if !errors.Is(err, ErrPageDownloadsOff) {
		t.Fatalf("err = %v, want ErrPageDownloadsOff", err)
	}
}
