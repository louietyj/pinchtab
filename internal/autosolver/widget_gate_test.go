package autosolver

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// framedPage is a page whose frames and their owner boxes are scripted. A frame
// with no box is measured as an error.
type framedPage struct {
	mockPage
	frames []FrameRef
	boxes  map[string]FrameBox
	// appearAfter withholds every frame but the top one for this many Frames calls.
	appearAfter int
	calls       int
}

func (p *framedPage) Frames(context.Context) ([]FrameRef, error) {
	p.calls++
	if p.calls <= p.appearAfter {
		return p.frames[:1], nil
	}
	return p.frames, nil
}

func (p *framedPage) EvaluateInFrame(context.Context, string, string, any) error { return nil }

func (p *framedPage) FrameBox(_ context.Context, id string) (FrameBox, error) {
	box, ok := p.boxes[id]
	if !ok {
		return FrameBox{}, errors.New("no owner")
	}
	return box, nil
}

const (
	anchorURL    = "https://www.google.com/recaptcha/api2/anchor?ar=1&k=6Le-wvkSAAAAAPBMRTvw0Q4Muexq9bi0DJwx_mJ-&size=normal"
	invisibleURL = "https://www.google.com/recaptcha/api2/anchor?ar=1&k=6LfMqSEUAAAAAIsy3sFv7uhmtGvsCqFy47OzBlHD&size=invisible&badge=inline"
	bframeURL    = "https://www.google.com/recaptcha/api2/bframe?hl=en&k=6Le-wvkSAAAAAPBMRTvw0Q4Muexq9bi0DJwx_mJ-"
	turnstileURL = "https://challenges.cloudflare.com/cdn-cgi/challenge-platform/h/g/turnstile/f/av0/rch/cglqv/3x00000000000000000000FF/auto/fbE/new/normal?lang=auto"
)

var (
	shown  = FrameBox{Width: 304, Height: 78, Visible: true}
	hidden = FrameBox{Width: 300, Height: 150, Visible: false}
	empty  = FrameBox{Width: 0, Height: 0, Visible: true}
)

func shortWidgetWait(t *testing.T) {
	t.Helper()
	wait, poll := widgetWait, widgetPoll
	widgetWait, widgetPoll = 200*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { widgetWait, widgetPoll = wait, poll })
}

func recaptchaPage(url string, frames []FrameRef, boxes map[string]FrameBox) *framedPage {
	top := FrameRef{ID: "top", URL: url}
	return &framedPage{
		mockPage: mockPage{url: url, title: "Sign up", html: `<div class="g-recaptcha" data-sitekey="6Le-wvkSAAAAAPBMRTvw0Q4Muexq9bi0DJwx_mJ-"></div>`},
		frames:   append([]FrameRef{top}, frames...),
		boxes:    boxes,
	}
}

func TestWidgetGateKeepsADrawnCheckbox(t *testing.T) {
	shortWidgetWait(t)
	page := recaptchaPage("https://site.example/drawn", []FrameRef{{ID: "a", ParentID: "top", URL: anchorURL}}, map[string]FrameBox{"a": shown})
	intent := DetectPageChallenge(context.Background(), page, page.html)
	if intent == nil || intent.ChallengeType != "recaptcha-v2" || intent.Widget == nil || intent.Widget.FrameID != "a" {
		t.Fatalf("got %+v", intent)
	}
}

// An invisible widget, or a puzzle that has not opened, asks a visitor nothing:
// solving it then buys a token and injects it for no reason.
func TestWidgetGateDropsAWidgetShowingNothing(t *testing.T) {
	shortWidgetWait(t)
	closedPuzzle := FrameBox{Width: 400, Height: 580, Top: -10000, Visible: true}
	for _, tc := range []struct {
		name   string
		frames []FrameRef
		boxes  map[string]FrameBox
	}{
		{"invisible badge and a closed puzzle", []FrameRef{{ID: "a", ParentID: "top", URL: invisibleURL}, {ID: "b", ParentID: "top", URL: bframeURL}}, map[string]FrameBox{"a": shown, "b": hidden}},
		{"puzzle parked off the page", []FrameRef{{ID: "a", ParentID: "top", URL: invisibleURL}, {ID: "b", ParentID: "top", URL: bframeURL}}, map[string]FrameBox{"a": shown, "b": closedPuzzle}},
		{"zero-size checkbox", []FrameRef{{ID: "a", ParentID: "top", URL: anchorURL}}, map[string]FrameBox{"a": empty}},
		{"checkout form in a drawn frame", []FrameRef{{ID: "form", ParentID: "top", URL: "https://pay.example/form"}, {ID: "a", ParentID: "form", URL: invisibleURL}}, map[string]FrameBox{"form": {Width: 500, Height: 400, Visible: true}, "a": shown}},
	} {
		page := recaptchaPage("https://site.example/"+strings.ReplaceAll(tc.name, " ", "-"), tc.frames, tc.boxes)
		if intent := DetectPageChallenge(context.Background(), page, page.html); intent != nil {
			t.Errorf("%s: kept as %+v", tc.name, intent)
		}
	}
}

// Epic's account page: invisible hCaptcha, frames present on every visit.
func TestWidgetGateDropsInvisibleHCaptchaUntilItsPuzzleOpens(t *testing.T) {
	shortWidgetWait(t)
	const base = "https://newassets.hcaptcha.com/captcha/v1/317eeece/static/hcaptcha.html"
	html := `<iframe src="` + base + `#frame=checkbox-invisible"></iframe><iframe src="` + base + `#frame=challenge"></iframe>`
	frames := []FrameRef{{ID: "top"}, {ID: "c", ParentID: "top", URL: base + "#frame=checkbox-invisible"}, {ID: "p", ParentID: "top", URL: base + "#frame=challenge"}}
	page := &framedPage{mockPage: mockPage{url: "https://accounts.example/account/personal", title: "Personal", html: html}, frames: frames,
		boxes: map[string]FrameBox{"c": shown, "p": hidden}}
	if intent := DetectPageChallenge(context.Background(), page, html); intent != nil {
		t.Fatalf("invisible widget kept as %+v", intent)
	}
	if showing, _, n, err := WidgetsOnPage(context.Background(), page); err != nil || showing != nil || n != 2 {
		t.Fatalf("WidgetsOnPage = %+v, %d, %v; want nothing showing among 2 frames", showing, n, err)
	}

	page.boxes["p"] = FrameBox{Width: 400, Height: 600, Top: 80, Visible: true}
	intent := DetectPageChallenge(context.Background(), page, html)
	if intent == nil || intent.Widget == nil || intent.Widget.FrameID != "p" {
		t.Fatalf("opened puzzle dropped: %+v", intent)
	}
	if showing, vendor, _, _ := WidgetsOnPage(context.Background(), page); showing == nil || vendor != "hcaptcha" {
		t.Fatalf("WidgetsOnPage missed the open puzzle: %+v %q", showing, vendor)
	}
}

func TestWidgetGateKeepsAnOpenedRecaptchaPuzzle(t *testing.T) {
	shortWidgetWait(t)
	page := recaptchaPage("https://site.example/opened", []FrameRef{{ID: "a", ParentID: "top", URL: invisibleURL}, {ID: "b", ParentID: "top", URL: bframeURL}},
		map[string]FrameBox{"a": shown, "b": {Width: 400, Height: 580, Top: 120, Visible: true}})
	if intent := DetectPageChallenge(context.Background(), page, page.html); intent == nil || intent.Widget == nil || intent.Widget.FrameID != "b" {
		t.Fatalf("got %+v", intent)
	}
}

func TestWidgetGateDropsAWidgetNoVisitorCanReach(t *testing.T) {
	shortWidgetWait(t)
	for _, tc := range []struct {
		name   string
		frames []FrameRef
		boxes  map[string]FrameBox
	}{
		// Carfax: an invisible reCAPTCHA in a 0x0 frame of the site's own.
		{"widget in a 0x0 frame", []FrameRef{{ID: "host", ParentID: "top", URL: "https://site.example/text-me"}, {ID: "a", ParentID: "host", URL: invisibleURL}}, map[string]FrameBox{"a": shown, "host": empty}},
		{"widget two frames into a hidden one", []FrameRef{{ID: "outer", ParentID: "top", URL: "https://site.example/outer"}, {ID: "inner", ParentID: "outer", URL: "https://site.example/inner"}, {ID: "a", ParentID: "inner", URL: anchorURL}},
			map[string]FrameBox{"outer": hidden, "inner": shown, "a": shown}},
	} {
		page := recaptchaPage("https://site.example/"+strings.ReplaceAll(tc.name, " ", "-"), tc.frames, tc.boxes)
		start := time.Now()
		if intent := DetectPageChallenge(context.Background(), page, page.html); intent != nil {
			t.Errorf("%s: kept as %+v", tc.name, intent)
		}
		// Frames that are there but unreachable are a verdict; there is nothing to wait for.
		if d := time.Since(start); d >= widgetWait {
			t.Errorf("%s: waited %v", tc.name, d)
		}
	}
}

func TestWidgetGateWaitsForAWidgetStillBeingCreated(t *testing.T) {
	shortWidgetWait(t)
	page := recaptchaPage("https://site.example/late", []FrameRef{{ID: "a", ParentID: "top", URL: anchorURL}}, map[string]FrameBox{"a": shown})
	page.appearAfter = 3
	if intent := DetectPageChallenge(context.Background(), page, page.html); intent == nil || intent.Widget == nil {
		t.Fatalf("got %+v", intent)
	}
}

func TestWidgetGateWaitsOnceForAWidgetThatNeverComes(t *testing.T) {
	shortWidgetWait(t)
	page := recaptchaPage("https://site.example/never", nil, nil)
	start := time.Now()
	if intent := DetectPageChallenge(context.Background(), page, page.html); intent != nil {
		t.Fatalf("kept as %+v", intent)
	}
	if d := time.Since(start); d < widgetWait {
		t.Errorf("gave up after %v, before widgetWait", d)
	}
	// The next action on the same page checks once instead of waiting again.
	start = time.Now()
	if intent := DetectPageChallenge(context.Background(), page, page.html); intent != nil {
		t.Fatalf("kept as %+v", intent)
	}
	if d := time.Since(start); d >= widgetWait {
		t.Errorf("waited again: %v", d)
	}
}

func TestWidgetGateFailsOpen(t *testing.T) {
	shortWidgetWait(t)
	// The frame is there but its owner cannot be measured.
	page := recaptchaPage("https://site.example/unmeasured", []FrameRef{{ID: "a", ParentID: "top", URL: anchorURL}}, nil)
	if intent := DetectPageChallenge(context.Background(), page, page.html); intent == nil || intent.ChallengeType != "recaptcha-v2" {
		t.Errorf("unmeasurable page lost its detection: %+v", intent)
	}
	// A page that cannot list frames at all keeps it too.
	plain := &mockPage{url: "https://site.example/plain", title: "Sign up", html: page.html}
	if intent := DetectPageChallenge(context.Background(), plain, plain.html); intent == nil {
		t.Error("frameless page lost its detection")
	}
}

func TestWidgetGateLeavesUngatedChallengesAlone(t *testing.T) {
	shortWidgetWait(t)
	for _, tc := range []struct{ name, title, html, want string }{
		{"cloudflare interstitial", "Just a moment...", `<script>window._cf_chl_opt={cvId:'3'}</script><script src="https://challenges.cloudflare.com/turnstile/v0/api.js"></script>`, "turnstile"},
		{"geetest", "Login", `<script src="https://static.geetest.com/v4/gt4.js"></script>`, "geetest"},
	} {
		page := &framedPage{mockPage: mockPage{url: "https://site.example/" + tc.want, title: tc.title, html: tc.html}, frames: []FrameRef{{ID: "top"}}}
		intent := DetectPageChallenge(context.Background(), page, page.html)
		if intent == nil || intent.ChallengeType != tc.want || intent.Widget != nil {
			t.Errorf("%s: got %+v", tc.name, intent)
		}
	}
}

func TestWidgetGateConfirmsAnEmbeddedTurnstile(t *testing.T) {
	shortWidgetWait(t)
	html := `<div class="cf-turnstile" data-sitekey="3x00000000000000000000FF"></div><script src="https://challenges.cloudflare.com/turnstile/v0/api.js"></script>`
	frames := []FrameRef{{ID: "top"}, {ID: "t", ParentID: "top", URL: turnstileURL}}
	drawn := &framedPage{mockPage: mockPage{url: "https://site.example/ts", title: "Demo", html: html}, frames: frames, boxes: map[string]FrameBox{"t": {Width: 300, Height: 65, Visible: true}}}
	if intent := DetectPageChallenge(context.Background(), drawn, html); intent == nil || intent.Widget == nil {
		t.Errorf("drawn turnstile dropped: %+v", intent)
	}
	// Only a script and a class name, and no widget was ever created.
	none := &framedPage{mockPage: mockPage{url: "https://site.example/ts-none", title: "Demo", html: html}, frames: frames[:1]}
	if intent := DetectPageChallenge(context.Background(), none, html); intent != nil {
		t.Errorf("turnstile with no frame kept: %+v", intent)
	}
}

func TestSolveSkipsTheSemanticStageForAGatedWidget(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxAttempts = 1
	semantic := &mockSemantic{
		intent: &Intent{Type: IntentCaptcha, ChallengeType: "recaptcha-v2", Widget: &WidgetFrame{FrameID: "a"}},
		action: &SuggestedAction{Action: ActionClick, Selector: "#continue"},
	}
	solver := &mockSolver{name: "token-solver", priority: 10, canHandle: true, solved: true}
	as := New(cfg, semantic, nil)
	as.Registry().MustRegister(solver)

	result, err := as.Solve(context.Background(), &mockPage{title: "Sign up", url: "https://site.example/"}, &mockExecutor{})
	if err != nil || !result.Solved || result.SolverUsed != "token-solver" {
		t.Fatalf("got %+v, %v", result, err)
	}
	if semantic.suggestCalls != 0 {
		t.Errorf("semantic stage ran %d steps", semantic.suggestCalls)
	}
	for _, h := range result.History {
		if h.Solver == SemanticSolverName {
			t.Errorf("semantic attempt recorded: %+v", h)
		}
	}
}

func TestSemanticClickNeverTargetsAGatedWidget(t *testing.T) {
	intent := &Intent{Type: IntentCaptcha, ChallengeType: "recaptcha-v2", Widget: &WidgetFrame{FrameID: "a"}}
	for _, click := range []*SuggestedAction{
		{Action: ActionClick, Selector: "#continue"},
		{Action: ActionClick, X: 640, Y: 420},
	} {
		if err := challengeClickAllowed(context.Background(), &refusingExecutor{}, intent, click); err == nil {
			t.Errorf("click %+v allowed", click)
		}
	}
}
