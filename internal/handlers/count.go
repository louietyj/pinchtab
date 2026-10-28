package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/httpx"
	"github.com/pinchtab/pinchtab/internal/selector"
	"github.com/pinchtab/semantic"
)

type countResponse struct {
	Selector string `json:"selector"`
	Count    int    `json:"count"`
}

// @Endpoint GET /count
func (h *Handlers) HandleCount(w http.ResponseWriter, r *http.Request) {
	tabID := r.URL.Query().Get("tabId")
	h.recordReadRequest(r, "inspect.count", tabID)

	selector := r.URL.Query().Get("selector")
	if selector == "" {
		httpx.Error(w, 400, fmt.Errorf("selector query parameter is required"))
		return
	}

	h.inspectElement(w, r, tabID, func(ctx context.Context, resolvedTabID string) (any, error) {
		count, err := h.countElements(ctx, resolvedTabID, selector)
		if err != nil {
			return nil, fmt.Errorf("count elements: %w", err)
		}
		return countResponse{Selector: selector, Count: count}, nil
	})
}

// @Endpoint GET /tabs/{id}/count
func (h *Handlers) HandleTabCount(w http.ResponseWriter, r *http.Request) {
	h.withPathTabID(w, r, h.HandleCount)
}

type frameCountEvaluator interface {
	EvaluateInFrame(ctx context.Context, frameID string, expression string, result any, opts bridge.EvalOpts) error
}

// countElements returns the number of elements matching a unified selector,
// dispatching by kind so /count covers the same selector families the other
// element handlers support (rather than feeding every prefixed string to
// querySelectorAll). All counts honor the active frame scope.
//
// Cardinality per family:
//   - css / xpath: true match count via querySelectorAll / document.evaluate.
//   - text: number of distinct leaf-most elements whose text matches, reusing
//     the same browser-side text-match logic that text: resolution uses to pick
//     a single node — so the count is consistent with what a text: action would
//     target (0 when nothing matches, otherwise the size of that leaf-most set).
//   - ref / first / last / nth: these select a single element by construction,
//     so the count is 0 (not found) or 1 (found), resolved via the single-node
//     path.
//   - semantic (role/label/testid/placeholder/alt/title/find): number of
//     accessibility-snapshot elements the matcher accepts for the query (score
//     >= threshold). Returns the same "matcher not configured" error the other
//     semantic handlers return when h.Matcher is nil.
func (h *Handlers) countElements(ctx context.Context, tabID, raw string) (int, error) {
	sel := selector.Parse(raw)
	switch sel.Kind {
	case selector.KindCSS:
		return h.countViaJS(ctx, tabID, fmt.Sprintf("document.querySelectorAll(%s).length", jsString(sel.Value)))

	case selector.KindXPath:
		// Snapshot length is the true cardinality of an XPath match set. Scope to
		// the frame's document/this so it tracks the active frame like css does.
		expr := fmt.Sprintf(
			"(function(){var r=document.evaluate(%s,document,null,XPathResult.ORDERED_NODE_SNAPSHOT_TYPE,null);return r.snapshotLength;})()",
			jsString(sel.Value),
		)
		return h.countViaJS(ctx, tabID, expr)

	case selector.KindText:
		return bridge.CountText(ctx, h.selectorFrameID(tabID), sel.Value)

	case selector.KindSemantic, selector.KindRole, selector.KindLabel,
		selector.KindPlaceholder, selector.KindAlt, selector.KindTitle, selector.KindTestID:
		return h.countSemantic(ctx, tabID, sel)

	case selector.KindRef, selector.KindFirst, selector.KindLast, selector.KindNth:
		return h.countSingleNode(ctx, tabID, sel)

	default:
		// Unknown/none: fall back to treating the raw string as CSS for backward
		// compatibility (Parse only yields these for empty input, which the handler
		// already rejects).
		return h.countViaJS(ctx, tabID, fmt.Sprintf("document.querySelectorAll(%s).length", jsString(raw)))
	}
}

// countSingleNode resolves a selector that inherently targets one element and
// returns 0 (not found) or 1 (found). A genuine "no match" is not an error here.
func (h *Handlers) countSingleNode(ctx context.Context, tabID string, sel selector.Selector) (int, error) {
	nodeID, err := h.resolveSelectorNodeID(ctx, tabID, sel.String())
	if err != nil {
		if errors.Is(err, bridge.ErrSelectorNoMatch) {
			return 0, nil
		}
		return 0, err
	}
	if nodeID == 0 {
		return 0, nil
	}
	return 1, nil
}

// countSemantic counts accessibility-snapshot elements the matcher accepts for
// a semantic query. It mirrors applySemanticActionSelectorInFrame's setup (and
// reuses its descriptor acquisition + "not configured" error) but asks the
// matcher for every element so the result is a true count rather than a single
// best ref.
func (h *Handlers) countSemantic(ctx context.Context, tabID string, sel selector.Selector) (int, error) {
	query, ok := sel.SemanticQuery()
	if !ok {
		return 0, fmt.Errorf("selector %q is not a semantic query", sel.String())
	}
	if h.Matcher == nil {
		return 0, ErrSemanticMatcherUnavailable
	}

	frameID := h.selectorFrameID(tabID)
	descs, err := h.semanticDescriptorsForTabInFrame(ctx, tabID, frameID)
	if err != nil {
		return 0, err
	}

	result, err := h.Matcher.Find(ctx, query, descs, semantic.FindOptions{
		Threshold: 0.3,
		TopK:      len(descs),
	})
	if err != nil {
		return 0, fmt.Errorf("semantic selector: %w", err)
	}
	return len(result.Matches), nil
}

// jsString JSON-encodes a value for safe inlining into a JS expression. Panics
// are impossible here because the input is always a Go string.
func jsString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

// countViaJS evaluates a count-producing JS expression, honoring the active
// frame scope. It is the shared bridge path for the css/xpath/text families.
func (h *Handlers) countViaJS(ctx context.Context, tabID, expr string) (int, error) {
	var count int
	if err := h.evaluateCount(ctx, tabID, expr, &count); err != nil {
		return 0, err
	}
	return count, nil
}

func (h *Handlers) evaluateCount(ctx context.Context, tabID, expr string, out any) error {
	frameID := h.selectorFrameID(tabID)
	if frameID == "" {
		return h.evalRuntime(ctx, expr, out, bridge.EvalOpts{})
	}
	evaluator, ok := bridgeAs[frameCountEvaluator](h.Bridge)
	if !ok {
		return fmt.Errorf("frame-scoped count unavailable")
	}
	return evaluator.EvaluateInFrame(ctx, frameID, expr, out, bridge.EvalOpts{})
}
