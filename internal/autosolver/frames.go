package autosolver

import (
	"context"
	"regexp"
	"time"
)

// FrameRef is one frame of a tab: the top document (ParentID ""), a frame in
// its process, or a cross-site frame in a process of its own.
type FrameRef struct {
	ID, ParentID, URL string
}

// FrameEvaluator is implemented by a Page or ActionExecutor that can see a
// tab's frames and run JavaScript in the main world of any of them. A captcha
// whose host page is framed (a checkout form in an iframe, AliExpress's punish
// overlay) is invisible to the top document's HTML.
type FrameEvaluator interface {
	// Frames lists the tab's frames, the top document first.
	Frames(ctx context.Context) ([]FrameRef, error)
	// EvaluateInFrame evaluates expr, an expression, in the frame and
	// unmarshals its value into result.
	EvaluateInFrame(ctx context.Context, frameID, expr string, result any) error
}

// vendorFrames are the frames each widget renders itself into, and what they
// tell apart. Their parent is the page the widget is on.
var vendorFrames = []struct {
	typ string
	re  *regexp.Regexp
}{
	{"recaptcha", regexp.MustCompile(`(?i)^https://(?:www\.google\.com|www\.recaptcha\.net|recaptcha\.net)/recaptcha/(?:api2|enterprise)/anchor\?`)},
	{"hcaptcha", regexp.MustCompile(`(?i)^https://(?:newassets\.|assets\.)?hcaptcha\.com/captcha/v1/[^#]*#frame=checkbox`)},
	{"turnstile", regexp.MustCompile(`(?i)^https://challenges\.cloudflare\.com/cdn-cgi/challenge-platform/.*/turnstile/`)},
}

// ChildFrameWidget finds a captcha widget whose page is a frame below the top
// document, returning that host frame and the vendor. A widget on the top
// document is left to the HTML detectors.
func ChildFrameWidget(frames []FrameRef) (host FrameRef, vendor string, ok bool) {
	if len(frames) == 0 {
		return FrameRef{}, "", false
	}
	byID := make(map[string]FrameRef, len(frames))
	for _, f := range frames {
		byID[f.ID] = f
	}
	top := frames[0].ID
	for _, f := range frames {
		for _, v := range vendorFrames {
			if !v.re.MatchString(f.URL) || f.ParentID == "" || f.ParentID == top {
				continue
			}
			if h, found := byID[f.ParentID]; found {
				return h, v.typ, true
			}
		}
	}
	return FrameRef{}, "", false
}

// frameDetectTimeout bounds the frame listing that runs on every navigation.
const frameDetectTimeout = 2 * time.Second

// DetectPageChallenge is DetectChallengeIntent plus captcha widgets hosted in
// a child frame, when the page can list its frames.
func DetectPageChallenge(ctx context.Context, page Page, html string) *Intent {
	if intent := DetectChallengeIntent(page.Title(), page.URL(), html); intent != nil {
		return intent
	}
	fe, ok := page.(FrameEvaluator)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, frameDetectTimeout)
	defer cancel()
	frames, err := fe.Frames(ctx)
	if err != nil {
		return nil
	}
	if _, vendor, ok := ChildFrameWidget(frames); ok {
		return childFrameIntent(vendor)
	}
	return nil
}
