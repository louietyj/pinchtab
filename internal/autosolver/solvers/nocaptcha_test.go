package solvers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

const punishURL = "https://www.aliexpress.us//item/3256805716460801.html/_____tmd_____/punish?x5secdata=abc&x5step=1"

// ncBrowser is a punish page whose drags fail with the given codes in turn;
// "" passes, which navigates to the item the way AliExpress does.
type ncBrowser struct {
	url      string
	outcomes []string
	blocked  bool
	drags    [][4]float64
	navs     []string
	dragged  bool
	// noSliderUntilNav shows the "Oops" state, with no handle, until a reload.
	noSliderUntilNav bool
}

func (b *ncBrowser) URL() string                              { return b.url }
func (b *ncBrowser) Title() string                            { return "Captcha Interception" }
func (b *ncBrowser) HTML() (string, error)                    { return `<div id="nocaptcha"></div>`, nil }
func (b *ncBrowser) HTMLWithin(time.Duration) (string, error) { return b.HTML() }
func (b *ncBrowser) Screenshot() ([]byte, error)              { return nil, nil }
func (b *ncBrowser) Click(context.Context, float64, float64) error {
	return nil
}
func (b *ncBrowser) Type(context.Context, string) error                   { return nil }
func (b *ncBrowser) WaitFor(context.Context, string, time.Duration) error { return nil }
func (b *ncBrowser) Navigate(_ context.Context, url string) error {
	b.navs = append(b.navs, url)
	b.url = punishURL
	b.noSliderUntilNav = false
	return nil
}
func (b *ncBrowser) Drag(_ context.Context, x, y, endX, endY float64) error {
	b.drags = append(b.drags, [4]float64{x, y, endX, endY})
	b.dragged = true
	if b.outcomes[len(b.drags)-1] == "" {
		b.url = "https://www.aliexpress.us/item/3256805716460801.html"
		return errors.New("Cannot read properties of null (reading 'dispatchEvent')")
	}
	return nil
}

func (b *ncBrowser) Evaluate(_ context.Context, _ string, result interface{}) error {
	st := map[string]any{"h": []float64{572, 466, 42, 30}, "t": []float64{570, 464, 300, 34}}
	if b.blocked {
		st = map[string]any{"blocked": true}
	} else if b.noSliderUntilNav {
		st = map[string]any{"err": "ei5Xq"}
	} else if b.dragged {
		b.dragged = false
		st["err"] = b.outcomes[len(b.drags)-1]
	}
	raw, _ := json.Marshal(st)
	return json.Unmarshal(raw, result)
}

func TestNoCaptchaPassesOnTheFirstDrag(t *testing.T) {
	b := &ncBrowser{url: punishURL, outcomes: []string{""}}
	res, err := (&NoCaptcha{}).Solve(context.Background(), b, b)
	if err != nil || !res.Solved || len(b.drags) != 1 {
		t.Fatalf("Solve = %+v, %v after %d drags", res, err, len(b.drags))
	}
	d := b.drags[0]
	if d[0] < 572 || d[0] > 614 || d[1] < 466 || d[1] > 496 {
		t.Errorf("pressed at (%.0f,%.0f), outside the handle", d[0], d[1])
	}
	if d[2] <= 870 {
		t.Errorf("released at x=%.0f, short of the track's end (870)", d[2])
	}
}

// A refused drag is not retried: no retry within seconds of a refusal has ever
// passed, and each refusal counts against the IP.
func TestNoCaptchaDoesNotRetryARefusedDrag(t *testing.T) {
	b := &ncBrowser{url: punishURL, outcomes: []string{"ZeNz9q", ""}}
	res, err := (&NoCaptcha{}).Solve(context.Background(), b, b)
	if res.Solved || len(b.drags) != 1 || len(b.navs) != 0 || !errors.Is(err, autosolver.ErrPermanent) {
		t.Fatalf("Solve = %+v, %v after %d drags, %d navs", res, err, len(b.drags), len(b.navs))
	}
	if !strings.Contains(res.Error, "ZeNz9q") || !strings.Contains(res.Error, "wait a minute") {
		t.Errorf("error = %q", res.Error)
	}
}

// Run 24: after refused drags the widget sat at "Oops... Please refresh" with
// no handle, and the next attempt gave up on "slider never appeared".
func TestNoCaptchaReloadsWhenTheSliderIsGone(t *testing.T) {
	b := &ncBrowser{url: punishURL, outcomes: []string{""}, noSliderUntilNav: true}
	res, err := (&NoCaptcha{}).Solve(context.Background(), b, b)
	if err != nil || !res.Solved || len(b.navs) != 1 || len(b.drags) != 1 {
		t.Errorf("Solve = %+v, %v after %d navs, %d drags", res, err, len(b.navs), len(b.drags))
	}
}

func TestNoCaptchaDoesNotDragAnOutrightBlock(t *testing.T) {
	b := &ncBrowser{url: punishURL, blocked: true}
	res, err := (&NoCaptcha{}).Solve(context.Background(), b, b)
	if res.Solved || len(b.drags) != 0 || !errors.Is(err, autosolver.ErrPermanent) {
		t.Errorf("Solve = %+v, %v after %d drags", res, err, len(b.drags))
	}
}

func TestPunishPageIsANoCaptchaChallenge(t *testing.T) {
	intent := autosolver.DetectChallengeIntent("Captcha Interception", punishURL, `<div id="nocaptcha"></div>`)
	if intent == nil || intent.ChallengeType != "nocaptcha" {
		t.Errorf("intent = %+v", intent)
	}
	if ok, _ := (&NoCaptcha{}).CanHandle(context.Background(), &ncBrowser{url: "https://www.aliexpress.us/item/1.html"}); ok {
		t.Error("claimed an ordinary item page")
	}
}

// AliExpress also punishes an item page's data API, which shows the punish page
// in a cross-origin iframe over the item. While that iframe is there, the page
// is still challenged, or the run would report it cleared.
func TestPunishFrameOverAnItemPageIsStillAChallenge(t *testing.T) {
	frame := `<div class="pdp"></div><iframe src="https://acs.aliexpress.us:443//h5/mtop.aliexpress.pdp.pc.query/1.0/_____tmd_____/punish?x5secdata=x" style="display:block"></iframe>`
	intent := autosolver.DetectChallengeIntent("", "https://www.aliexpress.us/item/3256812972357268.html", frame)
	if intent == nil || intent.ChallengeType != "punish-frame" {
		t.Errorf("intent = %+v", intent)
	}
	// Baxia's own config can name the path in a script; only an iframe counts.
	if intent := autosolver.DetectChallengeIntent("", "https://www.aliexpress.us/item/1.html", `<script>var p="/_____tmd_____/punish"</script>`); intent != nil {
		t.Errorf("a script mentioning the path was read as %+v", intent)
	}
}

// Run 19's escalation: a blank item page under an iframe of the punish template.
func TestPunishBlockFrameIsReportedBlockedWithoutADrag(t *testing.T) {
	html := `<div id="root"></div><iframe src="https://bixi-intl.alicdn.com/punish/punish:resource:template:AESpace:default_486949.html?qrcode=x&uuid=y"></iframe>`
	b := &ncBrowser{url: "https://www.aliexpress.us/item/3256812246543173.html"}
	page := &htmlPage{ncBrowser: b, html: html}
	if ok, _ := (&NoCaptcha{}).CanHandle(context.Background(), page); !ok {
		t.Fatal("did not claim the punish block")
	}
	res, err := (&NoCaptcha{}).Solve(context.Background(), page, b)
	if res.Solved || len(b.drags) != 0 || !errors.Is(err, autosolver.ErrPermanent) {
		t.Errorf("Solve = %+v, %v after %d drags", res, err, len(b.drags))
	}
}

type htmlPage struct {
	*ncBrowser
	html string
}

func (p *htmlPage) HTML() (string, error) { return p.html, nil }

func TestPunishOrigin(t *testing.T) {
	for in, want := range map[string]string{
		punishURL: "https://www.aliexpress.us/item/3256805716460801.html",
		"https://login.taobao.com/member/login.jhtml": "",
	} {
		if got := punishOrigin(in); got != want {
			t.Errorf("punishOrigin(%q) = %q, want %q", in, got, want)
		}
	}
}
