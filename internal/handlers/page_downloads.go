package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/httpx"
	"github.com/pinchtab/pinchtab/internal/routes"
)

// pageDownloadWaitMax keeps one long poll under the CLI's 60 s client timeout;
// the CLI polls again until its own deadline.
const pageDownloadWaitMax = 50 * time.Second

// HandlePageDownloadWait waits for the tab's next uncollected download to
// finish (`wait --download`). It answers state "none" when no download began,
// "inProgress" when one is still going, and otherwise the finished download.
//
// @Endpoint GET /download/wait
func (h *Handlers) HandlePageDownloadWait(w http.ResponseWriter, r *http.Request) {
	if !h.Config.AllowDownload {
		h.writeCapabilityDisabled(w, routes.CapDownload)
		return
	}
	q := r.URL.Query()
	ctx, cancel := context.WithTimeout(r.Context(), queryMillis(q.Get("waitMs"), pageDownloadWaitMax, pageDownloadWaitMax))
	defer cancel()
	if guid := q.Get("guid"); guid != "" {
		pd, err := h.Bridge.AwaitPageDownload(ctx, guid)
		switch {
		case errors.Is(err, bridge.ErrUnknownPageDownload):
			httpx.ErrorCode(w, 404, "download_not_found", fmt.Sprintf("no download %q", guid), false, nil)
		case err != nil:
			httpx.Error(w, 500, err)
		default:
			httpx.JSON(w, 200, map[string]any{"state": pd.State, "download": pd})
		}
		return
	}
	_, tabID, err := h.tabContext(r, q.Get("tabId"))
	if err != nil {
		WriteTabContextError(w, err, 404)
		return
	}
	// A wait that outlasts the quiet tail must not let the save prompt back.
	stopQuiet := make(chan struct{})
	defer close(stopQuiet)
	go func() {
		for {
			h.Bridge.QuietDownloads()
			select {
			case <-stopQuiet:
				return
			case <-time.After(5 * time.Second):
			}
		}
	}()
	pd, err := h.Bridge.WaitPageDownload(ctx, tabID)
	switch {
	case errors.Is(err, bridge.ErrNoPageDownload):
		httpx.JSON(w, 200, map[string]any{"tabId": tabID, "state": "none"})
	case pd != nil:
		httpx.JSON(w, 200, map[string]any{"tabId": tabID, "state": pd.State, "download": pd})
	default:
		httpx.Error(w, 500, err)
	}
}

// @Endpoint GET /tabs/{id}/download/wait
func (h *Handlers) HandleTabPageDownloadWait(w http.ResponseWriter, r *http.Request) {
	h.withPathTabID(w, r, h.HandlePageDownloadWait)
}

// HandlePageDownloadContent streams the bytes of a finished download, from the
// browser's disk wherever that is.
//
// @Endpoint GET /download/content
func (h *Handlers) HandlePageDownloadContent(w http.ResponseWriter, r *http.Request) {
	if !h.Config.AllowDownload {
		h.writeCapabilityDisabled(w, routes.CapDownload)
		return
	}
	q := r.URL.Query()
	// A tab is needed only to read a file off a remote browser's disk: the one
	// named, else the current one, else the bridge opens a blank tab, since a
	// `download <url>` may have been the session's first call.
	var tabCtx context.Context
	tabID := q.Get("tabId")
	if _, _, err := h.tabContext(r, tabID); tabID != "" || err == nil {
		ctx, _, ok := h.guardedTabContext(w, r, tabID, guardDialogBlocked)
		if !ok {
			return
		}
		tabCtx = ctx
	}
	guid := q.Get("guid")
	pd, found := h.Bridge.FindPageDownload(guid)
	if !found {
		httpx.ErrorCode(w, 404, "download_not_found", fmt.Sprintf("no download %q", guid), false, nil)
		return
	}
	if pd.State != bridge.PageDownloadCompleted {
		httpx.ErrorCode(w, 409, "download_not_completed", fmt.Sprintf("download %s is %s", pd.Filename, pd.State), false, nil)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(pd.ReceivedBytes, 10))
	// A failure after the first byte can only cut the body short; the CLI
	// compares what it got with Content-Length.
	_, _ = h.Bridge.CopyPageDownload(tabCtx, pd, w)
}

// HandlePageDownloads lists every download pinchtab has followed, with its
// progress: what `pinchtab downloads` prints, and what a monitor polls.
//
// @Endpoint GET /downloads
func (h *Handlers) HandlePageDownloads(w http.ResponseWriter, r *http.Request) {
	if !h.Config.AllowDownload {
		h.writeCapabilityDisabled(w, routes.CapDownload)
		return
	}
	all := h.Bridge.PageDownloads()
	if all == nil {
		all = []bridge.PageDownload{}
	}
	httpx.JSON(w, 200, map[string]any{"downloads": all})
}

// actionDownloadGrace is how long an unacknowledged action waits for a
// download to begin: a click that starts one often never completes as a click.
const actionDownloadGrace = time.Second

// noteActionDownloads adds the downloads an action started to its result, so
// the step reports them; such an action counts as acknowledged.
func (h *Handlers) noteActionDownloads(ctx context.Context, result map[string]any, tabID string, mark uint64) map[string]any {
	dls := h.Bridge.PageDownloadsSince(tabID, mark)
	if len(dls) == 0 && result != nil && result["confirmed"] == false {
		deadline := time.Now().Add(actionDownloadGrace)
		for len(dls) == 0 && time.Now().Before(deadline) && ctx.Err() == nil {
			time.Sleep(100 * time.Millisecond)
			dls = h.Bridge.PageDownloadsSince(tabID, mark)
		}
	}
	if len(dls) == 0 {
		return result
	}
	if result == nil {
		result = map[string]any{}
	}
	result["downloads"] = dls
	delete(result, "confirmed")
	delete(result, "warning")
	return result
}

// @Endpoint GET /tabs/{id}/download/content
func (h *Handlers) HandleTabPageDownloadContent(w http.ResponseWriter, r *http.Request) {
	h.withPathTabID(w, r, h.HandlePageDownloadContent)
}
