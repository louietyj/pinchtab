package actions

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pinchtab/pinchtab/internal/cli/apiclient"
	"github.com/pinchtab/pinchtab/internal/cli/output"
)

// downloadWaitDefault is how long `wait --download` waits when not told: the
// file is often built on demand (a zip of a filing's attachments) before it
// starts to arrive.
const downloadWaitDefault = 2 * time.Minute

// waitDownload waits for the download the page started to finish, then saves
// it locally, wherever the browser is.
func waitDownload(client *http.Client, base, token, tabID string, timeoutMs int, dst string) {
	timeout := downloadWaitDefault
	if timeoutMs > 0 {
		timeout = time.Duration(timeoutMs) * time.Millisecond
	}
	resp, err := apiclient.WaitForPageDownload(client, base, token, tabID, time.Now().Add(timeout))
	if err != nil {
		output.Error("wait", err.Error(), 1)
		return
	}
	dl, _ := resp["download"].(map[string]any)
	name, _ := dl["filename"].(string)
	switch resp["state"] {
	case "none":
		output.Error("wait", fmt.Sprintf("no download started on this tab within %s", timeout.Round(time.Second)), output.ExitTimeout)
		return
	case "inProgress":
		output.Error("wait", fmt.Sprintf("%s is still downloading after %s (%s so far); wait again", name, timeout.Round(time.Second), byteSize(dl["receivedBytes"])), output.ExitTimeout)
		return
	case "completed":
	default:
		reason, _ := dl["error"].(string)
		output.Error("wait", fmt.Sprintf("download %s %v: %s", name, resp["state"], reason), 1)
		return
	}

	if dst == "" {
		dst, err = defaultDownloadPath(name)
		if err != nil {
			output.Error("wait", err.Error(), 1)
			return
		}
	}
	guid, _ := dl["guid"].(string)
	n, err := apiclient.SavePageDownload(client, base, token, tabID, guid, dst)
	if err != nil {
		output.Error("wait", fmt.Sprintf("copy %s back: %v", name, err), 1)
		return
	}
	line := fmt.Sprintf("download: %s (%s) -> %s", name, byteSize(float64(n)), dst)
	// In a personal browser the file also stays where the browser saved it.
	if saved, _ := dl["filePath"].(string); saved != "" {
		line += " (saved by the browser as " + saved + ")"
	}
	output.Value(line)
}

// defaultDownloadPath names a file in ~/pinchtab-downloads after the page's
// own name for it, without overwriting an earlier one.
func defaultDownloadPath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "pinchtab-downloads")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "download"
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	path := filepath.Join(dir, name)
	for i := 2; ; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path, nil
		}
		path = filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, i, ext))
	}
}

func byteSize(v any) string {
	n, _ := v.(float64)
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", n/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", n/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", n/(1<<10))
	}
	return fmt.Sprintf("%.0f bytes", n)
}
