package bridge

import (
	"log/slog"
	"time"

	"github.com/pinchtab/pinchtab/internal/config"
)

func (tm *TabManager) ScheduleIdleLifecycle(tabID string) {
	if tm == nil || tm.config == nil || !config.IdleTabLifecycle(tm.config.TabLifecyclePolicy) {
		return
	}
	delay := tm.config.TabCloseDelay
	if delay <= 0 {
		return
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()
	entry, ok := tm.tabs[tabID]
	if !ok {
		return
	}
	entry.stopIdleTimer()
	gen := entry.idleGen
	entry.idleTimer = time.AfterFunc(delay, func() {
		tm.idleFire(tabID, gen)
	})
}

func (tm *TabManager) CancelIdleLifecycle(tabID string) {
	if tm == nil {
		return
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if entry, ok := tm.tabs[tabID]; ok {
		entry.stopIdleTimer()
	}
}

func (e *TabEntry) stopIdleTimer() {
	if e.idleTimer != nil {
		e.idleTimer.Stop()
		e.idleTimer = nil
	}
	e.idleGen++
}

func (tm *TabManager) freezesIdleTabs() bool {
	return tm.config != nil && tm.config.TabLifecyclePolicy == "freeze_idle"
}

func (tm *TabManager) idleFire(tabID string, gen uint64) {
	if tm.freezesIdleTabs() {
		tm.freezeIdleTab(tabID, gen)
		return
	}
	tm.mu.Lock()
	entry, ok := tm.tabs[tabID]
	if !ok || entry.idleGen != gen {
		tm.mu.Unlock()
		return
	}
	entry.idleTimer = nil
	tm.mu.Unlock()

	if err := tm.CloseTab(tabID); err != nil {
		slog.Debug("auto-close tab failed", "tabId", tabID, "err", err)
		return
	}
	slog.Info("tab auto-closed", "tabId", tabID, "reason", "auto_close")
}
