package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/pinchtab/pinchtab/internal/bridge"
)

// A tab's settle state lives in the page, in PinchTab's isolated world, so the
// page cannot see or disturb it and it lasts exactly as long as the document:
// an observer that records the last DOM change or scroll, and the anchor, the
// moment the last action ended or the last wait condition passed.
//
// A DOM-quiet wait looks back to the anchor rather than starting from scratch.
// It passes once nothing has changed for 300ms since the later of the last
// change and the anchor, or once its timeout has run since the anchor. So a
// second DOM-quiet wait right after the first, or after a step's own settle,
// passes at once, and a longer one only waits out the difference. A fresh
// document has no state; the first call installs it with the anchor at that
// moment, which is just after the navigation that loaded it.
const domQuietWindowMs = 300

// domQuietScript returns the expression for op: "mark" moves the anchor to now,
// "check" reports the state, "reported" records that an unsettled result was
// shown to the caller, so the next wait on the same anchor stays silent.
//
// A visible busy indicator or a running finite animation (a wheel scroll's
// ease, a panel sliding in) also counts as change: a CSS spinner, or a
// skeleton placeholder, awaiting a fetch mutates nothing (data.census.gov's
// results). Only an indeterminate progressbar counts; a determinate one, like
// a checkout stepper, never goes away.
func domQuietScript(op string) string {
	return fmt.Sprintf(`(function (op, quiet) {
  var s = window.__pinchtabQuiet, now = Date.now();
  if (!s) {
    s = {last: now, anchor: now, reported: false};
    var bump = function () { s.last = Date.now(); };
    new MutationObserver(bump).observe(document, {subtree: true, childList: true, attributes: true, characterData: true});
    addEventListener('scroll', bump, {capture: true, passive: true});
    window.__pinchtabQuiet = s;
  }
  if (op === 'mark') { s.anchor = now; s.reported = false; return {}; }
  if (op === 'reported') { s.reported = true; return {}; }
  function busy() {
    var as = document.getAnimations ? document.getAnimations() : [];
    for (var i = 0; i < as.length; i++) {
      var e = as[i].effect;
      if (as[i].playState === 'running' && e && isFinite(e.getComputedTiming().endTime)) return true;
    }
    var els = document.querySelectorAll('[aria-busy="true"], [role="progressbar"]:not([aria-valuenow]), [class*="spinner" i], [class*="loading" i], [class*="loader" i], [class*="skeleton" i], [class*="shimmer" i]');
    for (var j = 0; j < els.length; j++) {
      var r = els[j].getBoundingClientRect(), st = getComputedStyle(els[j]);
      if (r.width && r.height && st.visibility !== 'hidden' && st.opacity !== '0' && r.bottom > 0 && r.top < innerHeight) return true;
    }
    return false;
  }
  return {settled: now - Math.max(s.last, s.anchor) >= quiet && !busy(), age: now - s.anchor, reported: s.reported};
})(%q, %d)`, op, domQuietWindowMs)
}

type domQuietState struct {
	Settled  bool  `json:"settled"`
	Age      int64 `json:"age"`
	Reported bool  `json:"reported"`
}

const domQuietPoll = 50 * time.Millisecond

// markSettleAnchor moves the tab's anchor to now. Best effort: the page may be
// mid-navigation, and a missing anchor only means the next wait starts fresh.
func markSettleAnchor(ctx context.Context) {
	mctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_ = bridge.EvaluateInIsolatedWorld(mctx, "", domQuietScript("mark"), nil)
}

// waitDOMQuiet polls the tab's settle state from here rather than with timers
// in the page, which a background tab throttles to one a second. It reports
// whether the page settled and, when it did not, whether an earlier wait on
// the same anchor already said so. A failed check, such as a navigation
// mid-wait, reads as settled.
func waitDOMQuiet(ctx context.Context, timeout time.Duration, report bool) (settled, alreadyReported bool) {
	deadline := time.Now().Add(timeout + 2*time.Second)
	for {
		var st domQuietState
		if err := bridge.EvaluateInIsolatedWorld(ctx, "", domQuietScript("check"), &st); err != nil {
			return true, false
		}
		if st.Settled {
			return true, false
		}
		if time.Duration(st.Age)*time.Millisecond >= timeout || time.Now().After(deadline) {
			if report && !st.Reported {
				_ = bridge.EvaluateInIsolatedWorld(ctx, "", domQuietScript("reported"), nil)
			}
			return false, st.Reported
		}
		select {
		case <-ctx.Done():
			return true, false
		case <-time.After(domQuietPoll):
		}
	}
}
