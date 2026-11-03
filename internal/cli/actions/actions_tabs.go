package actions

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/pinchtab/pinchtab/internal/cli"
	"github.com/pinchtab/pinchtab/internal/cli/apiclient"
	"github.com/pinchtab/pinchtab/internal/cli/output"
	"github.com/spf13/cobra"
)

// TabList lists all open tabs.
func TabList(client *http.Client, base, token string, cmd *cobra.Command) {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		apiclient.DoGet(client, base, token, "/tabs", nil)
		return
	}

	// Terse: one line per tab: [*]<id>\t[<owner>\t]<url>\t<title>
	body := apiclient.DoGetRaw(client, base, token, "/tabs", nil)
	var listed struct {
		Tabs []map[string]any `json:"tabs"`
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		fmt.Println(string(body))
		return
	}
	current := ""
	if CurrentTab != nil {
		current = CurrentTab()
	}
	for _, tab := range listed.Tabs {
		id, _ := tab["id"].(string)
		url, _ := tab["url"].(string)
		title, _ := tab["title"].(string)
		prefix := ""
		if id == current {
			prefix = "*"
		}
		// Whose a tab is comes from the server, which knows the tabs PinchTab
		// opened, and from this caller's own record; titles are decoration.
		owner := ""
		if OwnsTab != nil {
			if owned, tracked := OwnsTab(id); tracked {
				opened, _ := tab["opened"].(bool)
				switch {
				case owned:
					owner = "yours"
				case opened:
					owner = "another agent's"
				default:
					owner = "not pinchtab's"
				}
			}
		}
		if owner != "" {
			fmt.Printf("%s%s\t%s\t%s\t%s\n", prefix, id, owner, url, title)
		} else {
			fmt.Printf("%s%s\t%s\t%s\n", prefix, id, url, title)
		}
	}
}

// CurrentTab and OwnsTab read the command layer's local tab state: the tab
// later steps go to, and the tabs this agent opened. tracked is false when the
// server keeps tabs per session instead, and the list then names no owners.
var (
	CurrentTab func() string
	OwnsTab    func(tabID string) (owned, tracked bool)
)

// TabClose closes a tab by ID.
func TabClose(client *http.Client, base, token string, tabID string, cmd *cobra.Command) {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		apiclient.DoPost(client, base, token, "/close", map[string]any{"tabId": tabID})
		return
	}

	apiclient.DoPostQuiet(client, base, token, "/close", map[string]any{"tabId": tabID})
	output.Success()
}

// TabFocus switches to a tab by ID, making it the active tab
// for subsequent commands.
func TabFocus(client *http.Client, base, token string, tabID string, cmd *cobra.Command) string {
	resolvedTabID, err := resolveTabReference(client, base, token, tabID)
	if err != nil {
		cli.Fatal("%v", err)
	}

	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		result := apiclient.DoPost(client, base, token, "/tab", map[string]any{
			"action": "focus",
			"tabId":  resolvedTabID,
		})
		if tid, ok := result["tabId"].(string); ok && tid != "" {
			return tid
		}
		return resolvedTabID
	}

	result := apiclient.DoPostQuiet(client, base, token, "/tab", map[string]any{
		"action": "focus",
		"tabId":  resolvedTabID,
	})
	if tid, ok := result["tabId"].(string); ok && tid != "" {
		output.Value(tid)
		return tid
	}
	output.Value(resolvedTabID)
	return resolvedTabID
}

// TabHandoff pauses automation on a tab for manual operator intervention.
func TabHandoff(client *http.Client, base, token, tabID, reason string, timeoutMS int, cmd *cobra.Command) {
	body := map[string]any{}
	if reason != "" {
		body["reason"] = reason
	}
	if timeoutMS > 0 {
		body["timeoutMs"] = timeoutMS
	}
	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		apiclient.DoPost(client, base, token, fmt.Sprintf("/tabs/%s/handoff", tabID), body)
		return
	}
	apiclient.DoPostQuiet(client, base, token, fmt.Sprintf("/tabs/%s/handoff", tabID), body)
	output.Value("paused")
	if reason != "" {
		output.Hint(fmt.Sprintf("reason: %s", reason))
	}
}

// TabResume resumes automation on a tab after manual intervention.
func TabResume(client *http.Client, base, token, tabID, status string, cmd *cobra.Command) {
	body := map[string]any{}
	if status != "" {
		body["status"] = status
	}
	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		apiclient.DoPost(client, base, token, fmt.Sprintf("/tabs/%s/resume", tabID), body)
		return
	}
	apiclient.DoPostQuiet(client, base, token, fmt.Sprintf("/tabs/%s/resume", tabID), body)
	output.Value("resumed")
}

// TabHandoffStatus shows whether a tab is in paused_handoff or active state.
func TabHandoffStatus(client *http.Client, base, token, tabID string, cmd *cobra.Command) {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		apiclient.DoGet(client, base, token, fmt.Sprintf("/tabs/%s/handoff", tabID), nil)
		return
	}
	body := apiclient.DoGetRaw(client, base, token, fmt.Sprintf("/tabs/%s/handoff", tabID), nil)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		fmt.Println(string(body))
		return
	}
	status, _ := result["status"].(string)
	output.Value(status)
	if reason, ok := result["reason"].(string); ok && reason != "" {
		output.Hint(fmt.Sprintf("reason: %s", reason))
	}
}
