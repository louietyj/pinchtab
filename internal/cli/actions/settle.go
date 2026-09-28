package actions

import (
	"encoding/json"
	"net/http"

	"github.com/pinchtab/pinchtab/internal/cli/apiclient"
)

// domQuietJS resolves true once the DOM has gone 300ms unchanged and unscrolled
// with no busy indicator showing (a CSS spinner awaiting a fetch mutates
// nothing), or false at 2s. A wheel scroll eases for ~1s, in Chrome and in JS
// scrollers like Lenis, without touching the DOM, and positions read mid-ease
// are wrong. A finite CSS animation is busy for the same reason; infinite ones
// never end. Only an indeterminate progressbar counts: a determinate one, like a
// checkout stepper, never goes away. It starts two frames in, or 100ms in a tab
// that paints none. Network idleness is no signal: analytics never stop.
const domQuietJS = `new Promise(function (resolve) {
  var quiet = 300, cap = 2000, t0 = performance.now(), last = t0, started = false;
  function bump() { last = performance.now(); }
  var mo = new MutationObserver(bump);
  mo.observe(document, {subtree: true, childList: true, attributes: true, characterData: true});
  addEventListener('scroll', bump, {capture: true, passive: true});
  function animating() {
    var as = document.getAnimations ? document.getAnimations() : [];
    for (var i = 0; i < as.length; i++) {
      var e = as[i].effect;
      if (as[i].playState === 'running' && e && isFinite(e.getComputedTiming().endTime)) return true;
    }
    return false;
  }
  function busy() {
    if (animating()) return true;
    var els = document.querySelectorAll('[aria-busy="true"], [role="progressbar"]:not([aria-valuenow]), [class*="spinner" i], [class*="loading" i], [class*="loader" i]');
    for (var i = 0; i < els.length; i++) {
      var r = els[i].getBoundingClientRect(), s = getComputedStyle(els[i]);
      if (r.width && r.height && s.visibility !== 'hidden' && s.opacity !== '0' &&
          r.bottom > 0 && r.top < innerHeight) return true;
    }
    return false;
  }
  function check() {
    var now = performance.now(), settled = now - last >= quiet && !busy();
    if (settled || now - t0 >= cap) {
      mo.disconnect();
      removeEventListener('scroll', bump, {capture: true});
      resolve(settled);
    }
    else setTimeout(check, 50);
  }
  function start() { if (!started) { started = true; check(); } }
  requestAnimationFrame(function () { requestAnimationFrame(start); });
  setTimeout(start, 100);
})`

// settleDOM waits for the page to stop changing, so what is read or captured
// next is what the action produced, not a frame of it re-rendering. It is best
// effort: a failed wait, such as a navigation mid-wait, reads as settled.
func settleDOM(client *http.Client, base, token, tabID string) bool {
	path := "/evaluate"
	if tabID != "" {
		path = "/tabs/" + tabID + "/evaluate"
	}
	raw, err := apiclient.DoPostRawE(client, base, token, path, map[string]any{
		"expression":   domQuietJS,
		"awaitPromise": true,
	})
	if err != nil {
		return true
	}
	var resp struct {
		Result any `json:"result"`
	}
	if json.Unmarshal(raw, &resp) != nil {
		return true
	}
	settled, ok := resp.Result.(bool)
	return !ok || settled
}
