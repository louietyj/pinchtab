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

// A download is waited for like a foreground command: past the timeout it
// carries on in the browser, and the command hands back a handle to collect
// it with. A longer wait belongs in a monitor, not a blocked command.
const (
	downloadWaitDefault = 30 * time.Second
	downloadWaitMax     = 2 * time.Minute
)

func downloadTimeout(timeoutMs int) time.Duration {
	if timeoutMs <= 0 {
		return downloadWaitDefault
	}
	return min(time.Duration(timeoutMs)*time.Millisecond, downloadWaitMax)
}

// waitDownload is `wait --download [handle]`: the download named by guid, else
// the next one the page started, saved locally wherever the browser is.
func waitDownload(client *http.Client, base, token, tabID, guid string, timeoutMs int, dst string) {
	timeout := downloadTimeout(timeoutMs)
	collectDownload(client, base, token, tabID, guid, time.Now().Add(timeout), timeout, dst, "wait")
}

// collectDownload waits until deadline for a download to finish and saves it
// to dst (default ~/pinchtab-downloads/<its name>). One still going by then is
// left running, and its handle printed.
func collectDownload(client *http.Client, base, token, tabID, guid string, deadline time.Time, timeout time.Duration, dst, verb string) {
	resp, err := apiclient.WaitForPageDownload(client, base, token, tabID, guid, deadline)
	if err != nil {
		output.Error(verb, err.Error(), 1)
		return
	}
	dl, _ := resp["download"].(map[string]any)
	name, _ := dl["filename"].(string)
	if g, _ := dl["guid"].(string); g != "" {
		guid = g
	}
	switch resp["state"] {
	case "none":
		output.Error(verb, fmt.Sprintf("no download started on this tab within %s", timeout.Round(time.Second)), output.ExitTimeout)
		return
	case "inProgress":
		fmt.Printf("download: %s still downloading after %s (%s); it carries on in the browser. Collect it: %s\n",
			name, timeout.Round(time.Second), progress(dl), collectCommand(guid, dst))
		os.Exit(output.ExitStillRunning)
	case "completed":
	default:
		reason, _ := dl["error"].(string)
		output.Error(verb, fmt.Sprintf("download %s %v: %s", name, resp["state"], reason), 1)
		return
	}

	if dst == "" {
		dst, err = defaultDownloadPath(name)
		if err != nil {
			output.Error(verb, err.Error(), 1)
			return
		}
	}
	n, err := apiclient.SavePageDownload(client, base, token, tabID, guid, dst)
	if err != nil {
		output.Error(verb, fmt.Sprintf("copy %s back: %v", name, err), 1)
		return
	}
	printSaved(dst, int(n))
	// A personal browser also keeps a page's download under its own name; a
	// file named for its guid is pinchtab's working copy.
	fromURL, _ := dl["fromUrl"].(bool)
	if saved, _ := dl["filePath"].(string); saved != "" && !fromURL && !strings.HasPrefix(filepath.Base(saved), guid) {
		output.Value("The browser also saved it as " + saved)
	}
}

// collectCommand is the command that picks up download guid later.
func collectCommand(guid, dst string) string {
	cmd := toolName() + " wait --download " + guid
	if dst != "" {
		cmd += " -o " + shellQuote(dst)
	}
	return cmd
}

// toolName is the command the agent types: pinchtab, pinchtab-cloud, ...
func toolName() string {
	if name := strings.TrimSpace(os.Getenv("PINCHTAB_NAME")); name != "" {
		return name
	}
	return "pinchtab"
}

func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r == '/' || r == '.' || r == '-' || r == '_' || r == '~' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// progress reads "12.0 MB of 30.0 MB", or "12.0 MB so far" with no total.
func progress(dl map[string]any) string {
	got := byteSize(dl["receivedBytes"])
	if total, _ := dl["totalBytes"].(float64); total > 0 {
		return got + " of " + byteSize(total)
	}
	return got + " so far"
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
