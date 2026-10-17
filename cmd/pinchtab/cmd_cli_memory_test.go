package main

import "testing"

func TestMemoryCommandsAreRegisteredWithTheirFlags(t *testing.T) {
	want := map[string][]string{
		"snapshot": {"tab", "out", "json"},
		"summary":  {"top", "json"},
		"compare":  {"top", "retained", "json"},
	}
	for _, sub := range memoryCmd.Commands() {
		flags, ok := want[sub.Name()]
		if !ok {
			continue
		}
		for _, name := range flags {
			if sub.Flags().Lookup(name) == nil {
				t.Errorf("memory %s missing --%s", sub.Name(), name)
			}
		}
		delete(want, sub.Name())
	}
	for name := range want {
		t.Errorf("memory %s is not registered under memoryCmd", name)
	}
	for _, name := range []string{"gc", "tab", "json"} {
		if memoryCmd.Flags().Lookup(name) == nil {
			t.Errorf("memory missing --%s", name)
		}
	}
	if def := memorySummaryCmd.Flags().Lookup("top").DefValue; def != "20" {
		t.Errorf("summary --top default = %s, want 20", def)
	}
}
