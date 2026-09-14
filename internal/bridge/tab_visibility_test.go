package bridge

import (
	"context"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/pinchtab/pinchtab/internal/config"
	"github.com/pinchtab/pinchtab/internal/testbrowser"
)

// Real-Chromium test: headless Chromium reports a tab opened with focus=false as
// hidden, which page-visibility checks read as a background tab. A mocked CDP
// executor has no visibility state to observe.
func TestHeadlessCreatedTabIsVisible(t *testing.T) {
	chromePath := testbrowser.Path(t)

	for _, tc := range []struct {
		name     string
		headless bool
		want     string
	}{
		{"headless config brings the tab forward", true, "visible"},
		{"without it the same tab stays hidden", false, "hidden"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), append(
				chromedp.DefaultExecAllocatorOptions[:],
				chromedp.ExecPath(chromePath),
				chromedp.UserDataDir(testbrowser.ProfileDir(t)),
				chromedp.Flag("headless", "new"),
				chromedp.Flag("no-sandbox", true),
			)...)
			defer cancelAlloc()

			browserCtx, cancelBrowser := chromedp.NewContext(alloc)
			defer cancelBrowser()
			if err := chromedp.Run(browserCtx); err != nil {
				t.Fatalf("start browser: %v", err)
			}

			tm := NewTabManager(browserCtx, &config.RuntimeConfig{Headless: tc.headless}, nil, nil, nil)
			_, tabCtx, cancelTab, err := tm.createTab("", "")
			if err != nil {
				t.Fatalf("create tab: %v", err)
			}
			defer cancelTab()

			var state string
			if err := chromedp.Run(tabCtx, chromedp.Evaluate(`document.visibilityState`, &state)); err != nil {
				t.Fatalf("read visibilityState: %v", err)
			}
			if state != tc.want {
				t.Fatalf("visibilityState = %q, want %q", state, tc.want)
			}
		})
	}
}
