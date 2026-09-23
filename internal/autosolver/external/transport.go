package external

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

// taskAPI speaks the createTask/getTaskResult protocol that CapSolver and
// 2Captcha share. It returns the raw solution: its shape depends on the task.
type taskAPI struct {
	label        string // prefixes every error, e.g. "capsolver"
	baseURL      string
	apiKey       string
	pollInterval time.Duration
	client       *http.Client
}

type taskResponse struct {
	ErrorID          int    `json:"errorId"`
	ErrorCode        string `json:"errorCode"`
	ErrorDescription string `json:"errorDescription"`
	// CapSolver quotes task IDs and 2Captcha does not; either is echoed back verbatim.
	TaskID   json.RawMessage `json:"taskId"`
	Status   string          `json:"status"`
	Solution json.RawMessage `json:"solution"`
}

// solve runs the create → poll cycle for a provider-specific task.
func (a *taskAPI) solve(ctx context.Context, task any) (json.RawMessage, error) {
	var created taskResponse
	if err := a.postJSON(ctx, "/createTask", map[string]any{"clientKey": a.apiKey, "task": task}, &created); err != nil {
		return nil, fmt.Errorf("%s createTask: %w", a.label, err)
	}
	if created.ErrorID != 0 {
		err := fmt.Errorf("%s createTask error %s: %s", a.label, created.ErrorCode, created.ErrorDescription)
		if transientTaskErrors[created.ErrorCode] {
			return nil, err
		}
		// A refused task is refused again on retry: same key, same balance, same data.
		return nil, autosolver.Permanent(err)
	}
	// Recognition tasks (VisionEngine, ImageToText) answer in the createTask reply.
	if created.Status == "ready" && hasSolution(created.Solution) {
		return created.Solution, nil
	}
	if isBlankJSON(created.TaskID) {
		return nil, fmt.Errorf("%s createTask returned no taskId", a.label)
	}

	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// The provider keeps working an accepted task and bills it when done, so
			// once polling breaks off (here or in a failed poll) a retry pays twice.
			return nil, autosolver.Spent(fmt.Errorf("%s: context cancelled while polling: %w", a.label, ctx.Err()))
		case <-ticker.C:
			var res taskResponse
			err := a.postJSON(ctx, "/getTaskResult", map[string]any{
				"clientKey": a.apiKey,
				"taskId":    created.TaskID,
			}, &res)
			if err != nil {
				return nil, autosolver.Spent(fmt.Errorf("%s getTaskResult: %w", a.label, err))
			}
			if res.ErrorID != 0 {
				return nil, fmt.Errorf("%s getTaskResult error %s: %s", a.label, res.ErrorCode, res.ErrorDescription)
			}
			switch res.Status {
			case "ready":
				return res.Solution, nil
			case "failed", "error":
				// Terminal failure that didn't set errorId — stop polling
				// instead of burning the whole solver deadline.
				return nil, fmt.Errorf("%s task failed (status %q): %s %s", a.label, res.Status, res.ErrorCode, res.ErrorDescription)
			}
			// status "processing" / "idle" → keep polling
		}
	}
}

// transientTaskErrors are the createTask refusals worth retrying.
var transientTaskErrors = map[string]bool{
	"ERROR_SERVICE_UNAVALIABLE": true, // sic: CapSolver's spelling
	"ERROR_SERVICE_UNAVAILABLE": true,
	"ERROR_RATE_LIMIT":          true,
	"ERROR_NO_SLOT_AVAILABLE":   true,
}

func isBlankJSON(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s == "" || s == "null" || s == `""`
}

func hasSolution(raw json.RawMessage) bool {
	return !isBlankJSON(raw) && strings.TrimSpace(string(raw)) != "{}"
}

func (a *taskAPI) postJSON(ctx context.Context, path string, body, out interface{}) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		// CapSolver reports a refused task as a 400 carrying the usual error body;
		// read it so the refusal is classified like any other.
		var probe struct {
			ErrorID int `json:"errorId"`
		}
		if json.Unmarshal(data, &probe) == nil && probe.ErrorID != 0 {
			return json.Unmarshal(data, out)
		}
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return json.Unmarshal(data, out)
}
