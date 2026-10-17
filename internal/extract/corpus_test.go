package extract

import (
	"bytes"
	"encoding/json"
	"flag"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/bridge/observe"
)

const (
	corpusDir      = "testdata/corpus"
	corpusBaseline = "testdata/corpus/baseline.txt"
	e2eCorpusDir   = "../../tests/e2e/fixtures/corpus"
	e2eManifest    = "../../tests/e2e/fixtures/corpus/manifest.json"
)

var updateManifest = flag.Bool("update", false, "rewrite the e2e corpus manifest with the offline hits and data of every mirrored entry")

type corpusEntry struct {
	name     string
	nodes    []observe.A11yNode
	schema   Schema
	expected map[string]any
}

type fieldOutcome struct {
	path string
	hit  bool
	got  any
	want any
}

func TestCorpus(t *testing.T) {
	entries := loadCorpus(t)
	if len(entries) < 6 {
		t.Fatalf("corpus has %d entries, want at least 6", len(entries))
	}

	var hits, total int
	for _, e := range entries {
		outcomes := scoreEntry(t, e)
		for _, o := range outcomes {
			mark := "miss"
			if o.hit {
				mark = "hit "
			}
			t.Logf("%s %-10s %-28s got=%s want=%s", mark, e.name, o.path, show(o.got), show(o.want))
		}
		entryHits := countHits(outcomes)
		t.Logf("entry %-10s %d/%d", e.name, entryHits, len(outcomes))
		hits += entryHits
		total += len(outcomes)
	}

	precision := roundPrecision(float64(hits) / float64(total))
	baseline := readBaseline(t)
	t.Logf("precision %.4f (%d/%d fields), baseline %.4f", precision, hits, total, baseline)
	if precision < baseline {
		t.Fatalf("corpus precision %.4f fell below the baseline %.4f in %s", precision, baseline, corpusBaseline)
	}
}

func TestCorpus_E2EMirrorMatchesUnitCorpus(t *testing.T) {
	manifest := readE2EManifest(t)
	entries := loadCorpus(t)
	if len(manifest) != len(entries) && !*updateManifest {
		t.Errorf("%s lists %d entries, the unit corpus has %d", e2eManifest, len(manifest), len(entries))
	}
	for _, e := range entries {
		m, listed := manifest[e.name]
		_, statErr := os.Stat(filepath.Join(e2eCorpusDir, e.name+".html"))
		mirrored := statErr == nil
		switch {
		case m.Offline != "":
			if mirrored {
				t.Errorf("%s is offline-only but has an e2e HTML fixture", e.name)
			}
			if m.Hits != nil || m.Data != nil {
				t.Errorf("%s is offline-only but carries offline hits or data", e.name)
			}
		case !mirrored && !listed:
			t.Errorf("%s is missing from %s", e.name, e2eManifest)
		case !mirrored:
			t.Errorf("%s needs either an e2e HTML fixture or an offline reason", e.name)
		default:
			for _, file := range []string{"schema.json", "expected.json"} {
				unit := mustRead(t, filepath.Join(corpusDir, e.name, file))
				mirror := mustRead(t, filepath.Join(e2eCorpusDir, e.name+"."+file))
				if !bytes.Equal(unit, mirror) {
					t.Errorf("%s: e2e copy of %s differs from the unit corpus", e.name, file)
				}
			}
			data := resolveEntry(t, e)
			hits := countHits(compareObject("", data, e.expected))
			if *updateManifest {
				manifest[e.name] = e2eEntry{Hits: &hits, Data: data}
				continue
			}
			checkManifestEntry(t, e.name, m, hits, data)
		}
	}
	if *updateManifest {
		writeE2EManifest(t, manifest)
	}
}

func checkManifestEntry(t *testing.T, name string, m e2eEntry, hits int, data map[string]any) {
	t.Helper()
	switch {
	case m.Hits == nil:
		t.Errorf("%s: %s carries no offline hits; run go test ./internal/extract -run TestCorpus_E2EMirror -update", name, e2eManifest)
	case *m.Hits != hits:
		t.Errorf("%s: %s records %d offline hits, the offline run scores %d; run go test ./internal/extract -run TestCorpus_E2EMirror -update", name, e2eManifest, *m.Hits, hits)
	}
	if reflect.DeepEqual(m.Data, data) {
		return
	}
	reported := map[string]bool{}
	for _, o := range append(compareObject("", m.Data, data), swapped(compareObject("", data, m.Data))...) {
		if !o.hit && !reported[o.path] {
			reported[o.path] = true
			t.Errorf("%s: %s records %s=%s, the offline run extracts %s", name, e2eManifest, o.path, show(o.got), show(o.want))
		}
	}
	t.Errorf("%s: %s data differs from the offline run; run go test ./internal/extract -run TestCorpus_E2EMirror -update", name, e2eManifest)
}

func loadCorpus(t *testing.T) []corpusEntry {
	t.Helper()
	dirs, err := os.ReadDir(corpusDir)
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	var entries []corpusEntry
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		dir := filepath.Join(corpusDir, d.Name())
		var snap struct {
			Nodes []observe.A11yNode `json:"nodes"`
		}
		if err := json.Unmarshal(mustRead(t, filepath.Join(dir, "snapshot.json")), &snap); err != nil {
			t.Fatalf("%s: decode snapshot: %v", d.Name(), err)
		}
		schema, err := ParseSchema(mustRead(t, filepath.Join(dir, "schema.json")))
		if err != nil {
			t.Fatalf("%s: parse schema: %v", d.Name(), err)
		}
		var expected map[string]any
		if err := json.Unmarshal(mustRead(t, filepath.Join(dir, "expected.json")), &expected); err != nil {
			t.Fatalf("%s: decode expected: %v", d.Name(), err)
		}
		entries = append(entries, corpusEntry{name: d.Name(), nodes: snap.Nodes, schema: schema, expected: expected})
	}
	return entries
}

func scoreEntry(t *testing.T, e corpusEntry) []fieldOutcome {
	t.Helper()
	return compareObject("", resolveEntry(t, e), e.expected)
}

func resolveEntry(t *testing.T, e corpusEntry) map[string]any {
	t.Helper()
	raw, err := json.Marshal(Resolve(e.schema, e.nodes, Options{}).Data)
	if err != nil {
		t.Fatalf("%s: encode data: %v", e.name, err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%s: decode data: %v", e.name, err)
	}
	return got
}

func compareObject(prefix string, got, want map[string]any) []fieldOutcome {
	var out []fieldOutcome
	for _, key := range sortedKeys(want) {
		path := prefix + key
		wantItems, isArray := want[key].([]any)
		if !isArray {
			out = append(out, fieldOutcome{path: path, hit: reflect.DeepEqual(got[key], want[key]), got: got[key], want: want[key]})
			continue
		}
		gotItems, _ := got[key].([]any)
		for i := 0; i < len(wantItems) || i < len(gotItems); i++ {
			itemPath := path + "[" + strconv.Itoa(i) + "]"
			if i >= len(wantItems) {
				out = append(out, fieldOutcome{path: itemPath, got: gotItems[i]})
				continue
			}
			wantItem, _ := wantItems[i].(map[string]any)
			var gotItem map[string]any
			if i < len(gotItems) {
				gotItem, _ = gotItems[i].(map[string]any)
			}
			out = append(out, compareObject(itemPath+".", gotItem, wantItem)...)
		}
	}
	return out
}

func swapped(outcomes []fieldOutcome) []fieldOutcome {
	for i := range outcomes {
		outcomes[i].got, outcomes[i].want = outcomes[i].want, outcomes[i].got
	}
	return outcomes
}

func countHits(outcomes []fieldOutcome) int {
	hits := 0
	for _, o := range outcomes {
		if o.hit {
			hits++
		}
	}
	return hits
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func show(v any) string {
	if v == nil {
		return "-"
	}
	raw, _ := json.Marshal(v)
	s := string(raw)
	if len(s) > 60 {
		s = s[:57] + "..."
	}
	return s
}

func roundPrecision(p float64) float64 {
	return math.Round(p*1e4) / 1e4
}

func readBaseline(t *testing.T) float64 {
	t.Helper()
	b, err := strconv.ParseFloat(strings.TrimSpace(string(mustRead(t, corpusBaseline))), 64)
	if err != nil {
		t.Fatalf("parse baseline: %v", err)
	}
	return b
}

type e2eEntry struct {
	Hits    *int           `json:"hits,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
	Offline string         `json:"offline,omitempty"`
}

func readE2EManifest(t *testing.T) map[string]e2eEntry {
	t.Helper()
	var manifest struct {
		Entries map[string]e2eEntry `json:"entries"`
	}
	if err := json.Unmarshal(mustRead(t, e2eManifest), &manifest); err != nil {
		t.Fatalf("decode %s: %v", e2eManifest, err)
	}
	return manifest.Entries
}

func writeE2EManifest(t *testing.T, entries map[string]e2eEntry) {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(struct {
		Entries map[string]e2eEntry `json:"entries"`
	}{entries}); err != nil {
		t.Fatalf("encode %s: %v", e2eManifest, err)
	}
	if err := os.WriteFile(e2eManifest, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write %s: %v", e2eManifest, err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
