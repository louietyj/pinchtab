package handlers

import "testing"

func TestMatchVisionPuzzle(t *testing.T) {
	if p := matchVisionPuzzle(`<div class="geetest_holder geetest_wind"><div class="geetest_radar_tip">Click to verify</div></div>`); p == nil || p.name != "geetest-v3-slide" {
		t.Errorf("GeeTest v3 widget matched %+v", p)
	}
	// GeeTest v4 is solved by token; its widget carries none of v3's classes.
	if p := matchVisionPuzzle(`<div class="geetest_captcha geetest_boxShow"><div class="geetest_btn_click"></div></div>`); p != nil {
		t.Errorf("GeeTest v4 matched %s", p.name)
	}
}
