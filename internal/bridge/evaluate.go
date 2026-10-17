package bridge

import (
	"context"
	"encoding/json"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// Evaluate runs expression in the tab and unmarshals the returned value into
// result. It delegates to EvaluateSubtype so a single decode body serves both;
// callers that only need the value ignore the subtype.
func (b *Bridge) Evaluate(ctx context.Context, expression string, result any, opts EvalOpts) error {
	_, err := b.EvaluateSubtype(ctx, expression, result, opts)
	return err
}

// EvaluateSubtype runs expression and reports the returned RemoteObject's subtype
// (e.g. "promise") alongside decoding its value into result. A Promise returned
// without AwaitPromise serialises to {} with no by-value payload, so the subtype
// is the only thing that tells it apart from a genuine empty object — letting the
// caller hint --await-promise instead of silently handing back {}.
//
// Detection and decode share one evaluation, so a side-effecting snippet runs
// exactly once. With AwaitPromise the resolved value is returned by value as
// before. Otherwise the expression is evaluated WITHOUT returnByValue — the only
// mode under which CDP populates subtype:"promise" (returnByValue serialises the
// Promise and drops the subtype) — and the value is read back from the same
// result: a primitive carries it inline, an object is serialised from its own
// handle via CallFunctionOn, which is byte-identical to a direct by-value return
// (a Promise serialises to {}, matching the prior output) and never re-runs the
// expression.
func (b *Bridge) EvaluateSubtype(ctx context.Context, expression string, result any, opts EvalOpts) (string, error) {
	var subtype string
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		if opts.AwaitPromise {
			obj, exc, err := runtime.Evaluate(expression).WithAwaitPromise(true).WithReturnByValue(true).Do(ctx)
			if err != nil {
				return err
			}
			if exc != nil {
				return exc
			}
			subtype = string(obj.Subtype)
			return decodeRemoteValue(obj.Value, result)
		}

		obj, exc, err := runtime.Evaluate(expression).Do(ctx)
		if err != nil {
			return err
		}
		if exc != nil {
			return exc
		}
		subtype = string(obj.Subtype)
		if result == nil {
			return nil
		}
		if len(obj.Value) > 0 || obj.ObjectID == "" {
			// A primitive (or null) carries its value inline; undefined and the
			// like have neither a value nor a handle.
			return decodeRemoteValue(obj.Value, result)
		}
		// An object (a Promise included) carries no inline value here. Serialise it
		// from its own handle so the bytes match a by-value return without running
		// the expression a second time, then release the handle.
		value, _, cferr := runtime.CallFunctionOn("function(){return this;}").
			WithObjectID(obj.ObjectID).WithReturnByValue(true).Do(ctx)
		_ = runtime.ReleaseObject(obj.ObjectID).Do(ctx)
		if cferr != nil {
			return cferr
		}
		return decodeRemoteValue(value.Value, result)
	}))
	return subtype, err
}

// decodeRemoteValue unmarshals a RemoteObject's by-value JSON into result,
// treating an absent value as JSON null (matching chromedp.Evaluate's decode).
func decodeRemoteValue(value []byte, result any) error {
	if result == nil {
		return nil
	}
	if len(value) == 0 {
		value = []byte("null")
	}
	return json.Unmarshal(value, result)
}
