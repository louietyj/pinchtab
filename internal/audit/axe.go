package audit

import (
	"encoding/json"
	"fmt"
	"strings"
)

// The two a11y engines /a11y/audit exposes. native is the home-grown rule set
// over the accessibility snapshot; axe runs vendored axe-core in the page.
const (
	EngineNative = "native"
	EngineAxe    = "axe"
)

// ParseEngine maps a requested ?engine= value onto an engine, refusing an
// unknown one so a typo is a 400 rather than a silent fallback to native.
func ParseEngine(requested string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(requested)) {
	case "", EngineNative:
		return EngineNative, nil
	case EngineAxe:
		return EngineAxe, nil
	default:
		return "", fmt.Errorf("unknown a11y engine %q; accepted values are axe, native (omit engine for the default native scan)", requested)
	}
}

// DefaultAxeTags is the tag set the axe engine runs when the caller names none.
// It is the WCAG conformance tags WITHOUT best-practice, so a best-practice rule
// surfaces only when the caller asks for it (tags=best-practice), keeping the
// default result focused on standards violations.
var DefaultAxeTags = []string{"wcag2a", "wcag2aa", "wcag21a", "wcag21aa"}

// AxeTestEngine is axe-core's self-report of which engine version ran.
type AxeTestEngine struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// AxeRawResult is the shape the in-page axe run returns: the violation and
// incomplete rule lists in full, with passes and inapplicable reduced to counts
// (the page snippet returns their lengths, since their node bodies are large and
// unused by the report).
type AxeRawResult struct {
	URL          string            `json:"url"`
	TestEngine   AxeTestEngine     `json:"testEngine"`
	Violations   []AxeRawViolation `json:"violations"`
	Incomplete   []AxeRawViolation `json:"incomplete"`
	Passes       int               `json:"passes"`
	Inapplicable int               `json:"inapplicable"`
}

// AxeRawViolation is one axe rule result with its offending nodes.
type AxeRawViolation struct {
	ID      string       `json:"id"`
	Impact  string       `json:"impact"`
	Tags    []string     `json:"tags"`
	Help    string       `json:"help"`
	HelpURL string       `json:"helpUrl"`
	Nodes   []AxeRawNode `json:"nodes"`
}

// AxeRawNode is one offending element as axe reports it: a CSS target path and
// the element's outer HTML, plus the human-readable failure summary.
type AxeRawNode struct {
	Target         []any  `json:"target"`
	HTML           string `json:"html"`
	FailureSummary string `json:"failureSummary"`
}

// AxeReport is the /a11y/audit?engine=axe response. Score reuses the native
// 100-minus-weighted-violations scale so axe and native scores stay comparable
// on a dashboard.
type AxeReport struct {
	Engine               string         `json:"engine"`
	Version              string         `json:"version"`
	URL                  string         `json:"url"`
	Violations           []AxeViolation `json:"violations"`
	IncompleteViolations []AxeViolation `json:"incompleteViolations,omitempty"`
	Passes               int            `json:"passes"`
	Incomplete           int            `json:"incomplete"`
	Inapplicable         int            `json:"inapplicable"`
	Score                int            `json:"score"`
}

// AxeViolation is one rule violation in the response.
type AxeViolation struct {
	ID      string    `json:"id"`
	Impact  string    `json:"impact"`
	Tags    []string  `json:"tags"`
	Help    string    `json:"help"`
	HelpURL string    `json:"helpUrl"`
	Nodes   []AxeNode `json:"nodes"`
}

// AxeNode is one offending element. Ref is filled when the element maps to a
// snapshot ref, so an agent can act on it directly through /action.
type AxeNode struct {
	Target         []string `json:"target"`
	HTML           string   `json:"html"`
	Ref            string   `json:"ref,omitempty"`
	FailureSummary string   `json:"failureSummary,omitempty"`
}

// axeImpactWeight maps an axe impact onto the native severity weight so the axe
// score deducts on the same scale. axe "critical" folds into the heaviest native
// bucket (serious); an absent/unknown impact deducts the minor weight rather than
// zero, so a rule with no impact still moves the score.
func axeImpactWeight(impact string) int {
	switch impact {
	case "critical", SeveritySerious:
		return a11yWeights[SeveritySerious]
	case SeverityModerate:
		return a11yWeights[SeverityModerate]
	default:
		return a11yWeights[SeverityMinor]
	}
}

// BuildAxeReport shapes a raw in-page axe result into the response envelope and
// computes the comparable score. Refs are left empty here; the handler fills
// them from the snapshot after this returns. includeIncomplete adds the
// incomplete rule details alongside the always-present incomplete count.
func BuildAxeReport(raw AxeRawResult, version string, includeIncomplete bool) AxeReport {
	report := AxeReport{
		Engine:       EngineAxe,
		Version:      version,
		URL:          raw.URL,
		Violations:   convertAxeViolations(raw.Violations),
		Passes:       raw.Passes,
		Incomplete:   len(raw.Incomplete),
		Inapplicable: raw.Inapplicable,
		Score:        100,
	}
	if includeIncomplete {
		report.IncompleteViolations = convertAxeViolations(raw.Incomplete)
	}
	for _, v := range raw.Violations {
		report.Score -= axeImpactWeight(v.Impact) * len(v.Nodes)
	}
	if report.Score < 0 {
		report.Score = 0
	}
	return report
}

func convertAxeViolations(raw []AxeRawViolation) []AxeViolation {
	out := make([]AxeViolation, 0, len(raw))
	for _, v := range raw {
		nodes := make([]AxeNode, 0, len(v.Nodes))
		for _, n := range v.Nodes {
			nodes = append(nodes, AxeNode{
				Target:         flattenAxeTarget(n.Target),
				HTML:           n.HTML,
				FailureSummary: n.FailureSummary,
			})
		}
		out = append(out, AxeViolation{
			ID:      v.ID,
			Impact:  v.Impact,
			Tags:    v.Tags,
			Help:    v.Help,
			HelpURL: v.HelpURL,
			Nodes:   nodes,
		})
	}
	return out
}

// flattenAxeTarget renders axe's target path (each hop a CSS string, or an array
// of shadow-DOM hops) into a flat list of selector strings, dropping any hop that
// is neither, so a nested/shadow target still yields the selectors it can.
func flattenAxeTarget(target []any) []string {
	out := make([]string, 0, len(target))
	for _, hop := range target {
		switch v := hop.(type) {
		case string:
			out = append(out, v)
		case []any:
			for _, inner := range v {
				if s, ok := inner.(string); ok {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

type axeRunConfig struct {
	RunOnly *axeRunOnly `json:"runOnly,omitempty"`
}

type axeRunOnly struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

// BuildAxeRunConfig renders the axe.run options JSON from the requested filters.
// A rules allowlist wins over tags (it names exact rule ids); otherwise the tags
// select the run, defaulting to DefaultAxeTags when the caller names none.
func BuildAxeRunConfig(tags, rules []string) (string, error) {
	cfg := axeRunConfig{}
	switch {
	case len(rules) > 0:
		cfg.RunOnly = &axeRunOnly{Type: "rule", Values: rules}
	case len(tags) > 0:
		cfg.RunOnly = &axeRunOnly{Type: "tag", Values: tags}
	default:
		cfg.RunOnly = &axeRunOnly{Type: "tag", Values: DefaultAxeTags}
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
