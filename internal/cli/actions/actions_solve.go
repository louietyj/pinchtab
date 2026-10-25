package actions

import (
	"net/http"

	"github.com/pinchtab/pinchtab/internal/cli/apiclient"
	"github.com/spf13/cobra"
)

// Solve runs the solvers on the current page now, or only the named one.
func Solve(client *http.Client, base, token string, cmd *cobra.Command, args []string) {
	body := map[string]any{}
	if len(args) == 1 {
		body["solver"] = args[0]
	}
	if tab, _ := cmd.Flags().GetString("tab"); tab != "" {
		body["tabId"] = tab
	}
	apiclient.DoPost(client, base, token, "/solve", body)
}
