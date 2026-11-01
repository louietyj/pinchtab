package bridge

import (
	"context"
	"encoding/base64"
	"os"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/pinchtab/pinchtab/internal/testbrowser"
)

// Real Chromium, since a mocked executor "succeeds" whether or not the tab
// paints. Headed: PINCHTAB_TEST_HEADED=1 under xvfb-run.
func TestCaptureScreenshotOfBackgroundTabWithoutRaisingIt(t *testing.T) {
	if !unraisedCaptureWorks {
		t.Skip("background tabs can't be captured unraised on this platform")
	}
	chromePath := testbrowser.Path(t)
	profile := testbrowser.ProfileDir(t)
	headed := os.Getenv("PINCHTAB_TEST_HEADED") == "1"

	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.UserDataDir(profile),
		chromedp.Flag("headless", !headed),
		chromedp.Flag("no-sandbox", true),
	)...)
	defer cancelAlloc()

	page1 := "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(`<h1>foreground</h1>`))
	browserCtx, cancelBrowser := chromedp.NewContext(alloc)
	defer cancelBrowser()
	if err := chromedp.Run(browserCtx, chromedp.Navigate(page1)); err != nil {
		t.Fatalf("start browser: %v", err)
	}

	html := `<h1>background tab</h1><script>window.frames_=0;(function f(){frames_++;requestAnimationFrame(f)})()</script>`
	dataURL := "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(html))
	var bgTarget target.ID
	if err := chromedp.Run(browserCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		id, err := target.CreateTarget(dataURL).WithBackground(true).Do(ctx)
		bgTarget = id
		return err
	})); err != nil {
		t.Fatalf("create background target: %v", err)
	}
	bgCtx, cancelBg := chromedp.NewContext(browserCtx, chromedp.WithTargetID(bgTarget))
	defer cancelBg()

	captureCtx, cancelCapture := context.WithTimeout(bgCtx, 10*time.Second)
	defer cancelCapture()
	if err := chromedp.Run(captureCtx); err != nil {
		t.Fatalf("attach to background target: %v", err)
	}
	buf, err := CaptureScreenshot(captureCtx, ScreenshotOpts{Format: page.CaptureScreenshotFormatPng, DisableActivation: true})
	if err != nil {
		t.Fatalf("capture background tab without activation: %v", err)
	}
	if len(buf) == 0 {
		t.Fatal("capture returned no image bytes")
	}

	var fgVisibility string
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(`document.visibilityState`, &fgVisibility)); err != nil {
		t.Fatalf("read foreground visibility: %v", err)
	}
	if headed && fgVisibility != "visible" {
		t.Fatalf("foreground tab visibility = %q after a background capture, want visible (the capture raised the background tab)", fgVisibility)
	}

	var before, after int
	if err := chromedp.Run(captureCtx,
		chromedp.Evaluate(`frames_`, &before),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`frames_`, &after),
	); err != nil {
		t.Fatalf("count frames: %v", err)
	}
	if after-before < 5 {
		t.Fatalf("background tab ran %d animation frames in 500ms after capture, want it rendering (focus emulation left on)", after-before)
	}
}
