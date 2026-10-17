package main

import (
	"testing"

	"github.com/pinchtab/pinchtab/internal/mcp"
	"github.com/pinchtab/pinchtab/internal/remedy"
)

// remedyVerbsWithoutMCPTool records the pinchtab CLI verbs declared remedies use that have
// no MCP tool counterpart. Each is a decision, not a gap: the MCP surface exposes browser
// control, not server/session/config/profile administration or the download path.
var remedyVerbsWithoutMCPTool = map[string]string{
	"server":   "server lifecycle is CLI/HTTP only",
	"session":  "agent-session administration is CLI/HTTP only",
	"config":   "config editing is CLI/HTTP only",
	"profiles": "profile management is CLI only",
	"download": "no pinchtab_download tool; downloads are a CLI/HTTP path",
}

// A declared remedy is one pinchtab CLI line the CLI renders verbatim; an MCP agent runs
// tools, so the funnel translates the remedy's verb into its MCP tool. This walks every
// remedy declared in the whole binary and pins that each verb either maps to a registered
// tool with the named parameter, or is a recorded no-counterpart verb — so a new remedy
// whose verb has an MCP tool cannot ship untranslated (nav, snap) or mis-translated (dialog).
//
// It reads remedy.Templates() rather than a producer list, so a remedy declared anywhere in
// the binary's import graph is covered; the floor guards against the walk losing its
// producer packages and passing vacuously.
func TestEveryRemedyVerbMapsToItsMCPToolOrIsRecordedCounterpartFree(t *testing.T) {
	declared := remedy.Templates()
	if len(declared) < 8 {
		t.Fatalf("only %d remedies declared (%v); the walk lost its producer packages and would pass vacuously", len(declared), declared)
	}

	seen := map[string]bool{}
	for _, line := range declared {
		for _, words := range remedy.Segments(line) {
			if len(words) < 2 {
				continue
			}
			verb := words[1]
			seen[verb] = true

			name, param, ok := mcp.RemedyToolForVerb(verb)
			if ok {
				if !mcp.ToolInputHasParam(name, param) {
					t.Errorf("remedy verb %q maps to tool %q param %q, which is not a registered tool with that parameter", verb, name, param)
				}
				continue
			}
			if _, recorded := remedyVerbsWithoutMCPTool[verb]; !recorded {
				t.Errorf("remedy %q uses verb %q with no MCP tool mapping and no recorded no-counterpart entry; add it to the MCP verb table or record it here with a reason", line, verb)
			}
		}
	}

	for verb, reason := range remedyVerbsWithoutMCPTool {
		if reason == "" {
			t.Errorf("no-counterpart verb %q is recorded without a reason", verb)
		}
		if !seen[verb] {
			t.Errorf("no-counterpart verb %q is recorded but no declared remedy uses it; drop the stale entry", verb)
		}
	}
}
