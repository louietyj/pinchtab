package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func grownFixture(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "heapsnap", "testdata", "grown.heapsnapshot"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func compareRequest(h *Handlers, query string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.HandleMemoryCompare(w, httptest.NewRequest(http.MethodGet, "/memory/compare"+query, nil))
	return w
}

func TestMemoryCompareReportsConstructorGrowth(t *testing.T) {
	h, stateDir := memoryHandlers(t, true)
	seedSnapshot(t, stateDir, "heap_base", committedFixture(t))
	seedSnapshot(t, stateDir, "heap_head", grownFixture(t))

	w := compareRequest(h, "?base=heap_base&head=heap_head&top=3")
	if w.Code != http.StatusOK {
		t.Fatalf("compare = %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "retainedSize") {
		t.Fatalf("compare without retained carries retainedSize: %s", w.Body.String())
	}
	var got memoryCompareResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Top != 3 || got.Retained || got.Base.ID != "heap_base" || got.Head.ID != "heap_head" || got.Changed != 7 || got.SizeDelta != 7284 {
		t.Fatalf("compare header = %+v", got)
	}
	if len(got.Constructors) != 3 || got.Constructors[0].Name != "(array)" || got.Constructors[0].SizeDelta != 9200 {
		t.Fatalf("constructors = %+v", got.Constructors)
	}
	if len(got.NewDuplicateStrings) != 1 || got.NewDuplicateStrings[0].Value != "new-dup-string" {
		t.Fatalf("new duplicate strings = %+v", got.NewDuplicateStrings)
	}

	w = compareRequest(h, "?base=heap_base&head=heap_head&top=5&retained=true")
	if w.Code != http.StatusOK {
		t.Fatalf("compare retained = %d %s", w.Code, w.Body.String())
	}
	got = memoryCompareResponse{}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Retained || len(got.Constructors) != 5 {
		t.Fatalf("retained compare = %s", w.Body.String())
	}
	for _, row := range got.Constructors {
		if row.RetainedSize == nil {
			t.Fatalf("row %s lacks retainedSize: %s", row.Name, w.Body.String())
		}
	}
	if got.Constructors[4].Name != "Leaker" || *got.Constructors[4].RetainedSize != 10168 {
		t.Fatalf("Leaker row = %+v", got.Constructors[4])
	}
}

func TestMemoryCompareRefusals(t *testing.T) {
	h, stateDir := memoryHandlers(t, true)
	seedSnapshot(t, stateDir, "heap_base", committedFixture(t))
	seedSnapshot(t, stateDir, "heap_broken", strings.Replace(committedFixture(t), `"node_count":20`, `"node_count":3`, 1))

	cases := []struct {
		query  string
		status int
		code   string
	}{
		{"?base=heap_base", http.StatusBadRequest, "bad_snapshot_id"},
		{"?head=heap_base", http.StatusBadRequest, "bad_snapshot_id"},
		{"?base=..&head=heap_base", http.StatusBadRequest, "bad_snapshot_id"},
		{"?base=heap_base&head=heap_base&top=-1", http.StatusBadRequest, "bad_top"},
		{"?base=heap_base&head=heap_base&retained=maybe", http.StatusBadRequest, "bad_retained"},
		{"?base=heap_missing&head=heap_base", http.StatusNotFound, "memory_snapshot_not_found"},
		{"?base=heap_base&head=heap_missing", http.StatusNotFound, "memory_snapshot_not_found"},
		{"?base=heap_base&head=heap_broken", http.StatusUnprocessableEntity, "memory_snapshot_invalid"},
	}
	for _, tc := range cases {
		w := compareRequest(h, tc.query)
		if w.Code != tc.status || errorCode(t, w) != tc.code {
			t.Errorf("%s = %d %s, want %d %s", tc.query, w.Code, w.Body.String(), tc.status, tc.code)
		}
	}
	w := compareRequest(h, "?base=heap_base&head=heap_missing")
	if !strings.Contains(w.Body.String(), `"id":"heap_missing"`) {
		t.Errorf("not-found refusal does not name the missing id: %s", w.Body.String())
	}

	off, offDir := memoryHandlers(t, false)
	seedSnapshot(t, offDir, "heap_base", committedFixture(t))
	w = compareRequest(off, "?base=heap_base&head=heap_base")
	if w.Code != http.StatusForbidden || errorCode(t, w) != "memory_disabled" {
		t.Fatalf("compare with allowMemory off = %d %s, want 403 memory_disabled", w.Code, w.Body.String())
	}
}
