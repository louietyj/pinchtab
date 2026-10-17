package dashboard

import (
	"testing"

	"github.com/pinchtab/pinchtab/internal/bridge"
)

type probingInstances struct {
	instances []bridge.Instance
	probed    string
}

func (s *probingInstances) List() []bridge.Instance { return s.instances }

func (s *probingInstances) CrashSummary() bridge.CrashSummary {
	for i := range s.instances {
		s.instances[i].Responsiveness = s.probed
	}
	return bridge.CrashSummary{}
}

func TestServerModeHealthDegradesWhileAnInstanceIsUnresponsive(t *testing.T) {
	body := healthBody(t, plainInstances{instances: []bridge.Instance{
		{ID: "inst_ok", Status: "running", Responsiveness: bridge.ResponsivenessResponsive},
		{ID: "inst_wedged", Status: "running", Responsiveness: bridge.ResponsivenessUnresponsive},
	}})
	if body["status"] != "degraded" {
		t.Errorf("status = %v, want degraded while an instance is unresponsive", body["status"])
	}
	named, _ := body["unresponsiveInstances"].([]any)
	if len(named) != 1 || named[0] != "inst_wedged" {
		t.Errorf("unresponsiveInstances = %v, want [inst_wedged]", body["unresponsiveInstances"])
	}
	def, _ := body["defaultInstance"].(map[string]any)
	if def["responsiveness"] != bridge.ResponsivenessResponsive || def["status"] != "running" {
		t.Errorf("defaultInstance = %v, want its responsiveness beside an unchanged status", def)
	}
}

func TestServerModeHealthStaysOkForResponsiveAndUnknownInstances(t *testing.T) {
	for _, value := range []string{bridge.ResponsivenessResponsive, bridge.ResponsivenessUnknown, ""} {
		body := healthBody(t, plainInstances{instances: []bridge.Instance{{ID: "inst_1", Status: "running", Responsiveness: value}}})
		if body["status"] != "ok" {
			t.Errorf("responsiveness %q: status = %v, want ok", value, body["status"])
		}
		if _, present := body["unresponsiveInstances"]; present {
			t.Errorf("responsiveness %q: unresponsiveInstances present: %v", value, body["unresponsiveInstances"])
		}
	}
}

func TestServerModeHealthReportsTheResponsivenessItJustProbed(t *testing.T) {
	source := &probingInstances{
		instances: []bridge.Instance{{ID: "inst_1", Status: "running", Responsiveness: bridge.ResponsivenessResponsive}},
		probed:    bridge.ResponsivenessUnresponsive,
	}
	body := healthBody(t, source)
	if body["status"] != "degraded" {
		t.Errorf("status = %v, want degraded: the list must be read after the probe, not before", body["status"])
	}
}
