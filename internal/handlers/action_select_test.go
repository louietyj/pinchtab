package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/bridge"
	bridgecdpops "github.com/pinchtab/pinchtab/internal/bridge/cdpops"
)

// A select whose value matches no option is a client input error, not a server
// fault: the element was found and resolved. It must classify as 4xx, carry the
// non-retryable option_not_found code, and hand back the real options so the
// caller can pick one without re-inspecting.
func TestSelectWithNoMatchingOptionReturns4xxWithAvailable(t *testing.T) {
	available := []bridgecdpops.SelectOption{
		{Value: "red", Text: "Red"},
		{Value: "green", Text: "Green"},
	}
	mb := &mockBridge{
		availableActions: []string{bridge.ActionSelect},
		executeActionErr: &bridgecdpops.NoOptionMatchError{Value: "Purple", Available: available},
	}
	w, failure := postActionBody(t, mb, `{"kind":"select","nodeId":42,"value":"Purple","tabId":"tab1"}`)

	if w.Code < 400 || w.Code >= 500 {
		t.Fatalf("status = %d, want 4xx; a resolved element with an unmatched value is a client error, not 500\nbody: %s", w.Code, w.Body.String())
	}
	if failure.Code != "option_not_found" {
		t.Errorf("code = %q, want option_not_found", failure.Code)
	}
	if failure.Retryable {
		t.Errorf("retryable = true, want false; retrying the identical value can never match an option")
	}

	var resp struct {
		Details struct {
			Available []bridgecdpops.SelectOption `json:"available"`
			Hint      string                      `json:"hint"`
		} `json:"details"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v\nbody: %s", err, w.Body.String())
	}
	if len(resp.Details.Available) != len(available) {
		t.Fatalf("details.available = %v, want the %d real options", resp.Details.Available, len(available))
	}
	for i, want := range available {
		if resp.Details.Available[i] != want {
			t.Errorf("details.available[%d] = %+v, want %+v", i, resp.Details.Available[i], want)
		}
	}
	for _, want := range available {
		if !strings.Contains(resp.Details.Hint, want.Value) {
			t.Errorf("details.hint = %q, want it to name option %q so the CLI renders it", resp.Details.Hint, want.Value)
		}
	}
}

// A genuinely missing element still returns the element-not-found status: the
// new option-not-found branch sits after the ErrTargetNotFound branch and keys
// on a distinct type, so it must not intercept the not-found path.
func TestSelectOnMissingElementStillNotFound(t *testing.T) {
	mb := &mockBridge{
		availableActions: []string{bridge.ActionSelect},
		executeActionErr: ErrTargetNotFound,
	}
	w, _ := postActionBody(t, mb, `{"kind":"select","nodeId":42,"value":"Red","tabId":"tab1"}`)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a missing element\nbody: %s", w.Code, w.Body.String())
	}
}
