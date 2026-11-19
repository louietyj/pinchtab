package bridge

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/pinchtab/pinchtab/internal/testbrowser"
)

// eBay's captcha page loads hCaptcha's api.js and calls
// hcaptcha.render(el, {callback}) itself; the hook must keep that callback.
func TestVendorCaptureKeepsHCaptchaRenderCallback(t *testing.T) {
	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(testbrowser.Path(t)),
		chromedp.UserDataDir(testbrowser.ProfileDir(t)),
		chromedp.Flag("no-sandbox", true),
	)...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(alloc)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 30*time.Second)
	defer cancelTimeout()

	// The api.js tag (unreachable here) arms the hook; the inline script stands
	// in for what api.js assigns and for the site script that renders.
	html := `<div id="ui"></div><script src="https://js.hcaptcha.invalid/1/api.js?render=explicit" async></script><script>
		window.hcaptcha = {render: function(el, p){ window.rendered = el; return "w1"; }};
		window.widget = hcaptcha.render("ui", {sitekey: "k", callback: function(t){ window.got = t; }});
	</script>`
	var got string
	err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(vendorCaptureScript).Do(ctx)
			return err
		}),
		chromedp.Navigate("data:text/html;base64,"+base64.StdEncoding.EncodeToString([]byte(html))),
		chromedp.Evaluate(`window.widget + "|" + window.rendered + "|" + window.__ptHcaptcha.length + "|" + (window.__ptHcaptcha[0]("TOK"), window.got)`, &got),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != "w1|ui|1|TOK" {
		t.Errorf("render/capture = %q, want w1|ui|1|TOK", got)
	}
}

// Cloudflare's JS detection flags a window.hcaptcha on a page that never loads
// hCaptcha, and its WAF then blocks the browser's POSTs (goodyearrebates.com).
func TestVendorCaptureLeavesNoHCaptchaWithoutScript(t *testing.T) {
	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(testbrowser.Path(t)),
		chromedp.UserDataDir(testbrowser.ProfileDir(t)),
		chromedp.Flag("no-sandbox", true),
	)...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(alloc)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 30*time.Second)
	defer cancelTimeout()

	html := `<script src="https://example.invalid/app.js" async></script><p>form</p>`
	var got bool
	err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(vendorCaptureScript).Do(ctx)
			return err
		}),
		chromedp.Navigate("data:text/html;base64,"+base64.StdEncoding.EncodeToString([]byte(html))),
		chromedp.Evaluate(`"hcaptcha" in window`, &got),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error(`"hcaptcha" in window = true on a page without hCaptcha`)
	}
}
