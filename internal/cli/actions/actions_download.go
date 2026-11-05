package actions

import (
	"fmt"
	"net/http"
	"time"

	"github.com/pinchtab/pinchtab/internal/cli/apiclient"
	"github.com/pinchtab/pinchtab/internal/cli/output"
)

// Download has the browser download dlURL, then collects it like `wait
// --download`: saved to dst once finished, or, past the timeout or with
// background, left running with its handle printed.
func Download(client *http.Client, base, token, dlURL, dst, tabID string, timeoutMs int, background bool) {
	timeout := downloadTimeout(timeoutMs)
	deadline := time.Now().Add(timeout)
	resp, err := apiclient.StartURLDownload(client, base, token, tabID, dlURL, timeout)
	if err != nil {
		output.Error("download", err.Error(), output.ExitRuntime)
		return
	}
	dl, _ := resp["download"].(map[string]any)
	guid, _ := dl["guid"].(string)
	if background {
		name, _ := dl["filename"].(string)
		output.Value(fmt.Sprintf("download started: %s. Collect it: %s", name, collectCommand(guid, dst)))
		return
	}
	collectDownload(client, base, token, tabID, guid, deadline, timeout, dst, "download")
}

// Downloads prints every download the server has followed, newest last.
func Downloads(client *http.Client, base, token string) {
	all, err := apiclient.ListDownloads(client, base, token)
	if err != nil {
		output.Error("downloads", err.Error(), output.ExitRuntime)
		return
	}
	if len(all) == 0 {
		output.Value("no downloads")
		return
	}
	for _, dl := range all {
		output.Value(formatDownloadLine(dl))
	}
}

func formatDownloadLine(dl map[string]any) string {
	guid, _ := dl["guid"].(string)
	name, _ := dl["filename"].(string)
	state, _ := dl["state"].(string)
	line := fmt.Sprintf("%s  %-10s  %s  %s", guid, state, progress(dl), name)
	if reason, _ := dl["error"].(string); reason != "" {
		line += "  (" + reason + ")"
	}
	return line
}
