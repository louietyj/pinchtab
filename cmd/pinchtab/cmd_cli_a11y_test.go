package main

import "testing"

// The axe engine and its filters must be reachable from the CLI: 'a11y audit'
// wires the flags the endpoint reads, and the subcommand hangs off the a11y
// group so 'pinchtab a11y audit' resolves.
func TestA11yAuditCommandRegistersEngineFlags(t *testing.T) {
	for _, name := range []string{"axe", "engine", "tags", "rules", "include-incomplete", "selector", "tab", "json"} {
		if a11yAuditCmd.Flags().Lookup(name) == nil {
			t.Errorf("a11y audit missing --%s flag", name)
		}
	}
}

func TestA11yAuditIsRegisteredUnderA11y(t *testing.T) {
	var found bool
	for _, c := range a11yCmd.Commands() {
		if c.Name() == "audit" {
			found = true
		}
	}
	if !found {
		t.Fatal("a11y audit subcommand is not registered under a11yCmd")
	}
	if a11yAuditCmd.Flags().Lookup("axe").DefValue != "false" {
		t.Errorf("--axe default = %q, want false (native is the default engine)", a11yAuditCmd.Flags().Lookup("axe").DefValue)
	}
}
