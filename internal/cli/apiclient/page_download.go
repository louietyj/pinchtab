package apiclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

// pageDownloadPollChunk keeps each long poll under the client's 60 s timeout.
const pageDownloadPollChunk = 45 * time.Second

// StartURLDownload has the browser start downloading dlURL, waiting up to wait
// for the server to start sending it, and returns the answer holding the
// download to collect.
func StartURLDownload(client *http.Client, base, token, tab, dlURL string, wait time.Duration) (map[string]any, error) {
	path := "/download"
	if tab != "" {
		path = "/tabs/" + url.PathEscape(tab) + "/download"
	}
	q := url.Values{"url": {dlURL}, "waitMs": {strconv.FormatInt(wait.Milliseconds(), 10)}}
	c := *client
	c.Timeout = wait + 30*time.Second
	status, body, err := sendOnce(&c, token, newRequest(http.MethodGet, base, path, WithQuery(q)))
	if err != nil {
		return nil, err
	}
	if status >= http.StatusBadRequest {
		return nil, fmt.Errorf("%s", apiErrorText(status, body))
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// WaitForPageDownload polls /download/wait until a download has finished or
// deadline passes: the one named by guid, else the tab's next one. It returns
// the last answer: state "none" when no download began, "inProgress" when one
// is still going.
func WaitForPageDownload(client *http.Client, base, token, tab, guid string, deadline time.Time) (map[string]any, error) {
	var last map[string]any
	for {
		wait := time.Until(deadline)
		if wait > pageDownloadPollChunk {
			wait = pageDownloadPollChunk
		}
		if wait <= 0 {
			if last != nil {
				return last, nil
			}
			wait = 0
		}
		q := url.Values{"waitMs": {strconv.FormatInt(wait.Milliseconds(), 10)}}
		if guid != "" {
			q.Set("guid", guid)
		} else if tab != "" {
			q.Set("tabId", tab)
		}
		status, body, err := sendOnce(client, token, newRequest(http.MethodGet, base, "/download/wait", WithQuery(q)))
		if err != nil {
			return nil, err
		}
		if status >= http.StatusBadRequest {
			return nil, fmt.Errorf("%s", apiErrorText(status, body))
		}
		var m map[string]any
		if err := json.Unmarshal(body, &m); err != nil {
			return nil, err
		}
		last = m
		if s, _ := m["state"].(string); s != "none" && s != "inProgress" {
			return m, nil
		}
	}
}

// ListDownloads returns every download the server has followed.
func ListDownloads(client *http.Client, base, token string) ([]map[string]any, error) {
	status, body, err := sendOnce(client, token, newRequest(http.MethodGet, base, "/downloads"))
	if err != nil {
		return nil, err
	}
	if status >= http.StatusBadRequest {
		return nil, fmt.Errorf("%s", apiErrorText(status, body))
	}
	var m struct {
		Downloads []map[string]any `json:"downloads"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	return m.Downloads, nil
}

// SavePageDownload streams a finished download's bytes to dst. The body can be
// far larger and slower than any API answer, so it runs with no client timeout.
// A tab is needed only to read off a remote browser; with none the server uses
// the current tab or a blank one.
func SavePageDownload(client *http.Client, base, token, tab, guid, dst string) (int64, error) {
	q := url.Values{"guid": {guid}}
	if tab != "" {
		q.Set("tabId", tab)
	}
	req, err := http.NewRequest(http.MethodGet, buildURL(base, "/download/content", q), nil)
	if err != nil {
		return 0, err
	}
	setClientHeaders(req, token)
	c := *client
	c.Timeout = 0
	resp, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("%s", apiErrorText(resp.StatusCode, body))
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	n, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr == nil && resp.ContentLength >= 0 && n != resp.ContentLength {
		copyErr = fmt.Errorf("got %d of %d bytes", n, resp.ContentLength)
	}
	if copyErr != nil {
		_ = os.Remove(dst)
		return n, copyErr
	}
	return n, closeErr
}

func apiErrorText(status int, body []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return e.Error
	}
	return fmt.Sprintf("HTTP %d: %s", status, string(body))
}
