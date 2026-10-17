package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// axeAuditMockServer answers /a11y/audit?engine=axe with one ref-carrying
// violation and stamps the vocabulary token the audit "minted", and echoes each
// /action body so the test can read the token the follow-up interaction sent.
func axeAuditMockServer(token string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/a11y/audit" {
			w.Header().Set(vocabHeader, token)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"engine":          "axe",
				"version":         "4.13.0",
				"vocabularyToken": token,
				"violations": []map[string]any{{
					"id":      "image-alt",
					"impact":  "critical",
					"tags":    []string{"wcag2a", "wcag111"},
					"helpUrl": "https://dequeuniversity.com/rules/axe/4.13/image-alt",
					"nodes":   []map[string]any{{"target": []string{"img"}, "html": "<img src=x>", "ref": "e7"}},
				}},
				"score": 90,
			})
			return
		}
		resp := map[string]any{"path": r.URL.Path}
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			var parsed map[string]any
			if json.Unmarshal(body, &parsed) == nil {
				resp["body"] = parsed
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// The in-process tool-result test the harness calls for: drive pinchtab_a11y_audit
// with engine=axe against a stub endpoint and assert the tool hands back the axe
// violations with a ref an agent can act on.
func TestA11yAuditAxeToolReturnsViolationsWithRefs(t *testing.T) {
	srv := axeAuditMockServer("vocab-axe-1")
	defer srv.Close()
	c := NewClient(srv.URL, "")

	result := callSharedTool(t, c, "pinchtab_a11y_audit", map[string]any{"engine": "axe", "tabId": "t1"})
	body := resultJSON(t, result)

	if body["engine"] != "axe" {
		t.Fatalf("engine = %v, want axe", body["engine"])
	}
	violations, _ := body["violations"].([]any)
	if len(violations) == 0 {
		t.Fatal("tool result carried no violations")
	}
	first, _ := violations[0].(map[string]any)
	if first["id"] != "image-alt" || first["impact"] != "critical" {
		t.Fatalf("violation = %v, want image-alt/critical", first)
	}
	nodes, _ := first["nodes"].([]any)
	if len(nodes) == 0 {
		t.Fatal("violation carried no nodes")
	}
	node0, _ := nodes[0].(map[string]any)
	if node0["ref"] != "e7" {
		t.Fatalf("node ref = %v, want e7 so the agent can act on it", node0["ref"])
	}
}

// Item 1 at the MCP surface: the audit re-epochs the tab and mints a fresh
// vocabulary token, so the tool must capture it and the next interaction must
// echo it. Without this a nav -> a11y audit --axe -> click flow echoes a stale
// token and the server refuses the very ref the audit just handed out with 409.
func TestA11yAuditAxeTokenIsEchoedOnTheNextAction(t *testing.T) {
	srv := axeAuditMockServer("vocab-axe-1")
	defer srv.Close()
	c := NewClient(srv.URL, "")

	callSharedTool(t, c, "pinchtab_a11y_audit", map[string]any{"engine": "axe", "tabId": "t1"})
	clickResult := callSharedTool(t, c, "pinchtab_click", map[string]any{"ref": "e7", "tabId": "t1"})

	got, ok := actionVocabSent(t, clickResult)
	if !ok {
		t.Fatal("the click carried no vocab token, so the ref the audit minted would be refused 409")
	}
	if got != "vocab-axe-1" {
		t.Errorf("click echoed vocab %q, want the token the axe audit returned", got)
	}
}
