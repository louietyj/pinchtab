package observe

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/pinchtab/pinchtab/internal/testbrowser"
)

const hydratedListFixtureHTML = `<body>
<style>.row { content-visibility: auto; contain-intrinsic-size: auto 64px; }</style>
<h2 id="list-title">Search results</h2>
<ul id="issues" role="list" aria-labelledby="list-title"></ul>
<script>
for (const title of ["Hydrated issue alpha", "Hydrated issue beta", "Hydrated issue gamma"]) {
  const row = document.createElement("div");
  row.className = "row";
  row.innerHTML = '<li role="listitem"><h3><a href="#">' + title + '</a></h3></li>';
  document.getElementById("issues").appendChild(row);
}
</script>
</body>`

func newBackgroundTabFixture(t *testing.T, html string) context.Context {
	t.Helper()
	chromePath := testbrowser.Path(t)

	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.UserDataDir(testbrowser.ProfileDir(t)),
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
	)...)
	browserCtx, cancelBrowser := chromedp.NewContext(alloc)
	browserCtx, cancelTimeout := context.WithTimeout(browserCtx, 30*time.Second)
	t.Cleanup(func() {
		cancelTimeout()
		cancelBrowser()
		cancelAlloc()
	})

	var targetID target.ID
	if err := chromedp.Run(browserCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		targetID, err = target.CreateTarget("about:blank").WithFocus(false).Do(ctx)
		return err
	})); err != nil {
		t.Fatal(err)
	}
	tabCtx, cancelTab := chromedp.NewContext(browserCtx, chromedp.WithTargetID(targetID))
	t.Cleanup(cancelTab)

	dataURL := "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(html))
	var visibility string
	if err := chromedp.Run(tabCtx,
		chromedp.Navigate(dataURL),
		chromedp.Evaluate(`document.visibilityState`, &visibility),
	); err != nil {
		t.Fatal(err)
	}
	if visibility != "hidden" {
		t.Fatalf("fixture tab visibilityState = %q, want hidden: the background-tab precondition no longer holds", visibility)
	}
	return tabCtx
}

func TestSnapshotOfAHiddenTabIncludesContentVisibilityAutoRows(t *testing.T) {
	ctx := newBackgroundTabFixture(t, hydratedListFixtureHTML)

	nodes := snapshotNodes(t, ctx)
	want := map[string]bool{"Hydrated issue alpha": false, "Hydrated issue beta": false, "Hydrated issue gamma": false}
	for _, n := range nodes {
		if n.Role == "link" {
			if _, ok := want[n.Name]; ok {
				want[n.Name] = true
			}
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("snapshot of a background tab is missing link %q that sits in a content-visibility:auto row", name)
		}
	}
}
