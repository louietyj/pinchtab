package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/pinchtab/pinchtab/internal/activity"
	"github.com/pinchtab/pinchtab/internal/autosolver/adapters"
	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/httpx"
	"github.com/pinchtab/pinchtab/internal/routes"
)

func (h *Handlers) evaluateEnabled() bool {
	return h != nil && h.Config != nil && h.Config.AllowEvaluate
}

// HandleEvaluate runs JavaScript in the current tab.
//
// @Endpoint POST /evaluate
func (h *Handlers) HandleEvaluate(w http.ResponseWriter, r *http.Request) {
	if !h.evaluateEnabled() {
		h.writeCapabilityDisabled(w, routes.CapEvaluate)
		return
	}

	var req struct {
		TabID        string `json:"tabId"`
		Expression   string `json:"expression"`
		AwaitPromise bool   `json:"awaitPromise"`
		// Frame runs the expression in a child frame instead of the top
		// document: a frame ID, or part of the frame's URL. Cross-site frames
		// included.
		Frame string `json:"frame"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodySize)).Decode(&req); err != nil {
		httpx.Error(w, 400, fmt.Errorf("decode: %w", err))
		return
	}
	if req.Expression == "" {
		httpx.Error(w, 400, fmt.Errorf("expression required"))
		return
	}

	ctx, _, ok := h.guardedTabContext(w, r, req.TabID, guardDomainPolicy|guardHandoffPause)
	if !ok {
		return
	}

	tCtx, tCancel := context.WithTimeout(ctx, h.Config.ActionTimeout)
	defer tCancel()
	go httpx.CancelOnClientDone(r.Context(), tCancel)

	slog.Warn("evaluate",
		"tabId", req.TabID,
		"expressionLength", len(req.Expression),
		"remoteAddr", r.RemoteAddr,
	)

	var result any
	if req.Frame != "" {
		frames, err := adapters.TabFrames(tCtx)
		if err != nil {
			httpx.Error(w, 500, fmt.Errorf("list frames: %w", err))
			return
		}
		frame, found := adapters.FindFrame(frames, req.Frame)
		if !found {
			available := make([]map[string]string, 0, len(frames))
			urls := make([]string, 0, len(frames))
			for _, f := range frames[min(1, len(frames)):] {
				available = append(available, map[string]string{"frameId": f.ID, "url": f.URL})
				urls = append(urls, f.URL)
			}
			// The CLI prints only the message, so the frames go in it too.
			httpx.ErrorCode(w, 404, "frame_not_found", fmt.Sprintf("no frame matches %q; the tab's frames are: %s", req.Frame, strings.Join(urls, " , ")), false, map[string]any{"frames": available})
			return
		}
		if err := adapters.EvaluateInFrame(tCtx, frame.ID, req.Expression, &result, req.AwaitPromise); err != nil {
			httpx.Error(w, 500, fmt.Errorf("evaluate in frame %s: %w", frame.URL, err))
			return
		}
		h.recordActivity(r, activity.Update{Action: "evaluate"})
		httpx.JSON(w, 200, map[string]any{"result": result, "frame": map[string]string{"frameId": frame.ID, "url": frame.URL}})
		return
	}
	opts := bridge.EvalOpts{AwaitPromise: req.AwaitPromise}
	if err := h.evalRuntime(tCtx, req.Expression, &result, opts); err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "Cannot read properties of null") ||
			strings.Contains(errMsg, "Cannot read property") ||
			strings.Contains(errMsg, "null is not an object") {
			httpx.ErrorCode(w, 500, "evaluate_null_ref", fmt.Sprintf("evaluate: %s", errMsg), false, map[string]any{
				"hint": "querySelector returned null — the element doesn't exist. Use snapshot refs (e0, e1, ...) instead of raw selectors.",
			})
			return
		}
		httpx.Error(w, 500, fmt.Errorf("evaluate: %w", err))
		return
	}

	h.recordActivity(r, activity.Update{Action: "evaluate"})

	httpx.JSON(w, 200, map[string]any{"result": result})
}

// HandleTabEvaluate runs JavaScript in a tab identified by path ID.
//
// @Endpoint POST /tabs/{id}/evaluate
func (h *Handlers) HandleTabEvaluate(w http.ResponseWriter, r *http.Request) {
	if !h.evaluateEnabled() {
		h.writeCapabilityDisabled(w, routes.CapEvaluate)
		return
	}

	h.withPathTabIDBody(w, r, h.HandleEvaluate)
}
