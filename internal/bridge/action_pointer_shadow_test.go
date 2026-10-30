package bridge

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	bridgecdpops "github.com/pinchtab/pinchtab/internal/bridge/cdpops"
	"github.com/pinchtab/pinchtab/internal/testbrowser"
)

// A design system's button is a web component whose real <button> lives in its
// shadow root (Cigna's <leaf-button>). document.elementFromPoint stops at the
// host, so the button read as covered by its own component; a real overlay on
// top of such a button must still read as covering it.
const shadowPointerFixtureHTML = `<!doctype html><html><body style="margin:0">
<x-button id="plain"></x-button>
<div style="position:relative;margin-top:40px">
  <x-button id="covered"></x-button>
  <div style="position:absolute;inset:0;background:rgba(0,0,0,.4)"></div>
</div>
<script>
for (const host of document.querySelectorAll('x-button')) {
  host.attachShadow({mode:'open'}).innerHTML = '<button style="width:120px;height:40px"><span>Log in</span></button>';
}
</script>
</body></html>`

func TestPointerSeesThroughAWebComponentToItsOwnButton(t *testing.T) {
	chromePath := testbrowser.Path(t)
	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
	)...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(alloc)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 30*time.Second)
	defer cancelTimeout()

	dataURL := "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(shadowPointerFixtureHTML))
	if err := chromedp.Run(ctx, chromedp.Navigate(dataURL)); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		host         string
		wantOccluded bool
	}{
		{host: "plain", wantOccluded: false},
		{host: "covered", wantOccluded: true},
	} {
		t.Run(tc.host, func(t *testing.T) {
			backendNodeID, err := shadowButtonNode(ctx, tc.host)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = bridgecdpops.PointerPointForNode(ctx, backendNodeID, true)
			gotOccluded := errors.Is(err, bridgecdpops.ErrElementOccluded)
			switch {
			case tc.wantOccluded && !gotOccluded:
				t.Errorf("the button under an overlay was accepted (err=%v)", err)
			case !tc.wantOccluded && err != nil:
				t.Errorf("the button inside its own component was refused: %v", err)
			}
		})
	}
}

func shadowButtonNode(ctx context.Context, hostID string) (int64, error) {
	var backendNodeID int64
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		obj, exception, err := runtime.Evaluate(`document.getElementById('` + hostID + `').shadowRoot.querySelector('button')`).Do(ctx)
		if err != nil {
			return err
		}
		if exception != nil {
			return exception
		}
		node, err := dom.DescribeNode().WithObjectID(obj.ObjectID).Do(ctx)
		if err != nil {
			return err
		}
		backendNodeID = int64(node.BackendNodeID)
		return nil
	}))
	return backendNodeID, err
}
