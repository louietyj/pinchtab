package types

const (
	HealthStatusOK       = "ok"
	HealthStatusDegraded = "degraded"
)

// ModeDashboard is the /health "mode" the front-door dashboard server reports. A
// bridge or instance server reports no mode, so a reader comparing against this
// const treats those as non-dashboard rather than by accident — and a rename
// stays one edit instead of a literal scattered across producers and readers.
const ModeDashboard = "dashboard"

func HealthStatusServing(status string) bool {
	return status == HealthStatusOK || status == HealthStatusDegraded
}
