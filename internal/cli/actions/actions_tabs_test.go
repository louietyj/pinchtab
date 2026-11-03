package actions

import (
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestTabCloseUsesCloseEndpoint(t *testing.T) {
	m := newMockServer()
	defer m.close()

	cmd := &cobra.Command{}
	cmd.Flags().Bool("json", false, "")

	TabClose(http.DefaultClient, m.base(), "", "tab_123", cmd)

	if m.lastPath != "/close" {
		t.Fatalf("expected /close, got %s", m.lastPath)
	}
	if !strings.Contains(m.lastBody, `"tabId":"tab_123"`) {
		t.Fatalf("expected tabId in body, got %s", m.lastBody)
	}
	if strings.Contains(m.lastBody, "action") {
		t.Fatalf("close endpoint body should not include legacy action field, got %s", m.lastBody)
	}
}

// In a shared browser the list says whose each tab is, and marks the current one.
func TestTabListMarksCurrentAndOwner(t *testing.T) {
	m := newMockServer()
	defer m.close()
	// Titles are decoration: the server's opened flag and the caller's own
	// record decide, so an untagged tab PinchTab opened is still an agent's.
	m.setResponse("GET", "/tabs", 200, `{"tabs":[
		{"id":"MINE","url":"https://a.test/","title":"A","opened":true},
		{"id":"OTHER","url":"https://b.test/","title":"B","opened":true},
		{"id":"USERS","url":"https://mail.test/","title":"[PinchTab :9867] Inbox"}]}`)
	oldCurrent, oldOwns := CurrentTab, OwnsTab
	defer func() { CurrentTab, OwnsTab = oldCurrent, oldOwns }()
	CurrentTab = func() string { return "USERS" }
	OwnsTab = func(id string) (bool, bool) { return id == "MINE", true }

	cmd := &cobra.Command{}
	cmd.Flags().Bool("json", false, "")
	out := captureStdout(t, func() { TabList(m.server.Client(), m.base(), "", cmd) })

	for _, want := range []string{
		"MINE\tyours\thttps://a.test/",
		"OTHER\tanother agent's\thttps://b.test/",
		"*USERS\tnot pinchtab's\thttps://mail.test/",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}

	// Where the server keeps tabs per session, the list names no owners.
	OwnsTab = func(string) (bool, bool) { return false, false }
	out = captureStdout(t, func() { TabList(m.server.Client(), m.base(), "", cmd) })
	if strings.Contains(out, "yours") || strings.Contains(out, "pinchtab's") {
		t.Fatalf("owners named without local tracking:\n%s", out)
	}
}

// A click that opened a tab says so, and that tab becomes the current one.
func TestActionThatOpenedATabReportsIt(t *testing.T) {
	var made string
	old := OnTabOpened
	defer func() { OnTabOpened = old }()
	OnTabOpened = func(id string) { made = id }

	out := captureStdout(t, func() {
		printActionResult("click", map[string]any{"success": true, "result": map[string]any{
			"switchedToTab": "POPUP", "switchedToTabUrl": "https://b.test/"}})
	})
	if !strings.Contains(out, "opened tab POPUP https://b.test/ (now current)") {
		t.Fatalf("output = %q", out)
	}
	if made != "POPUP" {
		t.Fatalf("current tab = %q, want POPUP", made)
	}
}

func TestTabFocusUsesResolvedTabID(t *testing.T) {
	m := newMockServer()
	m.response = `{"focused":true,"tabId":"tab_123"}`
	defer m.close()

	cmd := &cobra.Command{}
	cmd.Flags().Bool("json", false, "")

	got := TabFocus(http.DefaultClient, m.base(), "", "tab_123", cmd)

	if got != "tab_123" {
		t.Fatalf("focused tab = %q, want tab_123", got)
	}
	if m.lastPath != "/tab" {
		t.Fatalf("expected /tab, got %s", m.lastPath)
	}
	if !strings.Contains(m.lastBody, `"tabId":"tab_123"`) {
		t.Fatalf("expected tabId in body, got %s", m.lastBody)
	}
}

func TestTabFocusResolvesOneBasedIndex(t *testing.T) {
	m := newMockServer()
	m.response = `{"tabs":[{"id":"tab_1"},{"id":"tab_2"}]}`
	defer m.close()

	cmd := &cobra.Command{}
	cmd.Flags().Bool("json", false, "")

	got := TabFocus(http.DefaultClient, m.base(), "", "2", cmd)

	if got != "tab_2" {
		t.Fatalf("focused tab = %q, want tab_2", got)
	}
	if len(m.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(m.requests))
	}
	if m.requests[0].Path != "/tabs" {
		t.Fatalf("first request path = %q, want /tabs", m.requests[0].Path)
	}
	if m.requests[1].Path != "/tab" {
		t.Fatalf("second request path = %q, want /tab", m.requests[1].Path)
	}
	if !strings.Contains(m.requests[1].Body, `"tabId":"tab_2"`) {
		t.Fatalf("expected resolved tabId in body, got %s", m.requests[1].Body)
	}
}
