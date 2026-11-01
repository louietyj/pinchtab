package actions

import (
	"net/http"
	"time"

	"github.com/pinchtab/pinchtab/internal/cli/apiclient"
	"github.com/pinchtab/pinchtab/internal/cli/output"
	"github.com/spf13/cobra"
)

// Solve runs the solvers on the current page now, or only the named one. It
// blocks within the step wait budget; a solve still running after that goes on
// in the background, and the HINT says so.
func Solve(client *http.Client, base, token string, cmd *cobra.Command, args []string) {
	body := map[string]any{}
	if len(args) == 1 {
		body["solver"] = args[0]
	}
	if tab, _ := cmd.Flags().GetString("tab"); tab != "" {
		body["tabId"] = tab
	}
	result := apiclient.DoPostQuiet(client, base, token, "/solve", body)
	if solve, ok := result["autoSolve"].(map[string]any); ok {
		if pending, _ := solve["pending"].(bool); pending {
			output.Hint(autoSolveHint(result))
			return
		}
		result = solve
	}
	output.JSON(result)
}

// AwaitCaptcha watches the current tab for --linger-ms for a widget to open a
// puzzle, waits out the solve, and prints a HINT about it; nothing when there
// was none.
func AwaitCaptcha(client *http.Client, base, token string, cmd *cobra.Command) {
	linger, _ := cmd.Flags().GetInt("linger-ms")
	outcome := apiclient.AwaitCaptcha(client, base, token, time.Duration(linger)*time.Millisecond)
	// Still solving here means the solve's step budget is spent, which the
	// step that met it has already said.
	if pending, _ := outcome["pending"].(bool); outcome == nil || pending {
		return
	}
	if hint := autoSolveHint(map[string]any{"autoSolve": outcome}); hint != "" {
		output.Hint("a captcha opened after the last step, and the batch waited for pinchtab: " + hint)
	}
}
