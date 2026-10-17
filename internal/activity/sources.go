package activity

const (
	SourceClient       = "client"
	SourceDashboard    = "dashboard"
	SourceServer       = "server"
	SourceBridge       = "bridge"
	SourceOrchestrator = "orchestrator"
	SourceScheduler    = "scheduler"
	SourceMCP          = "mcp"
)

// DashboardAgentSources is the one list of sources that count as dashboard agent
// activity. Both the predicate below and the persisted-log query take their set from
// here, so the live broadcast, the in-memory filter and the store query that feeds the
// restart rebuild cannot select different sets.
func DashboardAgentSources() []string {
	return []string{SourceClient, SourceScheduler}
}

// IsDashboardAgentActivity is the one predicate for "does this event belong in the
// dashboard's per-agent views". The live broadcast and the persisted-log rebuild both
// call it, so a source counted while the server runs is the same source restored after
// a restart; two separately spelled predicates drifted once and dropped scheduled
// actions from the per-agent summaries after any restart.
func IsDashboardAgentActivity(evt Event) bool {
	return matchesAnySource(evt.Source, DashboardAgentSources())
}
