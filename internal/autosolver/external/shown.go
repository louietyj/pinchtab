package external

import (
	"context"
	"html"
	"net/url"
	"strings"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

// shownWidget is the captcha read off the frame the widget gate found showing.
// The page's HTML can name other widgets too: Fanatical loads a v3 scoring key
// (enterprise.js?render=) beside the v2 checkbox it shows, and reading the HTML
// bought a v3 token for the wrong key.
type shownWidget struct {
	typ, key string
	// reCAPTCHA only; the fields after fromAnchor are set only when it is.
	enterprise, fromAnchor bool
	invisible              bool
	pageAction, dataS      string
}

// showingWidget returns the widget the page is showing, if a gated vendor's is.
func showingWidget(ctx context.Context, page autosolver.Page) (shownWidget, bool) {
	frame, vendor, _, err := autosolver.WidgetsOnPage(ctx, page)
	if err != nil || frame == nil {
		return shownWidget{}, false
	}
	switch vendor {
	case "recaptcha":
		return recaptchaShown(ctx, page, frame.URL)
	case "hcaptcha":
		if k := firstSubmatch(frame.URL, frameSitekeyRe); k != "" {
			return shownWidget{typ: "hcaptcha", key: k}, true
		}
	case "turnstile":
		if k := firstSubmatch(frame.URL, turnstileFrameKeyRe); k != "" {
			return shownWidget{typ: "turnstile", key: k}, true
		}
	}
	return shownWidget{}, false
}

// recaptchaShown reads a showing anchor or bframe. A bframe carries only the
// key, so the rest comes from the anchor with that key (invisible v2 shows only
// its bframe).
func recaptchaShown(ctx context.Context, page autosolver.Page, frameURL string) (shownWidget, bool) {
	q := frameQuery(frameURL)
	w := shownWidget{typ: "recaptcha", key: q.Get("k"), enterprise: strings.Contains(strings.ToLower(frameURL), "/recaptcha/enterprise/")}
	if w.key == "" {
		return shownWidget{}, false
	}
	anchor := q
	if !isRecaptchaAnchor(frameURL) {
		anchor = nil
		if fe, ok := page.(autosolver.FrameEvaluator); ok {
			if frames, err := fe.Frames(ctx); err == nil {
				for _, f := range frames {
					if a := frameQuery(f.URL); isRecaptchaAnchor(f.URL) && a.Get("k") == w.key {
						anchor = a
						break
					}
				}
			}
		}
	}
	if anchor != nil {
		w.fromAnchor = true
		w.invisible = strings.EqualFold(anchor.Get("size"), "invisible")
		// CapSolver's docs: the anchor's sa is pageAction, its s the data-s value.
		w.pageAction = anchor.Get("sa")
		w.dataS = anchor.Get("s")
	}
	return w, true
}

func isRecaptchaAnchor(u string) bool {
	return strings.Contains(strings.ToLower(u), "/anchor?")
}

func frameQuery(u string) url.Values {
	parsed, err := url.Parse(html.UnescapeString(u))
	if err != nil {
		return url.Values{}
	}
	return parsed.Query()
}

func vendorOf(typ string) string {
	if strings.HasPrefix(typ, "recaptcha") {
		return "recaptcha"
	}
	return typ
}

// preferShown reports whether the shown widget overrides typ, the HTML's type:
// only for the same vendor or none. AliExpress's punish overlay hosts a
// reCAPTCHA of its own, solved its own way.
func preferShown(typ string, w shownWidget, ok bool) bool {
	return ok && (typ == "" || vendorOf(typ) == vendorOf(w.typ))
}

func (w shownWidget) apply(c *captcha) {
	c.key = w.key
	if w.typ != "recaptcha" {
		return
	}
	c.enterprise = w.enterprise
	if w.fromAnchor {
		c.invisible, c.pageAction, c.dataS = w.invisible, w.pageAction, w.dataS
	}
}
