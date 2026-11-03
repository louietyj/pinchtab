package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/chromedp/chromedp"
	"github.com/pinchtab/pinchtab/internal/cdptk"
)

// ErrSelectorAmbiguous marks a text selector whose best matches are more than one
// element, so none of them is the obvious target.
var ErrSelectorAmbiguous = errors.New("selector is ambiguous")

// resolveTextAt resolves text: — bare or under first:/last:/nth: — from the
// accessibility tree.
//
// text: is used to name the thing to act on ("click text:Next"). The browser has
// already worked out which elements are controls (their role), what each is
// called (its accessible name, from aria-label, a <label>, a placeholder or its
// rendered text), and which text is rendered at all (script source, display:none
// and aria-hidden content are not in the tree, or are marked hidden).
//
// Candidates are the controls whose name matches; only when no control does,
// the rendered text runs that match, each standing for the element holding it.
// Within a tier the best rank wins: exact, then starting with the query, then
// containing it. A bare selector needs exactly one candidate, as Playwright's
// strict locators do; more is refused rather than resolved to the first. A
// positional wrapper indexes the same candidates in tree order, so
// nth:1:text:Edit reaches the second of the controls a bare text:Edit refused.
//
// frameID scopes to one frame ("" is the top document); scopeBackendNodeID, when
// non-zero, to one subtree (an open dialog).
func resolveTextAt(ctx context.Context, frameID string, scopeBackendNodeID int64, query string, index int, fromEnd, positional bool) (int64, error) {
	candidates, near, err := textCandidates(ctx, frameID, scopeBackendNodeID, query)
	if err != nil {
		return 0, err
	}
	switch {
	case len(candidates) == 0:
		return 0, fmt.Errorf("no control is named %q and no visible text contains it%s: %w", query, didYouMean(near), ErrSelectorNoMatch)
	case !positional && len(candidates) == 1:
		return candidates[0].NodeID, nil
	case !positional:
		return 0, ambiguousCandidates(query, candidates)
	}
	if fromEnd {
		index = len(candidates) - 1
	}
	if index < 0 || index >= len(candidates) {
		return 0, fmt.Errorf("%d elements match %q, no index %d: %w", len(candidates), query, index, ErrSelectorNoMatch)
	}
	return candidates[index].NodeID, nil
}

// CountText is how many elements text:query has to choose among: the candidates
// a bare selector refuses when there is more than one, and the set first:, last:
// and nth: index.
func CountText(ctx context.Context, frameID, query string) (int, error) {
	candidates, _, err := textCandidates(ctx, frameID, 0, query)
	return len(candidates), err
}

// textLookupDeadline bounds a text: lookup so a slow read of a huge page cannot
// eat the whole action timeout.
const textLookupDeadline = 3 * time.Second

// textCandidates returns the best-ranked matches of the first tier that has any:
// controls by name, then rendered text runs as their elements, then a phrase
// that crosses elements as the one control holding it. With no match at all it
// returns near misses instead, for the error to name.
func textCandidates(ctx context.Context, frameID string, scopeBackendNodeID int64, query string) (matches, near []A11yNode, err error) {
	want := normalizeAccessibleName(query)
	if want == "" {
		return nil, nil, nil
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, textLookupDeadline)
	defer cancel()
	raw, err := FetchAXTree(ctx)
	if err != nil {
		if ctx.Err() != nil && parent.Err() == nil {
			return nil, nil, fmt.Errorf("text %q lookup timed out after %s (page may be large or unresponsive): %w", query, textLookupDeadline, err)
		}
		return nil, nil, fmt.Errorf("read accessibility tree: %w", err)
	}
	if scopeBackendNodeID != 0 {
		scoped := FilterSubtree(raw, scopeBackendNodeID)
		if len(scoped) == len(raw) {
			// FilterSubtree returns everything when the scope is not in the tree;
			// answering from the whole page would act outside the dialog.
			return nil, nil, fmt.Errorf("dialog is not in the accessibility tree: %w", ErrSelectorNoMatch)
		}
		raw = scoped
	}
	// No frame chosen means every frame, as snap shows them: a page that is one
	// same-origin iframe (Cigna's Find Care) lists its buttons in snap, and text:
	// has to reach them. The same name in two frames is ambiguous, never a quiet
	// pick of the top one. A dialog scope keeps to the dialog's own frame.
	if frameID == "" && scopeBackendNodeID != 0 {
		if tree, err := FetchFrameTree(ctx); err == nil {
			frameID = tree.Frame.ID
		}
	}
	nodes, _ := BuildSnapshot(raw, "", -1)
	usable := func(n A11yNode) bool {
		return n.NodeID != 0 && !n.Hidden && (frameID == "" || n.FrameID == "" || n.FrameID == frameID)
	}

	controls := bestNamed(nodes, want, func(n A11yNode) bool {
		return usable(n) && InteractiveRoles[n.Role] && !strings.EqualFold(n.Role, "iframe")
	})
	if len(controls) > 0 {
		return controls, nil, nil
	}

	runs := bestNamed(nodes, want, func(n A11yNode) bool { return usable(n) && n.Role == "StaticText" })
	elements := make([]A11yNode, 0, len(runs))
	seen := make(map[int64]bool, len(runs))
	for _, run := range runs {
		elementID, err := elementHolding(ctx, run.NodeID)
		if err != nil || seen[elementID] {
			continue
		}
		seen[elementID] = true
		run.NodeID = elementID
		elements = append(elements, run)
	}
	if len(elements) > 0 {
		return elements, nil, nil
	}

	// Frames and dialogs keep to the two tiers above: the phrase search reads
	// the top document's rendered text.
	if frameID == "" && scopeBackendNodeID == 0 {
		if spanning, err := controlHoldingPhrase(ctx, want, query); err != nil {
			return nil, nil, err
		} else if spanning != 0 {
			return []A11yNode{{NodeID: spanning, Name: query}}, nil, nil
		}
	}
	return nil, nearMisses(nodes, want, usable), nil
}

// controlHoldingPhrase handles a phrase that crosses elements, like "In stock
// in IKEA East Palo Alto, CA", where "In stock in" is text and the store name
// a link: no single element's own text holds it. It takes the smallest element
// whose rendered text does, and its one visible control, or the element itself
// when it is a control. Several controls in there is ambiguous, not a guess.
func controlHoldingPhrase(ctx context.Context, want, query string) (int64, error) {
	const find = `function (want) {
  var norm = function (s) { return (s || '').toLowerCase().replace(/\s+/g, ' ').trim(); };
  var el = document.body;
  if (!el || norm(el.innerText).indexOf(want) < 0) return '';
  descend: for (;;) {
    for (var i = 0; i < el.children.length; i++) {
      if (norm(el.children[i].innerText).indexOf(want) >= 0) { el = el.children[i]; continue descend; }
    }
    break;
  }
  var control = 'a[href], button, input, select, textarea, summary, [role=button], [role=link], [role=checkbox], [role=tab], [role=menuitem], [role=option]';
  if (el.matches(control)) return el;
  var found = Array.prototype.filter.call(el.querySelectorAll(control), function (c) {
    var r = c.getBoundingClientRect();
    return r.width > 0 && r.height > 0;
  });
  if (found.length === 1) return found[0];
  if (found.length === 0) return '';
  return found.length + ' controls: ' + found.slice(0, 3).map(function (c) { return JSON.stringify(norm(c.innerText || c.getAttribute('aria-label') || c.value)); }).join(', ');
}`
	execID, err := cdptk.IsolatedContextID(ctx, "")
	if err != nil {
		return 0, nil
	}
	var raw json.RawMessage
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return chromedp.FromContext(ctx).Target.Execute(ctx, "Runtime.callFunctionOn", map[string]any{
			"functionDeclaration": find,
			"executionContextId":  execID,
			"arguments":           []map[string]any{{"value": want}},
		}, &raw)
	})); err != nil {
		return 0, nil
	}
	var call struct {
		Result struct {
			ObjectID string `json:"objectId"`
			Value    any    `json:"value"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &call) != nil {
		return 0, nil
	}
	if call.Result.ObjectID != "" {
		return backendNodeIDFromObjectID(ctx, call.Result.ObjectID)
	}
	if many, _ := call.Result.Value.(string); many != "" {
		return 0, fmt.Errorf("%w: the text holding %q has %s; use a ref from snap or click one of them by its own text", ErrSelectorAmbiguous, query, many)
	}
	return 0, nil
}

// nearMisses are the visible controls and text whose names match the query
// once spaces and punctuation are ignored, or within a small edit distance:
// "+3 more" for "+ 3 more", whose space a downscaled screenshot loses. They
// are only ever suggested, never acted on.
func nearMisses(nodes []A11yNode, want string, usable func(A11yNode) bool) []A11yNode {
	compact := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return r
			}
			return -1
		}, strings.ToLower(s))
	}
	target := compact(want)
	if target == "" {
		return nil
	}
	var out []A11yNode
	seen := map[string]bool{}
	for _, n := range nodes {
		if !usable(n) || (n.Role != "StaticText" && !InteractiveRoles[n.Role]) {
			continue
		}
		// A control and the text run inside it share a name; one is enough.
		name := compact(n.Name)
		if name == "" || seen[n.Name] {
			continue
		}
		if strings.Contains(name, target) || editDistance(name, target) <= max(1, len([]rune(target))/5) {
			seen[n.Name] = true
			out = append(out, n)
			if len(out) == 3 {
				break
			}
		}
	}
	return out
}

func didYouMean(near []A11yNode) string {
	if len(near) == 0 {
		return ""
	}
	named := make([]string, 0, len(near))
	for _, n := range near {
		role := n.Role
		if role == "StaticText" {
			role = "text"
		}
		named = append(named, fmt.Sprintf("%s %q", role, n.Name))
	}
	return "; did you mean " + strings.Join(named, ", ") + "?"
}

func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

// bestNamed returns the nodes keep accepts whose name matches want at the best
// rank any of them reaches, in tree order.
func bestNamed(nodes []A11yNode, want string, keep func(A11yNode) bool) []A11yNode {
	var best []A11yNode
	bestRank := 0
	seen := make(map[int64]bool)
	for _, n := range nodes {
		if seen[n.NodeID] || !keep(n) {
			continue
		}
		rank := nameMatchRank(normalizeAccessibleName(n.Name), want)
		if rank == 0 || rank < bestRank {
			continue
		}
		seen[n.NodeID] = true
		if rank > bestRank {
			best, bestRank = nil, rank
		}
		best = append(best, n)
	}
	return best
}

// elementHolding returns the element a text run sits in: actions need a box and
// a click point, which a Text node does not offer.
func elementHolding(ctx context.Context, backendNodeID int64) (int64, error) {
	objectID, err := IsolatedNodeObjectID(ctx, backendNodeID)
	if err != nil {
		return 0, err
	}
	var callResult json.RawMessage
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return chromedp.FromContext(ctx).Target.Execute(ctx, "Runtime.callFunctionOn", map[string]any{
			"functionDeclaration": `function() { return this.nodeType === 1 ? this : this.parentElement; }`,
			"objectId":            objectID,
		}, &callResult)
	})); err != nil {
		return 0, err
	}
	var call struct {
		Result struct {
			ObjectID string `json:"objectId"`
		} `json:"result"`
	}
	if err := json.Unmarshal(callResult, &call); err != nil {
		return 0, err
	}
	if call.Result.ObjectID == "" {
		return 0, fmt.Errorf("text has no element: %w", ErrSelectorNoMatch)
	}
	return backendNodeIDFromObjectID(ctx, call.Result.ObjectID)
}

func ambiguousCandidates(query string, candidates []A11yNode) error {
	named := make([]string, 0, 3)
	for _, n := range candidates[:min(len(candidates), 3)] {
		named = append(named, fmt.Sprintf("%s %q", n.Role, n.Name))
	}
	more := ""
	if len(candidates) > 3 {
		more = fmt.Sprintf(" and %d more", len(candidates)-3)
	}
	return fmt.Errorf("%w: %d elements match %q (%s%s); use a ref from snap, --within <the container>, or nth:N:text:",
		ErrSelectorAmbiguous, len(candidates), query, strings.Join(named, ", "), more)
}

const (
	nameContains = 1 + iota
	namePrefix
	nameExact
)

func nameMatchRank(name, want string) int {
	switch {
	case name == want:
		return nameExact
	case strings.HasPrefix(name, want):
		return namePrefix
	case strings.Contains(name, want):
		return nameContains
	}
	return 0
}

func normalizeAccessibleName(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}
