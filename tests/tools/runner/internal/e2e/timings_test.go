package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func sampleResults() []suiteTestResult {
	return []suiteTestResult{
		{Name: "[actions-basic] pinchtab click <button>", Status: "passed", DurationMs: 790},
		{Name: "[actions-basic] pinchtab press <key>", Status: "passed", DurationMs: 20},
		{Name: "[audit-basic] sitemap mode discovers pages", Status: "failed", DurationMs: 6050},
		{Name: "ungrouped result with no scenario", Status: "passed", DurationMs: 140},
	}
}

func TestBuildSuiteTimingsGroupsByScenarioAndTotalsReconcile(t *testing.T) {
	timings := buildSuiteTimings("api-extended", "multiCompose", "chrome", "2026-01-01T00:00:00Z", sampleResults())

	if timings.Tests != 4 {
		t.Errorf("Tests = %d, want 4", timings.Tests)
	}

	var recordSum int64
	for _, record := range timings.Records {
		recordSum += record.Ms
	}
	var scenarioSum int64
	for _, scenario := range timings.Scenarios {
		scenarioSum += scenario.Ms
	}
	if timings.TotalMs != recordSum || timings.TotalMs != scenarioSum {
		t.Errorf("totals disagree: suite=%d records=%d scenarios=%d; a totals file whose parts do not add up cannot be diffed against a later run",
			timings.TotalMs, recordSum, scenarioSum)
	}
	if timings.TotalMs != 7000 {
		t.Errorf("TotalMs = %d, want 7000", timings.TotalMs)
	}

	if got := len(timings.Scenarios); got != 3 {
		t.Fatalf("scenarios = %d, want 3", got)
	}
	if timings.Scenarios[0].Scenario != "audit-basic" || timings.Scenarios[0].Ms != 6050 {
		t.Errorf("scenarios are not ordered slowest-first: got %+v", timings.Scenarios[0])
	}
	for _, scenario := range timings.Scenarios {
		if scenario.Scenario == "actions-basic" && scenario.Tests != 2 {
			t.Errorf("actions-basic tests = %d, want 2", scenario.Tests)
		}
	}
}

func TestBuildSuiteTimingsStampsEveryRecordWithItsRunConditions(t *testing.T) {
	timings := buildSuiteTimings("api-extended", "multiCompose", "cloak", "2026-01-01T00:00:00Z", sampleResults())

	for _, record := range timings.Records {
		if record.Suite != "api-extended" || record.Stack != "multiCompose" || record.Provider != "cloak" {
			t.Fatalf("record %q lost its run conditions (%+v); records extracted from the file would silently mix stacks and providers across runs", record.Test, record)
		}
	}
	if timings.ComparisonFloorMs != comparisonFloorMs {
		t.Errorf("ComparisonFloorMs = %d, want %d recorded in the file so consumers read the rule from the data", timings.ComparisonFloorMs, comparisonFloorMs)
	}
}

func TestSplitScenarioReadsTheBracketPrefix(t *testing.T) {
	for _, tc := range []struct {
		name         string
		in           string
		wantScenario string
		wantTest     string
	}{
		{"bracketed", "[actions-basic] pinchtab click", "actions-basic", "pinchtab click"},
		{"no bracket", "plain name", "", "plain name"},
		{"unclosed bracket", "[broken name", "", "[broken name"},
		{"bracket later in name", "click [not a scenario]", "", "click [not a scenario]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scenario, test := splitScenario(tc.in)
			if scenario != tc.wantScenario || test != tc.wantTest {
				t.Errorf("splitScenario(%q) = (%q, %q), want (%q, %q)", tc.in, scenario, test, tc.wantScenario, tc.wantTest)
			}
		})
	}
}

func TestStackLabelNamesTheComposeFile(t *testing.T) {
	if got := stackLabel(singleCompose); got != "singleCompose" {
		t.Errorf("stackLabel(single) = %q", got)
	}
	if got := stackLabel(multiCompose); got != "multiCompose" {
		t.Errorf("stackLabel(multi) = %q", got)
	}
	if got := stackLabel(""); got != "none" {
		t.Errorf("stackLabel(\"\") = %q, want none", got)
	}
}

func TestRenderSlowestReportsTotalsAndTheComparisonFloor(t *testing.T) {
	timings := buildSuiteTimings("api-extended", "multiCompose", "chrome", "2026-01-01T00:00:00Z", sampleResults())
	out := renderSlowest(timings, 2)

	if !strings.Contains(out, "slowest 2 tests") {
		t.Errorf("missing the slowest header:\n%s", out)
	}
	if !strings.Contains(out, "sitemap mode discovers pages") {
		t.Errorf("slowest test is not listed first:\n%s", out)
	}
	if !strings.Contains(out, "per-scenario totals") {
		t.Errorf("per-scenario totals missing; gating is meant to happen on aggregates:\n%s", out)
	}
	if !strings.Contains(out, "under 100ms") {
		t.Errorf("the report does not state the comparison floor, so a reader will compute ratios on noise:\n%s", out)
	}
	if !strings.Contains(out, "1 test\n") {
		t.Errorf("scenario counts should read '1 test', not '1 tests':\n%s", out)
	}
}

func TestRenderSlowestCapsTheListAtTheNumberOfTests(t *testing.T) {
	timings := buildSuiteTimings("api", "singleCompose", "chrome", "", sampleResults())
	out := renderSlowest(timings, 500)
	if !strings.Contains(out, "slowest 4 tests") {
		t.Errorf("asking for more tests than the run holds should report what exists:\n%s", out)
	}
}

func TestRunSlowestReadsTheCheckedInJSONWithoutRunningAnything(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tests/e2e/results"), 0o755); err != nil {
		t.Fatal(err)
	}
	timings := buildSuiteTimings("api-extended", "multiCompose", "chrome", "2026-01-01T00:00:00Z", sampleResults())
	encoded, err := json.MarshalIndent(timings, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, resultsPath("timings", "api-extended", "json")), encoded, 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runSlowest("api-extended", 3, root, &out); err != nil {
		t.Fatalf("runSlowest() error = %v", err)
	}
	if !strings.Contains(out.String(), "stack: multiCompose") {
		t.Errorf("run conditions missing from the report:\n%s", out.String())
	}
}

func TestRunSlowestSaysWhichSuiteHasNoTimingsYet(t *testing.T) {
	err := runSlowest("api-extended", 3, t.TempDir(), &bytes.Buffer{})
	if err == nil {
		t.Fatal("runSlowest() on a suite that has never run reported success")
	}
	if !strings.Contains(err.Error(), "api-extended") {
		t.Errorf("error does not name the suite: %v", err)
	}
}

func TestSuiteTimingsPathSitsBesideTheMarkdownReport(t *testing.T) {
	def := suiteDescriptor{Name: "api-extended", Compose: multiCompose}.build()
	if def.Timings != "tests/e2e/results/timings-api-extended.json" {
		t.Errorf("Timings = %q", def.Timings)
	}
}

func TestParseArgsValidatesSlowest(t *testing.T) {
	for _, tc := range []struct {
		name    string
		argv    []string
		want    int
		wantErr bool
	}{
		{"positive", []string{"--slowest", "10"}, 10, false},
		{"equals form", []string{"--slowest=5"}, 5, false},
		{"zero", []string{"--slowest", "0"}, 0, true},
		{"negative", []string{"--slowest", "-3"}, 0, true},
		{"not a number", []string{"--slowest", "many"}, 0, true},
		{"absent", []string{"--suite", "api"}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, err := ParseArgs(tc.argv)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseArgs(%v) error = nil, want an error", tc.argv)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseArgs(%v) error = %v", tc.argv, err)
			}
			if args.Slowest != tc.want {
				t.Errorf("Slowest = %d, want %d", args.Slowest, tc.want)
			}
		})
	}
}

func TestParseDockerStatsLine(t *testing.T) {
	for _, tc := range []struct {
		name     string
		line     string
		wantOK   bool
		wantID   string
		wantName string
		wantMiB  float64
		wantPids int
	}{
		{"chrome pinchtab", "a1b2c3,e2e-pinchtab-1,484MiB / 7.667GiB,12", true, "a1b2c3", "e2e-pinchtab-1", 484, 12},
		{"gib used", "d4e5f6,e2e-pinchtab-secure-1,1.5GiB / 7.667GiB,30", true, "d4e5f6", "e2e-pinchtab-secure-1", 1536, 30},
		{"non-numeric pids keeps the memory sample", "a1b2c3,e2e-pinchtab-1,200MiB / 8GiB,--", true, "a1b2c3", "e2e-pinchtab-1", 200, 0},
		{"too few fields", "e2e-pinchtab-1,200MiB / 8GiB,3", false, "", "", 0, 0},
		{"unparseable memory", "a1b2c3,e2e-pinchtab-1,notmem,3", false, "", "", 0, 0},
		{"blank", "", false, "", "", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseDockerStatsLine(tc.line)
			if ok != tc.wantOK {
				t.Fatalf("parseDockerStatsLine(%q) ok = %v, want %v", tc.line, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if got.ID != tc.wantID || got.Name != tc.wantName || got.MiB != tc.wantMiB || got.Pids != tc.wantPids {
				t.Errorf("parseDockerStatsLine(%q) = %+v, want id %q name %q mib %v pids %d", tc.line, got, tc.wantID, tc.wantName, tc.wantMiB, tc.wantPids)
			}
		})
	}
}

func TestSelectStackSamplesRecordsOnlyThisStacksContainers(t *testing.T) {
	stackIDs := map[string]bool{"a1b2c3d4e5f6": true}
	statsOutput := strings.Join([]string{
		"a1b2c3d4e5f6,e2e-pinchtab-1,484MiB / 8GiB,12",
		"9988776655ff,pinchtab-stealth-chrome,900MiB / 8GiB,40",
		"1122334455aa,e2e-fixtures-1,10MiB / 8GiB,3",
	}, "\n")

	got := selectStackSamples(statsOutput, stackIDs)
	if len(got) != 1 || got[0].Name != "e2e-pinchtab-1" {
		t.Fatalf("selectStackSamples = %+v; a foreign pinchtab container or the stack's fixtures must not be recorded", got)
	}
}

func TestContainerIDMatchesToleratesShortAndFullIDs(t *testing.T) {
	stackIDs := map[string]bool{"a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2": true}
	if !containerIDMatches("a1b2c3d4e5f6", stackIDs) {
		t.Error("a short docker stats id must match the full compose ps id it prefixes")
	}
	if containerIDMatches("ffffffffffff", stackIDs) {
		t.Error("an unrelated id must not match")
	}
}

func TestParseMemUsageMiB(t *testing.T) {
	for _, tc := range []struct {
		field  string
		want   float64
		wantOK bool
	}{
		{"484MiB / 7.667GiB", 484, true},
		{"1GiB / 8GiB", 1024, true},
		{"512KiB / 8GiB", 0.5, true},
		{"0B / 8GiB", 0, true},
		{"1TiB", 1024 * 1024, true},
		{"nonsense", 0, false},
	} {
		t.Run(tc.field, func(t *testing.T) {
			got, ok := parseMemUsageMiB(tc.field)
			if ok != tc.wantOK || (ok && got != tc.want) {
				t.Errorf("parseMemUsageMiB(%q) = (%v, %v), want (%v, %v)", tc.field, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestMemoryAccumulatorTracksPeakFinalAndPeakPids(t *testing.T) {
	acc := newMemoryAccumulator()
	for _, s := range []memorySample{
		{Name: "pinchtab-pinchtab-1", MiB: 300, Pids: 4},
		{Name: "pinchtab-pinchtab-secure-1", MiB: 120, Pids: 2},
		{Name: "pinchtab-pinchtab-1", MiB: 670, Pids: 12},
		{Name: "pinchtab-pinchtab-1", MiB: 450, Pids: 9},
	} {
		acc.add(s)
	}

	got := acc.reduce()
	want := []containerMemory{
		{Container: "pinchtab-pinchtab-1", PeakMiB: 670, FinalMiB: 450, PeakPids: 12},
		{Container: "pinchtab-pinchtab-secure-1", PeakMiB: 120, FinalMiB: 120, PeakPids: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reduce() = %+v, want %+v", got, want)
	}
}

func TestRenderSlowestPrintsContainerMemoryAfterTheScenarioTotals(t *testing.T) {
	timings := buildSuiteTimings("api", "singleCompose", "chrome", "2026-01-01T00:00:00Z", sampleResults())
	timings.Memory = &suiteMemory{
		Provider: "chrome",
		Containers: []containerMemory{
			{Container: "pinchtab-pinchtab-1", PeakMiB: 670, FinalMiB: 450, PeakPids: 12},
		},
	}
	out := renderSlowest(timings, 2)

	totalsIdx := strings.Index(out, "per-scenario totals")
	memIdx := strings.Index(out, "container memory (browser: chrome)")
	if memIdx < 0 {
		t.Fatalf("memory section missing:\n%s", out)
	}
	if memIdx < totalsIdx {
		t.Errorf("memory section must come after the per-scenario totals:\n%s", out)
	}
	if !strings.Contains(out, "pinchtab-pinchtab-1") || !strings.Contains(out, "peak pids  12") {
		t.Errorf("memory line missing peak/pid figures:\n%s", out)
	}

	timings.Memory = nil
	if strings.Contains(renderSlowest(timings, 2), "container memory") {
		t.Error("a run with no sampled memory must not print an empty memory section")
	}
}

func TestSuiteTimingsOmitsMemoryWhenNoneSampled(t *testing.T) {
	timings := buildSuiteTimings("api", "singleCompose", "chrome", "", sampleResults())
	encoded, err := json.Marshal(timings)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "\"memory\"") {
		t.Errorf("timings JSON carries a memory key with nothing sampled:\n%s", encoded)
	}
}

func TestIsPinchtabBrowserContainerExcludesFixturesAndRunners(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"pinchtab-pinchtab-1", true},
		{"pinchtab-pinchtab-secure-1", true},
		{"pinchtab-fixtures-1", false},
		{"pinchtab-runner-api-run-abc", false},
		{"some-other-service-1", false},
	} {
		if got := isPinchtabBrowserContainer(tc.name); got != tc.want {
			t.Errorf("isPinchtabBrowserContainer(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestPrepareSuiteResultsClearsStaleTimings(t *testing.T) {
	root := t.TempDir()
	def := suiteDescriptor{Name: "api-extended", Compose: multiCompose}.build()
	if err := os.MkdirAll(filepath.Join(root, "tests/e2e/results"), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(root, def.Timings)
	if err := os.WriteFile(stale, []byte(`{"tests":1}`), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &Runner{repoRoot: root}
	r.prepareSuiteResults(def)

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale timings survived the run preparation (stat err = %v); a run that dies before writing reports would leave the previous run's numbers looking current", err)
	}
}
