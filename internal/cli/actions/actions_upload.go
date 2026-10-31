package actions

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/pinchtab/pinchtab/internal/cli"
	"github.com/pinchtab/pinchtab/internal/cli/apiclient"
)

func Upload(client *http.Client, base, token string, args []string, selector, tabID string, drop bool) {
	if len(args) < 1 {
		cli.Fatal("Usage: pinchtab upload <file-path> [--selector <css>] [--drop] [--tab <id>]")
	}
	if drop && selector == "" {
		cli.Fatal("--drop needs -s <drop zone>")
	}

	var files, fileNames []string
	for _, path := range args {
		data, err := os.ReadFile(path)
		if err != nil {
			cli.Fatal("Failed to read %s: %v", path, err)
		}
		files = append(files, base64.StdEncoding.EncodeToString(data))
		fileNames = append(fileNames, filepath.Base(path))
	}

	body := map[string]any{
		"files":     files,
		"fileNames": fileNames,
	}
	if selector != "" {
		body["selector"] = selector
	}
	if drop {
		body["drop"] = true
	}

	path := "/upload"
	if tabID != "" {
		path = "/tabs/" + url.PathEscape(tabID) + "/upload"
	}

	apiclient.DoPost(client, base, token, path, body)
}
