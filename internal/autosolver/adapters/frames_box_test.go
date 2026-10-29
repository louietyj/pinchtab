package adapters

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/pinchtab/pinchtab/internal/autosolver"
	"github.com/pinchtab/pinchtab/internal/testbrowser"
)

// The page holds one frame per case, each loading /leaf?id=<case> from another
// site (localhost vs 127.0.0.1) so it runs in a process of its own, as vendor
// widgets do. "nested" sits inside a cross-site frame, so its owner is only
// reachable through that frame's target.
const framesBoxPage = `<!doctype html><body>
<iframe id="shown" src="%[1]s/leaf?id=shown" width="304" height="78"></iframe>
<div style="display:none"><iframe src="%[1]s/leaf?id=undisplayed" width="304" height="78"></iframe></div>
<div style="visibility:hidden"><iframe src="%[1]s/leaf?id=unseen" width="304" height="78"></iframe></div>
<div style="opacity:0"><iframe src="%[1]s/leaf?id=transparent" width="304" height="78"></iframe></div>
<iframe src="%[1]s/leaf?id=empty" width="0" height="0" style="border:0"></iframe>
<div id="host"></div>
<iframe src="%[1]s/outer" width="400" height="200"></iframe>
<script>
  const root = document.getElementById('host').attachShadow({mode: 'closed'});
  root.innerHTML = '<iframe src="%[1]s/leaf?id=shadowed" width="304" height="78"></iframe>';
</script>
</body>`

func TestTabFrameBoxMeasuresTheOwnerAcrossProcessesAndShadowRoots(t *testing.T) {
	chromePath := testbrowser.Path(t)

	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == "/outer" {
			fmt.Fprintf(w, `<iframe src="%s/leaf?id=nested" width="304" height="78"></iframe>`, otherSite(r))
			return
		}
		fmt.Fprintf(w, `<p>%s</p>`, r.URL.Query().Get("id"))
	}))
	t.Cleanup(other.Close)
	_, port, _ := net.SplitHostPort(other.Listener.Addr().String())
	crossSite := "http://localhost:" + port

	top := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, framesBoxPage, crossSite)
	}))
	t.Cleanup(top.Close)

	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.UserDataDir(testbrowser.ProfileDir(t)),
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("site-per-process", true),
	)...)
	ctx, cancelBrowser := chromedp.NewContext(alloc)
	ctx, cancelTimeout := context.WithTimeout(ctx, 30*time.Second)
	t.Cleanup(func() {
		cancelTimeout()
		cancelBrowser()
		cancelAlloc()
	})
	if err := chromedp.Run(ctx, chromedp.Navigate(top.URL)); err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{
		"shown": true, "shadowed": true, "nested": true,
		"undisplayed": false, "unseen": false, "transparent": false, "empty": false,
	}
	var frames []autosolver.FrameRef
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		var err error
		if frames, err = TabFrames(ctx); err != nil {
			t.Fatal(err)
		}
		if countLeaves(frames) == len(want) || time.Now().After(deadline) {
			break
		}
	}

	seen := 0
	for _, f := range frames {
		i := strings.Index(f.URL, "/leaf?id=")
		if i < 0 {
			continue
		}
		id := f.URL[i+len("/leaf?id="):]
		seen++
		box, err := TabFrameBox(ctx, f.ID)
		if err != nil {
			t.Errorf("%s: %v", id, err)
			continue
		}
		drawn := box.Visible && box.Width >= 20 && box.Height >= 20
		if drawn != want[id] {
			t.Errorf("%s: measured %+v, want drawn=%v", id, box, want[id])
		}
	}
	if seen != len(want) {
		t.Errorf("found %d of %d leaf frames: %+v", seen, len(want), frames)
	}
}

// otherSite is the site the request did not come in on, so a frame it serves
// is cross-site to its parent.
func otherSite(r *http.Request) string {
	_, port, _ := net.SplitHostPort(r.Host)
	if strings.HasPrefix(r.Host, "localhost") {
		return "http://127.0.0.1:" + port
	}
	return "http://localhost:" + port
}

func countLeaves(frames []autosolver.FrameRef) int {
	n := 0
	for _, f := range frames {
		if strings.Contains(f.URL, "/leaf?id=") {
			n++
		}
	}
	return n
}
