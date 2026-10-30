package autosolver

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
)

// The widget gate confirms a text detection for the vendors whose widget is a
// frame: reCAPTCHA, hCaptcha and an embedded Turnstile. Their markers turn up
// on pages with no widget a visitor could meet (a login bundle naming every
// vendor's api.js; Carfax's invisible reCAPTCHA in a 0x0 frame), so a
// detection of one of them stands only while one of the vendor's frames shows
// a visitor something to do: a checkbox, or a puzzle that has opened, drawn on
// the page, in a drawn document. An invisible widget (Epic's account page
// carries invisible hCaptcha on every visit) asks nothing until its puzzle
// opens, and a token bought before then is money and injected JS for nothing;
// the tab's watcher solves it when the puzzle opens. Every other detection
// passes through unchanged: the gate can drop a detection, never add one.

// WidgetFrame is the vendor frame that confirmed a detection.
type WidgetFrame struct {
	FrameID string `json:"frameId"`
	URL     string `json:"url"`
}

// FrameBox is how a frame's owner element is drawn in its parent document.
type FrameBox struct {
	Width, Height float64
	// Top and Left are the owner's offset in its document, scroll included.
	// reCAPTCHA parks its closed puzzle at top:-10000px.
	Top, Left float64
	// Visible is the owner element's checkVisibility(): no display:none,
	// visibility:hidden or zero opacity on it or an ancestor.
	Visible bool
}

func (b FrameBox) drawn() bool {
	return b.Visible && b.Width >= minHostSide && b.Height >= minHostSide &&
		b.Top+b.Height > 0 && b.Left+b.Width > 0
}

// FrameInspector is implemented by a Page that can measure its frames' owner
// elements, cross-origin and in closed shadow roots included.
type FrameInspector interface {
	FrameBox(ctx context.Context, frameID string) (FrameBox, error)
}

// minHostSide excludes the 0x0 and 1x1 frames sites park widgets in.
const minHostSide = 20

var (
	// widgetWait is how long a detection waits for its vendor's frames to be
	// created; hCaptcha took up to 3 s after load in testing.
	widgetWait = 6 * time.Second
	widgetPoll = 300 * time.Millisecond
	// noWidgetTTL is how long a page whose widget never appeared is checked
	// once rather than waited on again, so actions on it don't each pay widgetWait.
	noWidgetTTL = 5 * time.Minute
)

// widgetFrames are each gated vendor's frames: the checkbox or badge, and the
// puzzle it opens.
var widgetFrames = map[string]*regexp.Regexp{
	"recaptcha": regexp.MustCompile(`(?i)^https://(?:www\.google\.com|www\.recaptcha\.net|recaptcha\.net)/recaptcha/(?:api2|enterprise)/(?:anchor|bframe)\?`),
	"hcaptcha":  regexp.MustCompile(`(?i)^https://(?:newassets\.|assets\.)?hcaptcha\.com/captcha/v1/[^#]*#frame=(?:checkbox|challenge)`),
	"turnstile": regexp.MustCompile(`(?i)^https://challenges\.cloudflare\.com/cdn-cgi/challenge-platform/.*/turnstile/`),
}

func isWidgetFrame(vendor, url string) bool {
	re := widgetFrames[vendor]
	return re != nil && re.MatchString(url)
}

// asksSomething reports whether a vendor frame, once drawn, shows a visitor
// something to do. An invisible widget's anchor never does: reCAPTCHA's
// size=invisible badge, hCaptcha's #frame=checkbox-invisible.
func asksSomething(vendor, url string) bool {
	switch vendor {
	case "recaptcha":
		return !invisibleRecaptchaAnchorRe.MatchString(url)
	case "hcaptcha":
		return !strings.Contains(strings.ToLower(url), "#frame=checkbox-invisible")
	}
	return true
}

var invisibleRecaptchaAnchorRe = regexp.MustCompile(`(?i)/anchor\?.*\bsize=invisible\b`)

// gatedVendor is the vendor whose widget must confirm intent, or "".
func gatedVendor(intent *Intent, title, url, html string) string {
	switch intent.ChallengeType {
	case "recaptcha-v2", "recaptcha-in-frame":
		return "recaptcha"
	case "hcaptcha", "hcaptcha-in-frame":
		return "hcaptcha"
	case "turnstile-in-frame":
		return "turnstile"
	case "turnstile":
		if isCloudflareInterstitial(strings.ToLower(title), strings.ToLower(url), strings.ToLower(html)) {
			return ""
		}
		return "turnstile"
	}
	return ""
}

// isCloudflareInterstitial tells Cloudflare's own challenge page, which is
// served in place of the site, from a Turnstile widget the site embeds. Both
// carry challenge-platform frames; only the interstitial carries its options.
func isCloudflareInterstitial(title, url, html string) bool {
	return containsAny(url, "/cdn-cgi/challenge") ||
		cfChallengeTokenRe.MatchString(html) || containsAny(html, "_cf_chl_opt", "cf-browser-verification") ||
		(html == "" && containsAny(title, "just a moment", "attention required", "checking your browser"))
}

// confirmWidget returns intent with its widget frame when one of the vendor's
// frames is showing, or nil when none is. It fails open: a page that cannot be
// measured keeps its detection.
func confirmWidget(ctx context.Context, page Page, intent *Intent, vendor string) *Intent {
	fe, ok1 := page.(FrameEvaluator)
	fi, ok2 := page.(FrameInspector)
	if !ok1 || !ok2 {
		return intent
	}
	url := page.URL()
	key := vendor + " " + url
	deadline := time.Now().Add(widgetWait)
	if noWidgetRecently(key) {
		deadline = time.Now()
	}
	for {
		frames, err := fe.Frames(ctx)
		if err != nil {
			return intent
		}
		found, err := hostedWidget(ctx, fi, frames, vendor)
		if err != nil {
			slog.Debug("autosolver_widget_gate_unmeasured", "vendor", vendor, "url", url, "error", err)
			return intent
		}
		if found.frame != nil {
			confirmed := *intent
			confirmed.Widget = found.frame
			return &confirmed
		}
		if found.candidates > 0 {
			logOverride(intent, url, "none of its frames is showing anything to solve")
			return nil
		}
		if !time.Now().Before(deadline) {
			rememberNoWidget(key)
			logOverride(intent, url, "none of its frames appeared")
			return nil
		}
		select {
		case <-ctx.Done():
			return intent
		case <-time.After(widgetPoll):
		}
	}
}

type widgetSearch struct {
	frame      *WidgetFrame
	candidates int
}

func hostedWidget(ctx context.Context, fi FrameInspector, frames []FrameRef, vendor string) (widgetSearch, error) {
	byID := make(map[string]FrameRef, len(frames))
	for _, f := range frames {
		byID[f.ID] = f
	}
	var s widgetSearch
	for _, f := range frames {
		if f.ParentID == "" || !isWidgetFrame(vendor, f.URL) {
			continue
		}
		s.candidates++
		if !asksSomething(vendor, f.URL) {
			continue
		}
		drawn, err := chainDrawn(ctx, fi, byID, f.ID)
		if err != nil {
			return s, err
		}
		if drawn {
			s.frame = &WidgetFrame{FrameID: f.ID, URL: f.URL}
			return s, nil
		}
	}
	return s, nil
}

// chainDrawn requires a frame, and every frame above it, to be drawn.
func chainDrawn(ctx context.Context, fi FrameInspector, byID map[string]FrameRef, id string) (bool, error) {
	for {
		f, ok := byID[id]
		if !ok || f.ParentID == "" {
			return true, nil
		}
		box, err := fi.FrameBox(ctx, f.ID)
		if err != nil {
			return false, err
		}
		if !box.drawn() {
			return false, nil
		}
		id = f.ParentID
	}
}

// WidgetsOnPage looks over a tab's gated widgets: the vendor frame showing a
// visitor something to do, if any, and how many vendor frames the tab carries
// in all. Frames but nothing showing is an invisible widget, or a puzzle not
// open yet: the tab's watcher keeps looking.
func WidgetsOnPage(ctx context.Context, page Page) (showing *WidgetFrame, vendor string, frames int, err error) {
	fe, ok1 := page.(FrameEvaluator)
	fi, ok2 := page.(FrameInspector)
	if !ok1 || !ok2 {
		return nil, "", 0, nil
	}
	refs, err := fe.Frames(ctx)
	if err != nil {
		return nil, "", 0, err
	}
	for _, v := range gatedVendors {
		found, err := hostedWidget(ctx, fi, refs, v)
		if err != nil {
			return nil, "", 0, err
		}
		frames += found.candidates
		if found.frame != nil {
			return found.frame, v, frames, nil
		}
	}
	return nil, "", frames, nil
}

var gatedVendors = []string{"recaptcha", "hcaptcha", "turnstile"}

func logOverride(intent *Intent, url, why string) {
	slog.Info("autosolver_widget_gate_override",
		"challengeType", intent.ChallengeType,
		"url", url,
		"reason", why)
}

var noWidget = struct {
	sync.Mutex
	at map[string]time.Time
}{at: map[string]time.Time{}}

func noWidgetRecently(key string) bool {
	noWidget.Lock()
	defer noWidget.Unlock()
	at, ok := noWidget.at[key]
	return ok && time.Since(at) < noWidgetTTL
}

func rememberNoWidget(key string) {
	noWidget.Lock()
	defer noWidget.Unlock()
	for k, at := range noWidget.at {
		if time.Since(at) >= noWidgetTTL {
			delete(noWidget.at, k)
		}
	}
	noWidget.at[key] = time.Now()
}
