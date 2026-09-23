package actions

import "testing"

func TestVisionModuleFor(t *testing.T) {
	for _, tc := range []struct {
		puzzle        string
		hasBackground bool
		want          string
	}{
		{"slider", true, "slider_1"},
		{"rotate", true, "rotate_1"},
		{"rotate", false, "rotate_2"},
		{"select", false, "shein"},
		{"ocr", false, "ocr_gif"},
		{"slider_1", true, "slider_1"},
	} {
		if got := visionModuleFor(tc.puzzle, tc.hasBackground); got != tc.want {
			t.Errorf("visionModuleFor(%q, %v) = %q, want %q", tc.puzzle, tc.hasBackground, got, tc.want)
		}
	}
}
