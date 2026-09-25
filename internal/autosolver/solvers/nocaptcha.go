package solvers

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"time"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

// NoCaptcha passes Alibaba's slide-to-end NoCaptcha (nc.js), the slider behind
// AliExpress and Taobao's "_____tmd_____/punish" interstitial. There is no
// answer to buy: the server scores the drag itself, so this drags the handle
// to the end along a humanized path, once: a refused drag is reported rather
// than retried (see Solve). A slider already spent ("Oops... Please refresh",
// no handle) is replaced by loading the page the interstitial stands in front of.
type NoCaptcha struct{}

// noCaptchaRetryAfter is how long a refused slider is left before another drag:
// the next item page a minute after a refusal has passed.
const noCaptchaRetryAfter = time.Minute

const (
	noCaptchaAttempts   = 3
	noCaptchaMountWait  = 10 * time.Second
	noCaptchaResultWait = 8 * time.Second
	punishMarker        = "/_____tmd_____/"
)

func (s *NoCaptcha) Name() string  { return autosolver.NoCaptchaSolverName }
func (s *NoCaptcha) Priority() int { return 20 }

// SolveTimeout covers three attempts of load, mount, drag and verdict.
func (s *NoCaptcha) SolveTimeout() time.Duration { return 75 * time.Second }

func (s *NoCaptcha) CanHandle(_ context.Context, page autosolver.Page) (bool, error) {
	html, err := page.HTML()
	if err != nil {
		return false, nil
	}
	intent := autosolver.DetectChallengeIntent(page.Title(), page.URL(), html)
	return intent != nil && (intent.ChallengeType == "nocaptcha" || intent.ChallengeType == "punish-block"), nil
}

// ncState is the slider as the page shows it. Rects are [left, top, width,
// height] in CSS pixels, nil while the element is absent or not laid out.
type ncState struct {
	Handle  []float64 `json:"h"`
	Track   []float64 `json:"t"`
	Passed  bool      `json:"ok"`
	ErrCode string    `json:"err"`
	Blocked bool      `json:"blocked"`
}

// The slider mounts a few seconds after the punish page loads, so this is
// polled. "Blocked" is the no-slider refusal ("Sorry, there was a problem
// accessing the page"), which no drag can pass.
const ncStateJS = `(function(){
  function r(e){ if(!e) return null; var b=e.getBoundingClientRect(); return b.width>0 ? [b.left,b.top,b.width,b.height] : null; }
  var h=document.querySelector('#nc_1_n1z, .nc_iconfont.btn_slide');
  var t=document.querySelector('#nc_1_n1t, .nc_scale');
  var txt=(document.body && document.body.innerText) || '';
  var m=txt.match(/error:\s*([A-Za-z0-9]+)/);
  return { h:r(h), t:r(t), ok:!!document.querySelector('.nc_ok, .btn_ok'),
    err: m ? m[1] : '', blocked: !h && !t && /problem accessing the page/i.test(txt) };
})()`

var errNoCaptchaBlocked = errors.New("refused without a slider: this IP is blocked outright")

func (s *NoCaptcha) Solve(ctx context.Context, page autosolver.Page, executor autosolver.ActionExecutor) (*autosolver.Result, error) {
	result := &autosolver.Result{SolverUsed: s.Name()}
	dragger, ok := executor.(autosolver.Dragger)
	if !ok {
		result.Error = "executor cannot drag"
		return result, autosolver.Permanent(errors.New(result.Error))
	}
	if html, err := page.HTML(); err == nil {
		if intent := autosolver.DetectChallengeIntent(page.Title(), page.URL(), html); intent != nil && intent.ChallengeType == "punish-block" {
			result.Error = errNoCaptchaBlocked.Error()
			return result, autosolver.Permanent(errNoCaptchaBlocked)
		}
	}
	origin := punishOrigin(page.URL())

	var codes []string
	for attempt := 1; attempt <= noCaptchaAttempts; attempt++ {
		result.Attempts = attempt
		if attempt > 1 {
			// A failed slider does not reset in place; the page it guards
			// serves a fresh one.
			if origin == "" {
				break
			}
			if err := executor.Navigate(ctx, origin); err != nil {
				result.Error = fmt.Sprintf("reload for a fresh slider: %v", err)
				return result, nil
			}
			if !strings.Contains(page.URL(), punishMarker) {
				return passed(result, page), nil
			}
		}

		st, err := waitNC(ctx, executor, noCaptchaMountWait, func(st ncState) bool {
			// An error code with no handle is a spent slider: no point waiting.
			return st.Blocked || (st.Handle != nil && st.Track != nil) || (st.ErrCode != "" && st.Handle == nil)
		})
		if err != nil {
			result.Error = err.Error()
			return result, err
		}
		if st.Blocked {
			result.Error = errNoCaptchaBlocked.Error()
			return result, autosolver.Permanent(errNoCaptchaBlocked)
		}
		if st.Handle == nil || st.Track == nil {
			// A refused slider can leave "Oops... Please refresh" with no
			// handle; the page it guards serves a new one.
			codes = append(codes, "no slider")
			continue
		}

		// Press somewhere on the handle rather than its exact centre, and let
		// go a little past the track's end.
		h, t := st.Handle, st.Track
		x := h[0] + h[2]*(0.35+0.3*rand.Float64()) // #nosec G404 -- input humanisation, not cryptography.
		y := h[1] + h[3]*(0.35+0.3*rand.Float64()) // #nosec G404
		endX := t[0] + t[2] + 4 + 8*rand.Float64() // #nosec G404
		// A pass navigates away mid-drag, which fails the rest of the drag;
		// the page, not this error, says how it went.
		_ = dragger.Drag(ctx, x, y, endX, y+(rand.Float64()-0.5)*4) // #nosec G404

		st, err = waitNC(ctx, executor, noCaptchaResultWait, func(st ncState) bool {
			return st.Passed || st.ErrCode != "" || st.Blocked
		})
		if err != nil && ctx.Err() != nil {
			result.Error = ctx.Err().Error()
			return result, ctx.Err()
		}
		if (origin != "" && !strings.Contains(page.URL(), punishMarker)) || st.Passed {
			return passed(result, page), nil
		}
		// One drag per slider. Every pass so far came on the first drag, and
		// no retry within seconds of a refusal ever passed (a dozen, across
		// two runs); more refused drags only add to the IP's record. The page
		// passed later instead: the next item, a minute on.
		code := st.ErrCode
		if code == "" {
			code = "no verdict"
		}
		result.FinalURL, result.FinalTitle = page.URL(), page.Title()
		result.Error = fmt.Sprintf("slider refused the drag (error %s)", code)
		return result, autosolver.RetryLater(errors.New(result.Error), noCaptchaRetryAfter)
	}
	result.FinalURL, result.FinalTitle = page.URL(), page.Title()
	result.Error = fmt.Sprintf("no slider to drag after %d loads (%s)", len(codes), strings.Join(codes, ", "))
	return result, nil
}

func passed(result *autosolver.Result, page autosolver.Page) *autosolver.Result {
	result.Solved = true
	result.Error = ""
	result.FinalURL, result.FinalTitle = page.URL(), page.Title()
	return result
}

// waitNC polls the slider until done accepts its state or the wait runs out,
// returning the last state read. Evaluate fails while a pass is navigating,
// so read errors are waited through.
func waitNC(ctx context.Context, executor autosolver.ActionExecutor, wait time.Duration, done func(ncState) bool) (ncState, error) {
	deadline := time.Now().Add(wait)
	var st ncState
	for {
		var cur ncState
		if err := executor.Evaluate(ctx, ncStateJS, &cur); err == nil {
			st = cur
			if done(st) {
				return st, nil
			}
		}
		if time.Now().After(deadline) {
			return st, nil
		}
		select {
		case <-ctx.Done():
			return st, ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

var doubleSlashPath = regexp.MustCompile(`^(https?://[^/]+)/{2,}`)

// punishOrigin is the page a punish interstitial stands in front of:
// https://www.aliexpress.us//item/1.html/_____tmd_____/punish?x5secdata=...
// guards https://www.aliexpress.us/item/1.html. "" when url is not one.
func punishOrigin(url string) string {
	i := strings.Index(url, punishMarker)
	if i < 0 {
		return ""
	}
	return doubleSlashPath.ReplaceAllString(url[:i], "$1/")
}
