package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/config"
)

// engine=bogus is refused before any browser work: ParseEngine runs first, so an
// unknown engine is a 400 naming the value, not a native scan of the page.
func TestHandleA11yAudit_UnknownEngineIs400(t *testing.T) {
	h := New(&mockBridge{}, &config.RuntimeConfig{}, nil, nil, nil)
	req := httptest.NewRequest("GET", "/a11y/audit?engine=bogus", nil)
	w := httptest.NewRecorder()
	h.HandleA11yAudit(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("engine=bogus status = %d, want 400 (body=%s)", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "bogus") {
		t.Errorf("400 body does not name the rejected engine: %s", body)
	}
}

// splitCSVParam feeds the axe run config; a blank or whitespace value must yield
// no filter (the default tag set), not an empty-string rule that matches nothing.
func TestSplitCSVParam(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"wcag2a", []string{"wcag2a"}},
		{"wcag2a, wcag2aa ,best-practice", []string{"wcag2a", "wcag2aa", "best-practice"}},
		{"a,,b", []string{"a", "b"}},
	} {
		got := splitCSVParam(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("splitCSVParam(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("splitCSVParam(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}
