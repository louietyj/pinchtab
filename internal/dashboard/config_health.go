package dashboard

import (
	"net/http"
	"strings"
	"time"

	"github.com/pinchtab/pinchtab/internal/api/types"
	"github.com/pinchtab/pinchtab/internal/authn"
	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/cli/report"
	"github.com/pinchtab/pinchtab/internal/config"
)

type healthInstanceInfo struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	Responsiveness string `json:"responsiveness"`
}

type healthSecurityInfo struct {
	Level                     string   `json:"level"`
	Bind                      string   `json:"bind"`
	AllowedDomains            []string `json:"allowedDomains"`
	IDPIEnabled               bool     `json:"idpiEnabled"`
	EnabledSensitiveEndpoints []string `json:"enabledSensitiveEndpoints"`
	GuardsDown                bool     `json:"guardsDown"`
}

type healthEnvelope struct {
	Status                string               `json:"status"`
	Mode                  string               `json:"mode"`
	Version               string               `json:"version"`
	Uptime                int64                `json:"uptime"`
	AuthRequired          bool                 `json:"authRequired"`
	Profiles              int                  `json:"profiles"`
	TemporaryProfiles     int                  `json:"temporaryProfiles"`
	QuarantinedProfiles   int                  `json:"quarantinedProfiles"`
	Instances             int                  `json:"instances"`
	DefaultInstance       *healthInstanceInfo  `json:"defaultInstance,omitempty"`
	Agents                int                  `json:"agents"`
	RestartRequired       bool                 `json:"restartRequired"`
	RestartReasons        []string             `json:"restartReasons,omitempty"`
	Security              *healthSecurityInfo  `json:"security,omitempty"`
	Crashes               *bridge.CrashSummary `json:"crashes,omitempty"`
	UnresponsiveInstances []string             `json:"unresponsiveInstances,omitempty"`
}

func unresponsiveInstanceIDs(instances []bridge.Instance) []string {
	var ids []string
	for _, inst := range instances {
		if inst.Responsiveness == bridge.ResponsivenessUnresponsive {
			ids = append(ids, inst.ID)
		}
	}
	return ids
}

type crashReporter interface {
	CrashSummary() bridge.CrashSummary
}

type defaultInstancer interface {
	DefaultInstance() (bridge.Instance, bool)
}

func defaultInstanceInfo(lister InstanceLister, instances []bridge.Instance) *healthInstanceInfo {
	def, ok := bridge.Instance{}, false
	if d, isDefaulter := lister.(defaultInstancer); isDefaulter {
		def, ok = d.DefaultInstance()
	}
	if !ok {
		if len(instances) == 0 {
			return nil
		}
		def = instances[0]
	}
	return &healthInstanceInfo{
		ID:             def.ID,
		Status:         def.Status,
		Responsiveness: def.Responsiveness,
	}
}

func (c *ConfigAPI) healthInfo(includeSecurity bool) (healthEnvelope, error) {
	_, _, restartReasons, err := c.currentConfig()
	if err != nil {
		return healthEnvelope{}, err
	}

	profileCount, temporaryCount, quarantinedCount := 0, 0, 0
	if c.profiles != nil {
		profiles, err := c.profiles.List()
		if err == nil {
			for _, p := range profiles {
				// Temporary is checked before Quarantined to match GET /profiles'
				// own precedence, which hides Temporary first: a quarantined
				// temporary (an instance-*.quarantine-* dir) is a temporary the
				// listing hides, so counting it as quarantined would break the
				// reconciliation profiles + quarantinedProfiles == default list length.
				switch {
				case p.Temporary:
					temporaryCount++
				case p.Quarantined:
					quarantinedCount++
				default:
					profileCount++
				}
			}
		}
	}

	var crashes *bridge.CrashSummary
	if reporter, ok := c.instances.(crashReporter); ok {
		if summary := reporter.CrashSummary(); summary.Total > 0 {
			crashes = &summary
		}
	}
	instanceCount := 0
	var defaultInst *healthInstanceInfo
	var unresponsive []string
	if c.instances != nil {
		instances := c.instances.List()
		instanceCount = len(instances)
		unresponsive = unresponsiveInstanceIDs(instances)
		defaultInst = defaultInstanceInfo(c.instances, instances)
	}
	status := types.HealthStatusOK
	if len(unresponsive) > 0 {
		status = types.HealthStatusDegraded
	}
	agentCount := 0
	if c.agents != nil {
		agentCount = c.agents.AgentCount()
	}
	cfg := c.cfg()
	out := healthEnvelope{
		Status:                status,
		Mode:                  types.ModeDashboard,
		Version:               c.version,
		Uptime:                int64(time.Since(c.startedAt).Milliseconds()),
		AuthRequired:          cfg != nil && strings.TrimSpace(cfg.Token) != "",
		Profiles:              profileCount,
		TemporaryProfiles:     temporaryCount,
		QuarantinedProfiles:   quarantinedCount,
		Instances:             instanceCount,
		DefaultInstance:       defaultInst,
		Agents:                agentCount,
		RestartRequired:       len(restartReasons) > 0,
		RestartReasons:        restartReasons,
		Crashes:               crashes,
		UnresponsiveInstances: unresponsive,
	}
	if includeSecurity {
		security := runtimeSecurityInfo(cfg)
		out.Security = &security
	}
	return out, nil
}

func healthSecurityVisibleTo(r *http.Request) bool {
	switch authn.CredentialsFromRequest(r).Method {
	case authn.MethodHeader, authn.MethodCookie:
		return true
	default:
		return false
	}
}

func runtimeSecurityInfo(cfg *config.RuntimeConfig) healthSecurityInfo {
	if cfg == nil {
		return healthSecurityInfo{Level: "UNKNOWN"}
	}
	posture := report.AssessSecurityPosture(cfg)
	enabled := append([]string(nil), cfg.EnabledSensitiveEndpoints()...)
	domains := append([]string(nil), cfg.AllowedDomains...)
	return healthSecurityInfo{
		Level:                     posture.Level,
		Bind:                      cfg.Bind,
		AllowedDomains:            domains,
		IDPIEnabled:               cfg.IDPI.Enabled,
		EnabledSensitiveEndpoints: enabled,
		GuardsDown:                isGuardsDownPosture(cfg),
	}
}

// isGuardsDownPosture reports whether the runtime config matches the
// guards-down preset signature (all sensitive endpoints + attach + IDPI off).
func isGuardsDownPosture(cfg *config.RuntimeConfig) bool {
	if cfg == nil {
		return false
	}
	return cfg.AllowEvaluate &&
		cfg.AllowMacro &&
		cfg.AllowScreencast &&
		cfg.AllowDownload &&
		cfg.AllowCookies &&
		cfg.AllowUpload &&
		cfg.AllowNetworkIntercept &&
		cfg.AllowMemory &&
		cfg.AttachEnabled &&
		!cfg.IDPI.Enabled
}
