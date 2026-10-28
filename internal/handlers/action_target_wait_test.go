package handlers

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/config"
	"github.com/pinchtab/pinchtab/internal/testbrowser"
)

// TestAnActionWaitsForItsTargetToAppear: in a chain like `click "text:From" &&
// click "text:London Stansted"` the option is still animating in when the
// second click resolves, so it must wait for it rather than fail at once.
func TestAnActionWaitsForItsTargetToAppear(t *testing.T) {
	chromePath := testbrowser.Path(t)
	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.UserDataDir(testbrowser.ProfileDir(t)),
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
	)...)
	ctx, cancelBrowser := chromedp.NewContext(alloc)
	ctx, cancelTimeout := context.WithTimeout(ctx, 30*time.Second)
	t.Cleanup(func() {
		cancelTimeout()
		cancelBrowser()
		cancelAlloc()
	})

	html := `<button id="open">Open</button><script>
		setTimeout(() => {
			const b = document.createElement("button");
			b.id = "late";
			b.textContent = "Late option";
			document.body.append(b);
		}, 700);
	</script>`
	if err := chromedp.Run(ctx,
		chromedp.Navigate("data:text/html;base64,"+base64.StdEncoding.EncodeToString([]byte(html))),
		chromedp.WaitVisible("#open", chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}

	cfg := &config.RuntimeConfig{ActionTimeout: 10 * time.Second, DefaultBrowser: config.BrowserChrome, StateDir: t.TempDir()}
	b := bridge.New(context.Background(), ctx, cfg)
	b.RegisterTab("tab-wait", ctx)
	h := New(b, cfg, nil, nil, nil)

	req := bridge.ActionRequest{Selector: "text:Late option"}
	if _, err := h.resolveActionRequestSelector(ctx, "tab-wait", &req); err != nil {
		t.Fatalf("text: target that appears 700ms later: %v", err)
	}
	var id string
	if err := b.CallFunctionOnNode(ctx, req.NodeID, `function() { return this.id; }`, nil, &id); err != nil {
		t.Fatal(err)
	}
	if id != "late" {
		t.Errorf("resolved #%s, want #late", id)
	}

	prev := actionTargetWait
	actionTargetWait = 400 * time.Millisecond
	t.Cleanup(func() { actionTargetWait = prev })
	start := time.Now()
	req = bridge.ActionRequest{Selector: "text:Never there"}
	_, err := h.resolveActionRequestSelector(ctx, "tab-wait", &req)
	if !errors.Is(err, bridge.ErrSelectorNoMatch) {
		t.Fatalf("absent target: err = %v, want ErrSelectorNoMatch", err)
	}
	if elapsed := time.Since(start); elapsed < actionTargetWait || elapsed > actionTargetWait+2*time.Second {
		t.Errorf("absent target failed after %s, want about %s", elapsed, actionTargetWait)
	}
}
