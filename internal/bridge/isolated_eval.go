package bridge

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chromedp/chromedp"

	"github.com/pinchtab/pinchtab/internal/cdptk"
)

// EvaluateInIsolatedWorld runs expression in the isolated world of frameID (the
// top frame when frameID is empty) and unmarshals the returned value into
// result, awaiting a returned promise. It runs where page script cannot reach,
// so a page that redefines globals or DOM prototypes cannot alter the result —
// the property the axe engine depends on.
func EvaluateInIsolatedWorld(ctx context.Context, frameID, expression string, result any) error {
	return cdptk.EvaluateInIsolatedWorld(ctx, frameID, expression, result)
}

// BackendNodeIDForSelector resolves a CSS selector to a backend node id in the
// isolated world of frameID (top frame when empty). It returns 0 without error
// when the selector matches nothing, so a caller mapping many selectors treats a
// miss as "no ref" rather than a failure that aborts the whole mapping.
func BackendNodeIDForSelector(ctx context.Context, frameID, selector string) (int64, error) {
	execID, err := cdptk.IsolatedContextID(ctx, frameID)
	if err != nil {
		return 0, err
	}

	encodedSelector, err := json.Marshal(selector)
	if err != nil {
		return 0, err
	}

	var raw json.RawMessage
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return chromedp.FromContext(ctx).Target.Execute(ctx, "Runtime.evaluate", map[string]any{
			"expression": fmt.Sprintf(`document.querySelector(%s)`, encodedSelector),
			"contextId":  execID,
		}, &raw)
	})); err != nil {
		return 0, err
	}

	var parsed struct {
		Result struct {
			ObjectID string `json:"objectId"`
			Subtype  string `json:"subtype"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return 0, err
	}
	if parsed.Result.ObjectID == "" || parsed.Result.Subtype == "null" {
		return 0, nil
	}
	return backendNodeIDFromObjectID(ctx, parsed.Result.ObjectID)
}
