package bridge

import "sort"

func (tm *TabManager) RecordTabScope(tabID, scope string, created bool) {
	if tm == nil || tabID == "" || scope == "" {
		return
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	entry, ok := tm.tabs[tabID]
	if !ok {
		return
	}
	if created && entry.CreatorScope == "" {
		entry.CreatorScope = scope
	}
	if entry.CreatorScope != "" && scope != entry.CreatorScope {
		entry.UsedByOtherScope = true
	}
}

func (tm *TabManager) TabsOnlyUsedByCreator(scope string) []string {
	if tm == nil || scope == "" {
		return nil
	}
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	var ids []string
	for id, entry := range tm.tabs {
		if entry.CreatorScope == scope && !entry.UsedByOtherScope {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}
