package main

import (
	"slices"
	"testing"
)

func TestNetworkRulesIsRegisteredBesideRouteAndUnroute(t *testing.T) {
	if !slices.Contains(networkCmd.Commands(), networkRulesCmd) {
		t.Fatal("network rules is not registered under network")
	}
	for _, flag := range []string{"tab", "json"} {
		if networkRulesCmd.Flags().Lookup(flag) == nil {
			t.Errorf("network rules lacks --%s, which route and unroute take", flag)
		}
	}
}
