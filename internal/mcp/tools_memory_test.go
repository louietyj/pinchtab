package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func memoryMockServer(t *testing.T, seen *[]string, snapshotStatus int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/memory":
			_ = json.NewEncoder(w).Encode(map[string]any{"tabId": "t1", "usedJSHeapSize": 1024, "gc": r.URL.Query().Get("gc") == "true"})
		case "/memory/snapshot":
			w.WriteHeader(snapshotStatus)
			if snapshotStatus != http.StatusOK {
				_ = json.NewEncoder(w).Encode(map[string]any{"code": "memory_disabled", "error": "this endpoint requires the memory capability"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "heap_1", "path": "/state/heapsnapshots/heap_1.heapsnapshot", "bytes": 2048, "nodeCount": 20, "durationMs": 5})
		case "/memory/snapshot/heap_1/summary":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "heap_1", "path": "/state/heapsnapshots/heap_1.heapsnapshot", "top": 5,
				"nodeCount": 20, "edgeCount": 6,
				"topBySize": []map[string]any{{"name": "Array", "count": 3, "selfSize": 1664}},
			})
		case "/memory/compare":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"base": map[string]any{"id": r.URL.Query().Get("base")}, "head": map[string]any{"id": r.URL.Query().Get("head")},
				"constructors": []map[string]any{{"name": "(array)", "sizeDelta": 15728640}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestMemoryToolForwardsGCAndTab(t *testing.T) {
	var seen []string
	srv := memoryMockServer(t, &seen, http.StatusOK)
	defer srv.Close()

	body := resultJSON(t, callSharedTool(t, NewClient(srv.URL, ""), "pinchtab_memory", map[string]any{"tabId": "t1", "gc": true}))
	if len(seen) != 1 || seen[0] != "GET /memory?gc=true&tabId=t1" {
		t.Fatalf("requests = %v", seen)
	}
	if body["gc"] != true {
		t.Fatalf("result = %v", body)
	}
}

func TestMemorySnapshotToolReturnsThePathAndTheSummaryOnly(t *testing.T) {
	var seen []string
	srv := memoryMockServer(t, &seen, http.StatusOK)
	defer srv.Close()

	body := resultJSON(t, callSharedTool(t, NewClient(srv.URL, ""), "pinchtab_memory_snapshot", map[string]any{"tabId": "t1", "top": float64(5)}))
	if len(seen) != 2 || seen[0] != "POST /memory/snapshot" || seen[1] != "GET /memory/snapshot/heap_1/summary?top=5" {
		t.Fatalf("requests = %v, want the snapshot then its summary", seen)
	}
	if body["id"] != "heap_1" || body["path"] != "/state/heapsnapshots/heap_1.heapsnapshot" {
		t.Fatalf("result = %v", body)
	}
	summary, _ := body["summary"].(map[string]any)
	rows, _ := summary["topBySize"].([]any)
	if len(rows) != 1 || summary["nodeCount"] != float64(20) {
		t.Fatalf("summary = %v", summary)
	}
	if _, dup := summary["path"]; dup {
		t.Fatalf("summary repeats the path the result already carries: %v", summary)
	}
}

func TestMemorySnapshotToolSurfacesTheCapabilityRefusal(t *testing.T) {
	var seen []string
	srv := memoryMockServer(t, &seen, http.StatusForbidden)
	defer srv.Close()

	result := callSharedTool(t, NewClient(srv.URL, ""), "pinchtab_memory_snapshot", map[string]any{})
	if !result.IsError {
		t.Fatal("a refused snapshot was reported as success")
	}
	if len(seen) != 1 {
		t.Fatalf("requests = %v, want no summary call after a refusal", seen)
	}
	if text := resultText(t, result); !strings.Contains(text, "memory_disabled") {
		t.Fatalf("refusal text = %q, want the capability code", text)
	}
}

func TestMemoryCompareToolForwardsTheIdsTopAndRetained(t *testing.T) {
	var seen []string
	srv := memoryMockServer(t, &seen, http.StatusOK)
	defer srv.Close()

	body := resultJSON(t, callSharedTool(t, NewClient(srv.URL, ""), "pinchtab_memory_compare", map[string]any{"base": "heap_a", "head": "heap_b", "top": float64(5), "retained": true}))
	if len(seen) != 1 || seen[0] != "GET /memory/compare?base=heap_a&head=heap_b&retained=true&top=5" {
		t.Fatalf("requests = %v", seen)
	}
	rows, _ := body["constructors"].([]any)
	if len(rows) != 1 {
		t.Fatalf("result = %v", body)
	}

	seen = nil
	result := callSharedTool(t, NewClient(srv.URL, ""), "pinchtab_memory_compare", map[string]any{"base": "heap_a"})
	if !result.IsError || len(seen) != 0 {
		t.Fatalf("a compare without head = error %v after requests %v, want a local refusal", result.IsError, seen)
	}
}
