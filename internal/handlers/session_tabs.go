package handlers

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/pinchtab/pinchtab/internal/activity"
	"github.com/pinchtab/pinchtab/internal/httpx"
)

const SessionTabsClosePath = "/internal/session-tabs/close"

type keptSessionTab struct {
	TabID  string `json:"tabId"`
	Reason string `json:"reason"`
}

type sessionTabsCloseResult struct {
	Closed []string         `json:"closed"`
	Kept   []keptSessionTab `json:"kept"`
}

func (h *Handlers) HandleSessionTabsClose(w http.ResponseWriter, r *http.Request) {
	if !IsTrustedInternalProxy(r) {
		httpx.NoRoute(w, r, http.StatusNotFound)
		return
	}
	sessionID := strings.TrimSpace(r.Header.Get(activity.HeaderPTSessionID))
	if sessionID == "" {
		httpx.ErrorCode(w, http.StatusBadRequest, "session_id_required", activity.HeaderPTSessionID+" is required", false, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, h.closeSessionTabs(sessionID))
}

func (h *Handlers) closeSessionTabs(sessionID string) sessionTabsCloseResult {
	scope := scopedCurrentTab(currentTabScopeSession, sessionID)
	result := sessionTabsCloseResult{Closed: []string{}, Kept: []keptSessionTab{}}
	if h.CurrentTabs != nil {
		h.CurrentTabs.Clear(scope)
	}
	tracker, ok := h.tabScopes()
	if !ok {
		return result
	}
	for _, tabID := range tracker.TabsOnlyUsedByCreator(scope.key) {
		reason := h.sessionTabKeepReason(tabID)
		if reason == "" {
			if err := h.Bridge.CloseTab(tabID); err != nil {
				reason = "close failed: " + err.Error()
			}
		}
		if reason != "" {
			slog.Info("ended session's tab kept open", "sessionId", sessionID, "tabId", tabID, "reason", reason)
			result.Kept = append(result.Kept, keptSessionTab{TabID: tabID, Reason: reason})
			continue
		}
		h.clearCurrentTabReferences(tabID)
		result.Closed = append(result.Closed, tabID)
	}
	if len(result.Closed) > 0 {
		slog.Info("closed ended session's tabs", "sessionId", sessionID, "tabIds", result.Closed)
	}
	return result
}

func (h *Handlers) sessionTabKeepReason(tabID string) string {
	if err := h.enforceTabNotPausedForHandoff(tabID); err != nil {
		return "paused for human handoff"
	}
	if lock := h.Bridge.TabLockInfo(tabID); lock != nil {
		return "locked by " + lock.Owner
	}
	return ""
}
