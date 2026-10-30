package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/pinchtab/pinchtab/internal/activity"
	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/httpx"
	"github.com/pinchtab/pinchtab/internal/routes"
)

const (
	maxWaitTimeout   = 30_000 // 30s max
	defaultTimeout   = 10_000 // 10s default
	pollInterval     = 250 * time.Millisecond
	maxFixedWaitMS   = 30_000
	defaultIdleForMS = 500
	maxIdleForMS     = 10_000
	networkIdlePoll  = 100 * time.Millisecond
)

// waitRequest is the JSON body for POST /wait and POST /tabs/{id}/wait.
type waitRequest struct {
	TabID    string `json:"tabId,omitempty"`
	Selector string `json:"selector,omitempty"` // CSS/XPath/text selector
	State    string `json:"state,omitempty"`    // "visible" (default) or "hidden"
	Text     string `json:"text,omitempty"`     // wait for text on page
	NotText  string `json:"notText,omitempty"`  // wait for text to disappear from page
	URL      string `json:"url,omitempty"`      // wait for URL glob match
	Load     string `json:"load,omitempty"`     // "ready-state" | "content-loaded" | "network-idle" (alias: networkidle)
	Fn       string `json:"fn,omitempty"`       // JS expression to poll for truthy
	Ms       *int   `json:"ms,omitempty"`       // fixed duration wait
	Timeout  *int   `json:"timeout,omitempty"`  // timeout in ms
	IdleFor  *int   `json:"idleFor,omitempty"`  // network-idle quiet period in ms (default 500, max 10000)
}

// waitResponse is the JSON response for wait endpoints.
type waitResponse struct {
	Waited  bool   `json:"waited"`
	Elapsed int64  `json:"elapsed"`
	Match   string `json:"match,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (wr *waitRequest) mode() string {
	switch {
	case wr.Ms != nil:
		return "ms"
	case wr.Selector != "":
		return "selector"
	case wr.Text != "":
		return "text"
	case wr.NotText != "":
		return "notText"
	case wr.URL != "":
		return "url"
	case wr.Load != "":
		return "load"
	case wr.Fn != "":
		return "fn"
	default:
		return ""
	}
}

func (wr *waitRequest) resolvedTimeout() time.Duration {
	ms := defaultTimeout
	if wr.Timeout != nil {
		ms = *wr.Timeout
	}
	if ms < 100 {
		ms = 100
	}
	if ms > maxWaitTimeout {
		ms = maxWaitTimeout
	}
	return time.Duration(ms) * time.Millisecond
}

func (wr *waitRequest) resolvedIdleFor() time.Duration {
	ms := defaultIdleForMS
	if wr.IdleFor != nil {
		ms = *wr.IdleFor
	}
	if ms < 0 {
		ms = 0
	}
	if ms > maxIdleForMS {
		ms = maxIdleForMS
	}
	return time.Duration(ms) * time.Millisecond
}

// canonicalLoadState maps the user-supplied --load value to a canonical
// state name and returns false if the value is not recognised. Accepts
// the legacy "networkidle" spelling as an alias for "network-idle".
func canonicalLoadState(s string) (string, bool) {
	switch s {
	case "ready-state", "content-loaded", "network-idle":
		return s, true
	case "networkidle":
		return "network-idle", true
	default:
		return "", false
	}
}

// HandleWait handles POST /wait.
//
// @Endpoint POST /wait
func (h *Handlers) HandleWait(w http.ResponseWriter, r *http.Request) {
	var req waitRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodySize)).Decode(&req); err != nil {
		httpx.Error(w, 400, fmt.Errorf("decode: %w", err))
		return
	}

	h.handleWaitCore(w, r, req)
}

// HandleTabWait handles POST /tabs/{id}/wait.
//
// @Endpoint POST /tabs/{id}/wait
func (h *Handlers) HandleTabWait(w http.ResponseWriter, r *http.Request) {
	h.withPathTabIDBody(w, r, h.HandleWait)
}

func (h *Handlers) handleWaitCore(w http.ResponseWriter, r *http.Request, req waitRequest) {
	start := time.Now()

	mode := req.mode()
	if mode == "" {
		httpx.Error(w, 400, fmt.Errorf("one of selector, text, url, load, fn, or ms is required"))
		return
	}

	h.recordActivity(r, activity.Update{Action: "wait." + mode, TabID: req.TabID})
	if mode == "fn" && !h.evaluateEnabled() {
		h.writeCapabilityDisabled(w, routes.CapEvaluate)
		return
	}

	if mode == "ms" {
		if _, tabID, err := h.tabContext(r, req.TabID); err == nil && h.refuseIfDialogBlocked(w, tabID) {
			return
		}
		ms := *req.Ms
		if ms < 0 {
			ms = 0
		}
		if ms > maxFixedWaitMS {
			ms = maxFixedWaitMS
		}
		select {
		case <-time.After(time.Duration(ms) * time.Millisecond):
			httpx.JSON(w, 200, waitResponse{
				Waited:  true,
				Elapsed: time.Since(start).Milliseconds(),
				Match:   fmt.Sprintf("%dms", ms),
			})
		case <-r.Context().Done():
			httpx.JSON(w, 200, waitResponse{
				Waited:  false,
				Elapsed: time.Since(start).Milliseconds(),
				Error:   "cancelled",
			})
		}
		return
	}

	ctx, resolvedTabID, ok := h.guardedTabContext(w, r, req.TabID, guardDialogBlocked|guardDomainPolicy)
	if !ok {
		return
	}

	timeout := req.resolvedTimeout()
	tCtx, tCancel := context.WithTimeout(ctx, timeout)
	defer tCancel()
	go httpx.CancelOnClientDone(r.Context(), tCancel)

	var js string
	var matchLabel string

	switch mode {
	case "selector":
		matchLabel = req.Selector
	case "text":
		js = fmt.Sprintf(`document.body && document.body.innerText.includes(%s)`, jsonStr(req.Text))
		matchLabel = req.Text
	case "notText":
		js = fmt.Sprintf(`!document.body || !document.body.innerText.includes(%s)`, jsonStr(req.NotText))
		matchLabel = "!" + req.NotText
	case "url":
		matchLabel = req.URL
	case "load":
		canonical, ok := canonicalLoadState(req.Load)
		if !ok {
			httpx.Error(w, 400, fmt.Errorf("unsupported load state: %s (supported: ready-state, content-loaded, network-idle)", req.Load))
			return
		}
		matchLabel = canonical
		switch canonical {
		case "ready-state":
			js = `document.readyState === 'complete'`
		case "content-loaded":
			js = `document.readyState === 'interactive' || document.readyState === 'complete'`
		case "network-idle":
			h.handleNetworkIdleWait(w, tCtx, start, resolvedTabID, req.resolvedIdleFor())
			return
		}
	case "fn":
		js = fmt.Sprintf(`!!(function(){try{return %s}catch(e){return false}})()`, req.Fn)
		matchLabel = "fn"
	}

	// A semantic selector can refresh the tab's refs while it resolves.
	vocabBefore := h.tabVocab(resolvedTabID)
	err := pollUntil(tCtx, pollInterval, func() (bool, error) {
		if mode == "selector" {
			shown := h.selectorShown(tCtx, resolvedTabID, req.Selector)
			return shown != (req.State == "hidden"), nil
		}
		if mode == "url" {
			var href string
			if evalErr := h.Bridge.Evaluate(tCtx, "window.location.href", &href, bridge.EvalOpts{}); evalErr != nil {
				return false, nil
			}
			return matchURLPattern(req.URL, href), nil
		}
		var result bool
		evalErr := h.Bridge.Evaluate(tCtx, js, &result, bridge.EvalOpts{})
		return evalErr == nil && result, nil
	})
	h.publishVocabIfReepoched(w, resolvedTabID, vocabBefore)
	if err == nil {
		httpx.JSON(w, 200, waitResponse{
			Waited:  true,
			Elapsed: time.Since(start).Milliseconds(),
			Match:   matchLabel,
		})
		return
	}
	elapsed := time.Since(start).Milliseconds()
	httpx.JSON(w, 200, waitResponse{
		Waited:  false,
		Elapsed: elapsed,
		Error:   fmt.Sprintf("timeout after %dms waiting for %s", elapsed, mode),
	})
}

// handleNetworkIdleWait polls the per-tab network monitor until in-flight
// requests have stayed at zero for idleFor, or the context is cancelled.
// Falls back to a readyState=='complete' check if the monitor is unavailable
// (e.g. tab created without network capture).
func (h *Handlers) handleNetworkIdleWait(w http.ResponseWriter, ctx context.Context, start time.Time, tabID string, idleFor time.Duration) {
	_, inflight, err := h.waitNetworkIdle(ctx, tabID, idleFor)
	switch {
	case errors.Is(err, errNetworkMonitorUnavailable):
		httpx.JSON(w, 200, waitResponse{
			Waited:  false,
			Elapsed: time.Since(start).Milliseconds(),
			Error:   "network monitor unavailable for tab",
		})
	case err != nil:
		elapsed := time.Since(start).Milliseconds()
		httpx.JSON(w, 200, waitResponse{
			Waited:  false,
			Elapsed: elapsed,
			Error:   fmt.Sprintf("timeout after %dms waiting for network-idle (inflight=%d)", elapsed, inflight),
		})
	default:
		httpx.JSON(w, 200, waitResponse{
			Waited:  true,
			Elapsed: time.Since(start).Milliseconds(),
			Match:   "network-idle",
		})
	}
}

var errNetworkMonitorUnavailable = errors.New("network monitor unavailable for tab")

// waitNetworkIdle polls the per-tab network monitor until in-flight requests have
// stayed at zero for idleFor, or ctx is cancelled. It returns
// errNetworkMonitorUnavailable when the tab has no network buffer, and the last
// observed in-flight count alongside any cancellation error.
func (h *Handlers) waitNetworkIdle(ctx context.Context, tabID string, idleFor time.Duration) (matched bool, inflight int, err error) {
	mon := h.Bridge.NetworkMonitor()
	var buf *bridge.NetworkBuffer
	if mon != nil {
		buf = mon.GetBuffer(tabID)
	}
	if buf == nil {
		return false, 0, errNetworkMonitorUnavailable
	}

	err = pollUntil(ctx, networkIdlePoll, func() (bool, error) {
		count, lastChange := buf.InflightStatus()
		inflight = count
		return count == 0 && time.Since(lastChange) >= idleFor, nil
	})
	if err != nil {
		return false, inflight, err
	}
	return true, inflight, nil
}

func matchURLPattern(pattern, url string) bool {
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
				continue
			}
			b.WriteString("[^/]*")
		case '?':
			b.WriteByte('.')
		case '.', '+', '^', '$', '{', '}', '(', ')', '|', '[', ']', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	re, err := regexp.Compile(b.String())
	if err != nil {
		return false
	}
	return re.MatchString(url)
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// selectorShown is whether sel resolves, the way an action resolves it (frames,
// the topmost dialog, refs, semantic selectors), to an element that is
// rendered. wait and expect answer from this, so they pass exactly when a click
// on sel would have a visible target, and a hidden copy of the text or an
// element in a frame cannot fool them.
func (h *Handlers) selectorShown(ctx context.Context, tabID, sel string) bool {
	req := bridge.ActionRequest{Selector: sel}
	if _, err := h.resolveActionRequestSelectorOnce(ctx, tabID, &req); err != nil || req.NodeID == 0 {
		return false
	}
	rendered, err := h.elementRendered(ctx, req.NodeID)
	return err == nil && rendered
}
