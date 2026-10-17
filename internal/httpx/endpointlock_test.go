package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDisabledEndpointHandlerIncludesHintAndRemedy(t *testing.T) {
	handler := DisabledEndpointHandler("recording", "security.allowScreencast", "recording_disabled")

	w := httptest.NewRecorder()
	r, _ := http.NewRequest("POST", "/record/start", nil)
	handler(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}

	var resp struct {
		Error   string         `json:"error"`
		Code    string         `json:"code"`
		Details map[string]any `json:"details"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if resp.Code != "recording_disabled" {
		t.Fatalf("code = %q, want recording_disabled", resp.Code)
	}

	hint, _ := resp.Details["hint"].(string)
	remedy, _ := resp.Details["remedy"].(string)

	if hint == "" {
		t.Fatal("expected non-empty hint in details")
	}
	if remedy == "" {
		t.Fatal("expected non-empty remedy in details")
	}
	if remedy != "pinchtab config set security.allowScreencast true" {
		t.Fatalf("remedy = %q, want the mode-neutral config write with no `server restart`", remedy)
	}
	if strings.Contains(remedy, "pinchtab server restart") {
		t.Fatalf("remedy names the server-only restart, which destroys a bridge: %q", remedy)
	}
	if !strings.Contains(strings.ToLower(hint), "restart pinchtab") {
		t.Fatalf("hint does not carry the mode-neutral restart guidance: %q", hint)
	}
}

// The label is the capability, not the endpoint: /storage is gated by
// stateExport, and /record/* by the screencast setting. Calling either label an
// "endpoint" sends the reader looking for a route that does not exist.
func TestDisabledEndpointMessageNamesTheCapabilityNotAnEndpoint(t *testing.T) {
	tests := []struct {
		name       string
		capability string
		setting    string
	}{
		{"capability named after another endpoint", "stateExport", "security.allowStateExport"},
		{"capability matching its own endpoint", "cookies", "security.allowCookies"},
		{"feature gated by a differently named setting", "recording", "security.allowScreencast"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := DisabledEndpointMessage(tt.capability, tt.setting)

			if strings.Contains(msg, tt.capability+" endpoint") {
				t.Fatalf("message calls the capability an endpoint: %q", msg)
			}
			if !strings.Contains(msg, tt.capability+" capability") {
				t.Fatalf("message does not name the required capability: %q", msg)
			}
			if !strings.Contains(msg, tt.setting) {
				t.Fatalf("message does not name the setting to change: %q", msg)
			}
		})
	}
}

func TestDisabledEndpointHandlerKeepsSettingHintAndRemedy(t *testing.T) {
	handler := DisabledEndpointHandler("stateExport", "security.allowStateExport", "state_export_disabled")

	w := httptest.NewRecorder()
	r, _ := http.NewRequest("GET", "/storage", nil)
	handler(w, r)

	var resp struct {
		Error   string         `json:"error"`
		Code    string         `json:"code"`
		Details map[string]any `json:"details"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.Code != "state_export_disabled" {
		t.Fatalf("code = %q, want state_export_disabled", resp.Code)
	}
	if strings.Contains(resp.Error, "stateExport endpoint") {
		t.Fatalf("error still describes a stateExport endpoint: %q", resp.Error)
	}
	for key, want := range map[string]string{
		"setting": "security.allowStateExport",
		"hint":    "Enable security.allowStateExport to use this feature, then restart PinchTab to apply the change.",
		"remedy":  "pinchtab config set security.allowStateExport true",
	} {
		if got, _ := resp.Details[key].(string); got != want {
			t.Fatalf("details[%q] = %q, want %q", key, got, want)
		}
	}
}

// This gate answers on a bridge as well as a server, and `pinchtab server restart`
// stops a bridge and silently swaps it for a server. So the executable remedy must
// carry ONLY the mode-neutral config write — no server-restart verb a bridge user
// could run verbatim — and the restart, which is what actually applies the change
// (the security block is read at boot), is stated in the hint as mode-neutral prose
// the caller applies for their own mode. This supersedes the old contract that put
// `&& pinchtab server restart` in the executable remedy.
func TestDisabledEndpointRemedyIsModeNeutralConfigWriteWithRestartInTheHint(t *testing.T) {
	for _, setting := range []string{"security.allowCookies", "security.allowStateExport", "security.allowClipboard"} {
		details := DisabledEndpointDetails(setting)
		remedy, _ := details["remedy"].(string)
		hint, _ := details["hint"].(string)

		configCmd := "pinchtab config set " + setting + " true"
		if remedy != configCmd {
			t.Errorf("remedy for %s = %q, want exactly the mode-neutral config write %q", setting, remedy, configCmd)
		}
		if strings.Contains(remedy, "pinchtab server restart") {
			t.Errorf("remedy for %s names the server-only restart, which destroys a bridge if run: %q", setting, remedy)
		}
		if strings.Contains(remedy, "\n") {
			t.Errorf("remedy for %s spans lines, so it cannot be run verbatim: %q", setting, remedy)
		}
		if !strings.Contains(strings.ToLower(hint), "restart pinchtab") {
			t.Errorf("hint for %s does not tell the caller to restart PinchTab to apply the change: %q", setting, hint)
		}
		if strings.Contains(hint, "pinchtab server restart") {
			t.Errorf("hint for %s names the server-only restart command, wrong on a bridge: %q", setting, hint)
		}
	}
}
