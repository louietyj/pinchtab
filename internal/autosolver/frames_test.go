package autosolver

import "testing"

func TestChildFrameWidgetFindsTheFrameHostingAWidget(t *testing.T) {
	frames := []FrameRef{
		{ID: "top", URL: "https://shop.example/checkout"},
		{ID: "form", ParentID: "top", URL: "https://pay.vendor.example/form"},
		{ID: "anchor", ParentID: "form", URL: "https://www.google.com/recaptcha/api2/anchor?ar=1&k=6Lc_abcdefghijklmnopqrst&co=x"},
	}
	host, vendor, ok := ChildFrameWidget(frames)
	if !ok || host.ID != "form" || vendor != "recaptcha" {
		t.Errorf("ChildFrameWidget = %+v, %q, %v", host, vendor, ok)
	}
	// On the top document it is the HTML detectors' job, not this one's.
	if _, _, ok := ChildFrameWidget(frames[:1:1]); ok {
		t.Error("found a widget with no vendor frame")
	}
	topWidget := []FrameRef{frames[0], {ID: "anchor", ParentID: "top", URL: frames[2].URL}}
	if _, _, ok := ChildFrameWidget(topWidget); ok {
		t.Error("claimed a widget on the top document")
	}
}
