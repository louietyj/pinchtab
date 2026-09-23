package actions

import (
	"net/http"

	"github.com/pinchtab/pinchtab/internal/cli"
	"github.com/pinchtab/pinchtab/internal/cli/apiclient"
	"github.com/spf13/cobra"
)

// visionModuleFor maps the CLI's puzzle names onto Vision Engine modules; the
// module names themselves pass through.
func visionModuleFor(puzzle string, hasBackground bool) string {
	switch puzzle {
	case "slider":
		return "slider_1"
	case "rotate":
		if hasBackground {
			return "rotate_1"
		}
		return "rotate_2"
	case "select":
		return "shein"
	case "ocr":
		return "ocr_gif"
	}
	return puzzle
}

// Vision solves a visual puzzle on the page with CapSolver's Vision Engine and,
// given the controls, acts on the answer.
func Vision(client *http.Client, base, token string, cmd *cobra.Command, args []string) {
	if len(args) != 2 {
		cli.Fatal("Usage: pinchtab vision <slider|rotate|select|ocr> <image> [--background <sel>] [--handle <sel>] ...")
	}
	background, _ := cmd.Flags().GetString("background")
	body := map[string]any{
		"module": visionModuleFor(args[0], background != ""),
		"image":  args[1],
	}
	for _, flag := range []string{"background", "question", "handle", "track"} {
		if v, _ := cmd.Flags().GetString(flag); v != "" {
			body[flag] = v
		}
	}
	if ratio, _ := cmd.Flags().GetFloat64("ratio"); ratio != 0 {
		body["ratio"] = ratio
	}
	if click, _ := cmd.Flags().GetBool("click"); click {
		body["click"] = true
	}
	if tab, _ := cmd.Flags().GetString("tab"); tab != "" {
		body["tabId"] = tab
	}
	apiclient.DoPost(client, base, token, "/vision", body)
}
