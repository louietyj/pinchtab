package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/pinchtab/pinchtab/internal/activity"
	"github.com/pinchtab/pinchtab/internal/bridge"
)

func (h *Handlers) tabContext(r *http.Request, tabID string) (context.Context, string, error) {
	tabID = strings.TrimSpace(tabID)
	scope := currentTabScopeFromRequest(r)
	explicitTab := tabID != ""

	if !explicitTab && !scope.IsGlobal() {
		storedTabID, ok := h.CurrentTabs.Get(scope)
		if !ok {
			return nil, "", noCurrentTabError(scope.Description())
		}
		tabID = storedTabID
	}

	ctx, resolvedID, err := h.Bridge.TabContext(tabID)
	var unfreeze *bridge.TabUnfreezeError
	if err != nil && !explicitTab && !scope.IsGlobal() && !errors.As(err, &unfreeze) {
		h.CurrentTabs.Clear(scope)
		return nil, "", noCurrentTabError(scope.Description())
	}
	if err == nil {
		h.holdTabAwakeForRequest(r, resolvedID)
		h.setCurrentTabForRequest(r, resolvedID)
		h.recordActivity(r, activity.Update{TabID: resolvedID})
	}
	return ctx, resolvedID, err
}

// tabContextWithHeader resolves the tab and sets the resolved tab ID as a
// response header so the orchestrator proxy can enrich activity events
// even for large responses (e.g. snapshots) that exceed the body-inspection threshold.
func (h *Handlers) tabContextWithHeader(w http.ResponseWriter, r *http.Request, tabID string) (context.Context, string, error) {
	ctx, resolvedID, err := h.tabContext(r, tabID)
	if err == nil {
		w.Header().Set(activity.HeaderPTTabID, resolvedID)
	}
	return ctx, resolvedID, err
}

func (h *Handlers) recordActivity(r *http.Request, update activity.Update) {
	activity.EnrichRequest(r, update)
}

func (h *Handlers) recordNavigateRequest(r *http.Request, tabID, url string) {
	h.recordActivity(r, activity.Update{
		Action: "navigate",
		TabID:  tabID,
		URL:    url,
	})
}

func (h *Handlers) recordActionRequest(r *http.Request, req bridge.ActionRequest) {
	h.recordActivity(r, activity.Update{
		Action: req.Kind,
		TabID:  req.TabID,
		Ref:    req.Ref,
	})
}

func (h *Handlers) recordReadRequest(r *http.Request, action, tabID string) {
	h.recordActivity(r, activity.Update{
		Action: action,
		TabID:  tabID,
	})
}

func (h *Handlers) recordResolvedURL(r *http.Request, url string) {
	h.recordActivity(r, activity.Update{URL: url})
}

func (h *Handlers) recordResolvedTab(r *http.Request, tabID string) {
	h.recordActivity(r, activity.Update{TabID: tabID})
}

type tabAwakeHolder interface {
	HoldAwakeUntil(ctx context.Context, tabID string)
}

var _ tabAwakeHolder = (*bridge.Bridge)(nil)

func (h *Handlers) holdTabAwakeForRequest(r *http.Request, tabID string) {
	if holder, ok := bridgeAs[tabAwakeHolder](h.Bridge); ok {
		holder.HoldAwakeUntil(r.Context(), tabID)
	}
}

type tabScopeTracker interface {
	RecordTabScope(tabID, scope string, created bool)
	TabsOnlyUsedByCreator(scope string) []string
}

var _ tabScopeTracker = (*bridge.Bridge)(nil)

func (h *Handlers) tabScopes() (tabScopeTracker, bool) {
	if h == nil {
		return nil, false
	}
	tracker, ok := bridgeAs[tabScopeTracker](h.Bridge)
	return tracker, ok
}

func (h *Handlers) markCreatedTab(w http.ResponseWriter, r *http.Request, tabID string) {
	if strings.TrimSpace(tabID) == "" {
		return
	}
	if tracker, ok := h.tabScopes(); ok {
		tracker.RecordTabScope(tabID, currentTabScopeFromRequest(r).key, true)
	}
	if w == nil {
		return
	}
	w.Header().Set(activity.HeaderPTTabID, tabID)
	w.Header().Set(activity.HeaderPTTabCreated, "true")
}

func (h *Handlers) setCurrentTabForRequest(r *http.Request, tabID string) {
	if h == nil {
		return
	}
	scope := currentTabScopeFromRequest(r)
	if tracker, ok := h.tabScopes(); ok {
		tracker.RecordTabScope(tabID, scope.key, false)
	}
	if h.CurrentTabs == nil {
		return
	}
	h.CurrentTabs.Set(scope, tabID)
}

func (h *Handlers) clearCurrentTabReferences(tabID string) {
	if h == nil || h.CurrentTabs == nil {
		return
	}
	h.CurrentTabs.ClearTab(tabID)
}

func (h *Handlers) scopedCurrentTabForRequest(r *http.Request) (string, bool) {
	if h == nil || h.CurrentTabs == nil {
		return "", false
	}
	return h.CurrentTabs.Get(currentTabScopeFromRequest(r))
}
