package external

import (
	"context"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

// framedTab is a checkout page whose payment form, a cross-site frame, holds a
// reCAPTCHA; the top document has no captcha at all.
type framedTab struct {
	fakePage
	fakeExecutor
	inFrame []string
}

func (f *framedTab) Frames(context.Context) ([]autosolver.FrameRef, error) {
	return []autosolver.FrameRef{
		{ID: "top", URL: "https://shop.example/checkout"},
		{ID: "form", ParentID: "top", URL: "https://pay.vendor.example/form"},
		{ID: "anchor", ParentID: "form", URL: "https://www.google.com/recaptcha/api2/anchor?ar=1&k=6LfD3PIbAAAAAJs_eEHvoOl75_83eXSqpPSRFJ_u&co=x"},
	}, nil
}

func (f *framedTab) EvaluateInFrame(_ context.Context, frameID, expr string, result any) error {
	f.inFrame = append(f.inFrame, frameID+": "+expr)
	if sp, ok := result.(*string); ok && strings.Contains(expr, "outerHTML") {
		*sp = `<form><div class="g-recaptcha" data-sitekey="6LfD3PIbAAAAAJs_eEHvoOl75_83eXSqpPSRFJ_u"></div></form>`
	}
	return nil
}

func TestProviderSolvesAWidgetInAChildFrameInsideThatFrame(t *testing.T) {
	var task map[string]any
	srv := mockTwoCaptcha(t, `{"gRecaptchaResponse":"03A-FRAMED"}`, &task)
	defer srv.Close()
	tab := &framedTab{fakePage: fakePage{url: "https://shop.example/checkout", html: `<iframe src="https://pay.vendor.example/form"></iframe>`}}
	solver := newTestTwoCaptcha(srv.URL)
	if ok, _ := solver.CanHandle(context.Background(), tab); !ok {
		t.Fatal("did not claim the framed widget")
	}
	res, err := solver.Solve(context.Background(), tab, tab)
	if err != nil || !res.Solved {
		t.Fatalf("Solve = %+v, %v", res, err)
	}
	if task["websiteURL"] != "https://pay.vendor.example/form" || task["websiteKey"] != "6LfD3PIbAAAAAJs_eEHvoOl75_83eXSqpPSRFJ_u" {
		t.Errorf("task = %v; want the form frame's URL and key", task)
	}
	delivered := false
	for _, call := range tab.inFrame {
		delivered = delivered || (strings.HasPrefix(call, "form: ") && strings.Contains(call, "03A-FRAMED"))
	}
	if !delivered {
		t.Errorf("token not delivered in the form frame: %v", tab.inFrame)
	}
	if res.FinalURL != "https://shop.example/checkout" {
		t.Errorf("FinalURL = %q, want the tab's own page", res.FinalURL)
	}
}
