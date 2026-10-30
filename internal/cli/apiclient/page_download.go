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

// WaitForPageDownload polls /download/wait until the tab's next download has
// finished or deadline passes. It returns the last answer: state "none" when
// no download began, "inProgress" when one is still going.
func WaitForPageDownload(client *http.Client, base, token, tab string, deadline time.Time) (map[string]any, error) {
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
		if tab != "" {
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

// SavePageDownload streams a finished download's bytes to dst. The body can be
// far larger and slower than any API answer, so it runs with no client timeout.
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
