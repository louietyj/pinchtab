package external_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/pinchtab/pinchtab/internal/autosolver/external"
	"github.com/pinchtab/pinchtab/internal/testbrowser"
)

// eBay's render callback builds the field its form posts, so injection must
// call it; a callback named both ways is called once.
func TestHCaptchaInjectCallsRenderAndDataCallbacksOnce(t *testing.T) {
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

	html := `<div class="h-captcha" data-callback="onDeclared"></div>
		<textarea name="h-captcha-response"></textarea>
		<script>
		window.calls = [];
		window.onDeclared = function(t){ calls.push("declared:" + t); };
		window.hcaptcha = {getResponse: function(){ return ""; }};
		window.__ptHcaptcha = [function(t){ calls.push("render:" + t); }, "onDeclared"];
		</script>`
	var got string
	err := chromedp.Run(ctx,
		chromedp.Navigate("data:text/html;base64,"+base64.StdEncoding.EncodeToString([]byte(html))),
		chromedp.Evaluate(fmt.Sprintf(`(%s)("TOK","RK")`, external.HCaptchaInjectJS), nil),
		chromedp.Evaluate(`calls.join(",") + "|" + document.querySelector("textarea").value + "|" + hcaptcha.getResponse() + "|" + hcaptcha.getRespKey()`, &got),
	)
	if err != nil {
		t.Fatal(err)
	}
	if want := "render:TOK,declared:TOK|TOK|TOK|RK"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
