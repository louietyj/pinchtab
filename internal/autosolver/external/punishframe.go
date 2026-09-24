package external

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

// AliExpress (Alibaba's baxia) punishes a page's data API by laying its punish
// page over the page in an iframe. When it picks the reCAPTCHA step, the widget
// is two frames down, rendered explicitly with an action computed at runtime:
//
//	item page
//	└─ *-acs.aliexpress.us/…/_____tmd_____/punish?x5secdata=…            (outer, cross-origin)
//	   └─ …/_____tmd_____/punish?recaptcha=1&iframe=1&…&redirectURL=…    (inner)
//	      └─ www.google.com/recaptcha/enterprise/anchor?k=…&sa=<action>
//
// The inner frame takes the token through its own window.__recaptchaValidateCB__,
// which stores it with Alibaba's second signal (epssw.getParam()) and moves on
// to the page that verifies it. There is no form, so nothing reads the textarea.
var punishFrameRe = regexp.MustCompile(`(?i)<iframe[^>]+src="[^"]*/_____tmd_____/punish`)

// findFrame is the tab's first frame whose URL match accepts.
func findFrame(ctx context.Context, fe autosolver.FrameEvaluator, match func(string) bool) (autosolver.FrameRef, bool) {
	frames, err := fe.Frames(ctx)
	if err != nil {
		return autosolver.FrameRef{}, false
	}
	for _, f := range frames {
		if match(f.URL) {
			return f, true
		}
	}
	return autosolver.FrameRef{}, false
}

func isPunishRecaptchaFrame(u string) bool {
	return strings.Contains(u, "/_____tmd_____/punish") && strings.Contains(u, "recaptcha=1")
}

const punishFrameReadJS = `(function(){
  var a = document.querySelector('iframe[src*="/recaptcha/enterprise/anchor"], iframe[src*="/recaptcha/api2/anchor"]');
  return { href: location.href, anchor: a ? a.src : '' };
})()`

// punishFrameWait covers the inner frame and Google's anchor loading after the
// outer frame is already in the item page.
const punishFrameWait = 10 * time.Second

var errNoFrameAccess = errors.New("executor cannot evaluate in frames")

// readPunishFrame fills the task inputs from the inner frame: the anchor URL
// carries the sitekey (k) and the render action (sa), and the frame's own URL
// is where the widget lives.
func readPunishFrame(ctx context.Context, executor autosolver.ActionExecutor, c *captcha) error {
	fe, ok := executor.(autosolver.FrameEvaluator)
	if !ok {
		return errNoFrameAccess
	}
	deadline := time.Now().Add(punishFrameWait)
	for {
		var got struct{ Href, Anchor string }
		if f, ok := findFrame(ctx, fe, isPunishRecaptchaFrame); ok && fe.EvaluateInFrame(ctx, f.ID, punishFrameReadJS, &got) == nil && got.Anchor != "" {
			a, err := url.Parse(got.Anchor)
			if err != nil {
				return fmt.Errorf("parse reCAPTCHA anchor: %w", err)
			}
			c.key = a.Query().Get("k")
			c.pageAction = a.Query().Get("sa")
			c.enterprise = strings.Contains(a.Path, "/enterprise/")
			c.url = got.Href
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("no reCAPTCHA in the punish frame")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

const punishFrameInjectJS = `(function(token){
  if (typeof window.__recaptchaValidateCB__ !== 'function') return false;
  var ta = document.getElementById('g-recaptcha-response');
  if (ta) ta.value = token;
  window.__recaptchaValidateCB__(token);
  return true;
})`

const punishFrameGoneJS = `!document.querySelector('iframe[src*="/_____tmd_____/punish"]')`

// punishFrameSettle is how long the verify page gets to lift the overlay.
const punishFrameSettle = 20 * time.Second

// injectPunishFrame hands the token to the inner frame's callback, then waits
// for the punish iframe to leave the page: only that shows the token was taken.
func injectPunishFrame(ctx context.Context, executor autosolver.ActionExecutor, _ *captcha, solution json.RawMessage) error {
	var sol tokenSolution
	if err := json.Unmarshal(solution, &sol); err != nil {
		return fmt.Errorf("decode solution: %w", err)
	}
	if sol.token() == "" {
		return errors.New("solution carries no token")
	}
	fe, ok := executor.(autosolver.FrameEvaluator)
	if !ok {
		return errNoFrameAccess
	}
	tok, _ := json.Marshal(sol.token())
	// The callback navigates its own frame away, so the call can fail after it
	// delivered ("Cannot find context"). Whether the overlay leaves is the verdict.
	var delivered bool
	deliverErr := errors.New("no punish frame")
	if f, ok := findFrame(ctx, fe, isPunishRecaptchaFrame); ok {
		deliverErr = fe.EvaluateInFrame(ctx, f.ID, fmt.Sprintf("%s(%s)", punishFrameInjectJS, tok), &delivered)
	}
	if deliverErr == nil && !delivered {
		return errors.New("the punish frame has no __recaptchaValidateCB__")
	}
	deadline := time.Now().Add(punishFrameSettle)
	for {
		var gone bool
		if err := executor.Evaluate(ctx, punishFrameGoneJS, &gone); err == nil && gone {
			return nil
		}
		if time.Now().After(deadline) {
			if deliverErr != nil {
				return fmt.Errorf("deliver token: %w", deliverErr)
			}
			return errors.New("token delivered, but the punish frame stayed up")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}
