package actions

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/pinchtab/pinchtab/internal/cli/apiclient"
	"github.com/pinchtab/pinchtab/internal/cli/output"
	"github.com/spf13/cobra"
)

func Wait(client *http.Client, base, token string, args []string, cmd *cobra.Command) {
	waitFor(client, base, token, args, cmd, "wait", 0)
}

// Expect is wait as a check: a short default timeout, and a failure that names
// what was not met, so a batch stops on a wrong state instead of acting past it.
func Expect(client *http.Client, base, token string, args []string, cmd *cobra.Command) {
	waitFor(client, base, token, args, cmd, "expect", 3000)
}

func waitFor(client *http.Client, base, token string, args []string, cmd *cobra.Command, verb string, defaultTimeoutMs int) {
	body := map[string]any{}

	textFlag, _ := cmd.Flags().GetString("text")
	notTextFlag, _ := cmd.Flags().GetString("not-text")
	urlFlag, _ := cmd.Flags().GetString("url")
	loadFlag, _ := cmd.Flags().GetString("load")
	fnFlag, _ := cmd.Flags().GetString("fn")
	stateFlag, _ := cmd.Flags().GetString("state")
	tabID, _ := cmd.Flags().GetString("tab")

	if domQuiet, _ := cmd.Flags().GetBool("dom-quiet"); domQuiet {
		if !settleDOM(client, base, token, tabID) {
			output.Hint("the page was still changing after 2s")
		}
		output.Success()
		return
	}

	// The server's /wait timeout is milliseconds. --timeout-ms is the canonical
	// flag; --timeout is the deprecated millisecond alias (cobra prints its
	// deprecation note on use). --timeout-ms wins when both are given.
	timeoutMs, _ := cmd.Flags().GetInt("timeout-ms")
	if timeoutMs == 0 {
		timeoutMs, _ = cmd.Flags().GetInt("timeout")
	}

	switch {
	case textFlag != "":
		body["text"] = textFlag
	case notTextFlag != "":
		body["notText"] = notTextFlag
	case urlFlag != "":
		body["url"] = urlFlag
	case loadFlag != "":
		body["load"] = loadFlag
	case fnFlag != "":
		body["fn"] = fnFlag
	case len(args) > 0:
		// Bare arg is overloaded: a number means a ms wait, anything else a selector.
		if ms, err := strconv.Atoi(args[0]); err == nil {
			body["ms"] = ms
		} else {
			body["selector"] = args[0]
			if stateFlag != "" {
				body["state"] = stateFlag
			}
		}
	default:
		if verb == "expect" {
			output.Error("expect", "give a selector, --text, --not-text, --url or --fn", 2)
			return
		}
		fmt.Println("Usage: pinchtab wait <selector|ms> [--text|--not-text|--url|--load|--fn] [--timeout-ms ms] [--tab id]")
		return
	}

	if timeoutMs == 0 {
		timeoutMs = defaultTimeoutMs
	}
	if timeoutMs > 0 {
		body["timeout"] = timeoutMs
	}

	path := "/wait"
	if tabID != "" {
		path = "/tabs/" + tabID + "/wait"
	}

	result := apiclient.DoPostQuiet(client, base, token, path, body)

	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		printIndented(result)
	}

	if waited, ok := result["waited"].(bool); ok && !waited {
		if verb == "expect" {
			output.Error("expect", fmt.Sprintf("%s not met within %dms", expectation(body), timeoutMs), output.ExitTimeout)
			return
		}
		output.Error("wait", "timeout", output.ExitTimeout)
		return
	}
	if !jsonOutput {
		output.Success()
	}
}

func expectation(body map[string]any) string {
	for _, k := range []string{"text", "notText", "url", "fn", "selector"} {
		if v, ok := body[k]; ok {
			return fmt.Sprintf("%s %q", k, v)
		}
	}
	return "expectation"
}
