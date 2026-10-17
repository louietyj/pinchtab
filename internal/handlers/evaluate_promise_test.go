package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/config"
)

// subtypeMockBridge implements the optional subtypeEvaluator capability so the
// handler takes its Promise-detecting path. The value/subtype are canned per
// test; the embedded mockBridge supplies the rest of the BridgeAPI surface
// (tab context, browser readiness) HandleEvaluate needs.
type subtypeMockBridge struct {
	*mockBridge
	subtype string
	value   any
}

func (m *subtypeMockBridge) EvaluateSubtype(_ context.Context, _ string, result any, _ bridge.EvalOpts) (string, error) {
	if ptr, ok := result.(*any); ok {
		*ptr = m.value
	}
	return m.subtype, nil
}

func evalResponse(t *testing.T, br bridge.BridgeAPI, body string) map[string]any {
	t.Helper()
	h := New(br, &config.RuntimeConfig{AllowEvaluate: true, ActionTimeout: time.Second}, nil, nil, nil)
	req := httptest.NewRequest("POST", "/evaluate", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	h.HandleEvaluate(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return payload
}

// A Promise returned without --await-promise still answers 200, but now carries a
// hint naming the flag instead of silently handing back {}.
func TestHandleEvaluateHintsAwaitPromiseForPromiseResult(t *testing.T) {
	br := &subtypeMockBridge{mockBridge: &mockBridge{}, subtype: "promise", value: map[string]any{}}
	payload := evalResponse(t, br, `{"expression":"Promise.resolve(42)"}`)

	hint, ok := payload["hint"].(string)
	if !ok || hint == "" {
		t.Fatalf("no hint for an un-awaited Promise result: %#v", payload)
	}
	if !bytes.Contains([]byte(hint), []byte("await-promise")) && !bytes.Contains([]byte(hint), []byte("awaitPromise")) {
		t.Errorf("hint = %q, want it to name --await-promise / awaitPromise:true", hint)
	}
}

// A genuine empty (or non-empty) object result must not trip the hint — the whole
// point is telling a Promise apart from an ordinary object.
func TestHandleEvaluateNoHintForPlainObject(t *testing.T) {
	br := &subtypeMockBridge{mockBridge: &mockBridge{}, subtype: "", value: map[string]any{"a": float64(1)}}
	payload := evalResponse(t, br, `{"expression":"({a:1})"}`)

	if _, present := payload["hint"]; present {
		t.Errorf("a plain object result carried a spurious hint: %#v", payload)
	}
	result, _ := payload["result"].(map[string]any)
	if result["a"] != float64(1) {
		t.Errorf("result = %#v, want the object value passed through", payload["result"])
	}
}

// With --await-promise the value resolves and no hint fires, even for a Promise
// subtype — the flag stays authoritative and the response is unchanged.
func TestHandleEvaluateAwaitPromiseResolvesWithoutHint(t *testing.T) {
	br := &subtypeMockBridge{mockBridge: &mockBridge{}, subtype: "promise", value: "resolved"}
	payload := evalResponse(t, br, `{"expression":"Promise.resolve(42)","awaitPromise":true}`)

	if _, present := payload["hint"]; present {
		t.Errorf("awaitPromise:true must not carry the hint: %#v", payload)
	}
	if payload["result"] != "resolved" {
		t.Errorf("result = %#v, want the resolved value", payload["result"])
	}
}
