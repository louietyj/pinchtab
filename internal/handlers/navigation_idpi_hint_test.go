package handlers

import (
	"strings"
	"testing"
)

// The allowlist guidance used to be appended to the error MESSAGE. It now lives
// in details.remedy, which the CLI renders on its own line — but it has to keep
// the properties that made it usable: name the host, append to the current value
// instead of replacing it, and carry no placeholder a user would paste literally.
func TestIDPIAllowlistRemedy(t *testing.T) {
	line := idpiAllowlistRemedy("https://example.com/some/path").String()
	if !strings.Contains(line, "example.com") {
		t.Errorf("remedy should name the blocked host; got %q", line)
	}
	if !strings.Contains(line, "security.allowedDomains") {
		t.Errorf("remedy should name the allowlist config key; got %q", line)
	}
	// The remedy is the mode-neutral config write only: `pinchtab server restart`
	// would destroy a bridge answering this gate, so the restart guidance moved to
	// the hint (checked below).
	if strings.Contains(line, "server restart") {
		t.Errorf("remedy must not name `server restart`, which destroys a bridge; got %q", line)
	}
	if strings.ContainsRune(line, '…') {
		t.Errorf("remedy must not contain the … placeholder; got %q", line)
	}
	if !strings.Contains(line, "config get security.allowedDomains") {
		t.Errorf("remedy should append to existing domains via config get; got %q", line)
	}
	if hint, _ := idpiRefusedURLDetails("https://example.com/some/path")["hint"].(string); !strings.Contains(strings.ToLower(hint), "restart pinchtab") {
		t.Errorf("refused-URL hint should remind the user to restart PinchTab; got %q", hint)
	}

	// A hostless target cannot be allowlisted, so it gets no remedy rather than
	// one that cannot work.
	if got := idpiAllowlistRemedy("about:blank"); !got.Empty() {
		t.Errorf("expected no remedy for a hostless url; got %q", got)
	}
	if _, ok := idpiRefusedURLDetails("about:blank")["remedy"]; ok {
		t.Error("a hostless refused URL must not carry a remedy")
	}
}

func TestIDPIScannerHint(t *testing.T) {
	hint := idpiScannerHint()
	if !strings.Contains(hint, "strictMode") {
		t.Errorf("scanner hint should point at strictMode; got %q", hint)
	}
	if strings.Contains(hint, "server restart") {
		t.Errorf("scanner hint must not name `server restart`, which destroys a bridge; got %q", hint)
	}
	if !strings.Contains(strings.ToLower(hint), "restart pinchtab") {
		t.Errorf("scanner hint should remind the user to restart PinchTab; got %q", hint)
	}
}
