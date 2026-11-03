package actions

import (
	"encoding/json"
	"net/http"

	"github.com/pinchtab/pinchtab/internal/cli/apiclient"
)

// Settle timeouts, counted from the tab's anchor (the end of the last action,
// or the last wait condition that passed), not from the call. A step's own
// settle, before its screenshot or snapshot, gives up at 1s: on pages that
// never stop changing a longer wait bought nothing. An explicit
// `wait --dom-quiet` defaults to 2s, and only waits out what the settle before
// it has not.
const (
	stepSettleMs     = 1000
	explicitSettleMs = 2000
)

// settleDOM waits for the page to stop changing, so what is read or captured
// next is what the action produced, not a frame of it re-rendering. The server
// tracks quiet in the page and looks back to the anchor, so a settle right
// after another passes at once. It is best effort: a failed wait reads as
// settled. report marks an unsettled result as shown to the caller; the
// second result says an earlier wait on the same anchor already showed it.
func settleDOM(client *http.Client, base, token, tabID string, timeoutMs int, report bool) (settled, alreadyReported bool) {
	path := "/wait"
	if tabID != "" {
		path = "/tabs/" + tabID + "/wait"
	}
	raw, err := apiclient.DoRawE(client, base, token, http.MethodPost, path, apiclient.WithBody(map[string]any{
		"domQuiet": true,
		"timeout":  timeoutMs,
		"report":   report,
	}))
	if err != nil {
		return true, false
	}
	var resp struct {
		Unsettled       bool `json:"unsettled"`
		AlreadyReported bool `json:"alreadyReported"`
	}
	if json.Unmarshal(raw, &resp) != nil {
		return true, false
	}
	return !resp.Unsettled, resp.AlreadyReported
}
