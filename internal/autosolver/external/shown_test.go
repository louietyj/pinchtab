package external

import (
	"context"
	"testing"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

// shownTab is a page whose frames can be listed and measured: every frame is
// drawn, so the widget gate sees each asking frame as showing.
type shownTab struct {
	fakePage
	frames []autosolver.FrameRef
}

func (t *shownTab) Frames(context.Context) ([]autosolver.FrameRef, error) { return t.frames, nil }

func (t *shownTab) EvaluateInFrame(context.Context, string, string, any) error { return nil }

func (t *shownTab) FrameBox(context.Context, string) (autosolver.FrameBox, error) {
	return autosolver.FrameBox{Width: 300, Height: 80, Top: 100, Left: 100, Visible: true}, nil
}

const (
	fanaticalV3Key = "6LfGZ74pAAAAACfpKy3U-ba7y9HWKQcyoXampSzX"
	fanaticalV2Key = "6LfQZ74pAAAAADXOpGBI4YVHRkm_Yb377d1GCdwP"
)

// fanaticalLogin is Fanatical's login after it asks for a check: v3 Enterprise
// scores every visit through a render= key and its invisible anchor, and the
// check is a v2 checkbox with a key of its own.
func fanaticalLogin() *shownTab {
	return &shownTab{
		fakePage: fakePage{
			url:  "https://www.fanatical.com/en/game/letter-quest",
			html: `<script src="https://www.google.com/recaptcha/enterprise.js?render=` + fanaticalV3Key + `"></script>`,
		},
		frames: []autosolver.FrameRef{
			{ID: "top", URL: "https://www.fanatical.com/en/game/letter-quest"},
			{ID: "v3", ParentID: "top", URL: "https://www.google.com/recaptcha/enterprise/anchor?ar=1&k=" + fanaticalV3Key + "&size=invisible&cb=a"},
			{ID: "v2", ParentID: "top", URL: "https://www.google.com/recaptcha/enterprise/anchor?ar=1&k=" + fanaticalV2Key + "&size=normal&sa=LOGIN&s=DATA-S&cb=b"},
		},
	}
}

func TestSolveBuysForTheShownCheckboxNotTheScoringKey(t *testing.T) {
	var task capsolverTask
	srv := mockCapsolver(t, "V2-TOKEN", &task)
	defer srv.Close()
	c := NewCapsolver(CapsolverConfig{APIKey: "k", BaseURL: srv.URL})
	tab := fanaticalLogin()

	if res, err := c.Solve(context.Background(), tab, &fakeExecutor{}); err != nil || !res.Solved {
		t.Fatalf("Solve = %+v, %v", res, err)
	}
	if task.Type != "ReCaptchaV2EnterpriseTaskProxyLess" || task.WebsiteKey != fanaticalV2Key {
		t.Errorf("task = %s for %s; want ReCaptchaV2EnterpriseTaskProxyLess for the checkbox key", task.Type, task.WebsiteKey)
	}
	if task.PageAction != "LOGIN" || task.IsInvisible || task.EnterprisePayload["s"] != "DATA-S" {
		t.Errorf("task = %+v; want the anchor's sa as pageAction, its s, and not invisible", task)
	}
}

func TestInvisibleV2ReadsItsAnchorWhenOnlyThePuzzleShows(t *testing.T) {
	const key = "6LcINVISIBLEKEYxxxxxxxxxxxxxxxxxxxxxxxxx"
	tab := &shownTab{frames: []autosolver.FrameRef{
		{ID: "top", URL: "https://shop.example/"},
		{ID: "anchor", ParentID: "top", URL: "https://www.google.com/recaptcha/api2/anchor?k=" + key + "&size=invisible&sa=submit"},
		{ID: "bframe", ParentID: "top", URL: "https://www.google.com/recaptcha/api2/bframe?k=" + key},
	}}
	w, ok := showingWidget(context.Background(), tab)
	if !ok || w.key != key || !w.invisible || w.pageAction != "submit" || w.enterprise {
		t.Errorf("showingWidget = %+v, %v; want the bframe's key with its invisible anchor's parameters", w, ok)
	}
}

func TestShownWidgetLeavesAnotherVendorsTypeAlone(t *testing.T) {
	recaptcha := shownWidget{typ: "recaptcha", key: "k"}
	for _, c := range []struct {
		html string
		want bool
	}{
		{"", true},
		{"recaptcha-v3", true},
		{"recaptcha", true},
		{"punish-frame", false},
		{"hcaptcha", false},
	} {
		if got := preferShown(c.html, recaptcha, true); got != c.want {
			t.Errorf("preferShown(%q) = %v, want %v", c.html, got, c.want)
		}
	}
}
