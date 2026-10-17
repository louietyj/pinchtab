package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/pinchtab/pinchtab/internal/httpx"
	"github.com/pinchtab/pinchtab/internal/routes"
)

func (h *Handlers) HandleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, 200, h.openAPIDocument(""))
}

// ServeOpenAPI writes the spec with an optional info.description. A proxy front
// door serves the catalogue-derived instance surface it forwards to, so it states
// the scope of what the document does and does not enumerate.
func (h *Handlers) ServeOpenAPI(w http.ResponseWriter, description string) {
	httpx.JSON(w, 200, h.openAPIDocument(description))
}

func (h *Handlers) openAPIDocument(description string) map[string]any {
	security := h.endpointSecurityStates()

	paths := map[string]map[string]any{}
	addOp := func(path, method string, op map[string]any) {
		m := paths[path]
		if m == nil {
			m = map[string]any{}
			paths[path] = m
		}
		m[strings.ToLower(method)] = op
	}

	operationFor := func(ep routes.Endpoint) map[string]any {
		op := map[string]any{"summary": ep.Summary}
		if ep.Capability != routes.CapNone {
			if st, ok := security[string(ep.Capability)]; ok {
				op["description"] = st.Message
				op["x-pinchtab-enabled"] = st.Enabled
			}
		}
		return op
	}

	// Baseline: every catalog route. Root entry unless the endpoint is registered
	// only in its /tabs/{id}/... form, plus the tab-scoped variant where applicable.
	for _, ep := range routes.Core() {
		if !tabOnlyRoutes[ep.Route()] {
			addOp(ep.Path, ep.Method, operationFor(ep))
		}
		if ep.TabScoped {
			addOp("/tabs/{id}"+ep.Path, ep.Method, operationFor(ep))
		}
	}

	// Non-catalog meta/docs/alias routes (registered outside the catalog loop).
	// Management routes (/ensure-*, /shutdown, /openapi.json) stay undocumented.
	addOp("/health", "GET", map[string]any{"summary": "Health"})
	addOp("/browser/restart", "POST", map[string]any{"summary": "Soft restart the browser process without restarting the bridge"})
	addOp("/tabs", "GET", map[string]any{"summary": "List tabs"})
	addOp("/help", "GET", map[string]any{"summary": "Alias for /openapi.json"})
	addOp("/navigate", "GET", map[string]any{"summary": "Navigate (query params)"})
	addOp("/action", "GET", map[string]any{"summary": "Single action (query params)"})

	// Per-operation extras layered onto the generated ops.
	evaluateRequestBody := map[string]any{
		"required": true,
		"content": map[string]any{
			"application/json": map[string]any{
				"schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"tabId": map[string]any{
							"type":        "string",
							"description": "Optional tab ID for top-level /evaluate requests",
						},
						"expression": map[string]any{
							"type":        "string",
							"description": "JavaScript expression to evaluate",
						},
						"awaitPromise": map[string]any{
							"type":        "boolean",
							"description": "Wait for a returned promise to resolve before returning the result",
						},
					},
					"required": []string{"expression"},
				},
			},
		},
	}
	for _, p := range []string{"/evaluate", "/tabs/{id}/evaluate"} {
		if op, ok := paths[p]["post"].(map[string]any); ok {
			op["requestBody"] = evaluateRequestBody
		}
	}
	extractRequestBody := map[string]any{
		"required": true,
		"content": map[string]any{
			"application/json": map[string]any{
				"schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"tabId":     map[string]any{"type": "string", "description": "Optional tab ID for top-level /extract requests"},
						"schema":    map[string]any{"type": "object", "description": "JSON schema: an object with string/number/integer/boolean properties or arrays of such objects; x-pinchtab-hint pins a field, x-pinchtab-scope pins an array's container"},
						"scope":     map[string]any{"type": "string", "description": "Confine every field to the subtree of one element: a ref, role:, text: or plain query; CSS and XPath are refused"},
						"threshold": map[string]any{"type": "number", "description": "Minimum match score per field (default 0.3)"},
						"maxItems":  map[string]any{"type": "integer", "description": "Cap on array items (default 100)"},
					},
					"required": []string{"schema"},
				},
			},
		},
	}
	extractResponses := map[string]any{
		strconv.Itoa(http.StatusOK): map[string]any{
			"description": "Typed data with per-field diagnostics; X-PinchTab-Tab-Id names the resolved tab and X-PinchTab-Vocab carries the vocabularyToken",
			"content": map[string]any{
				"application/json": map[string]any{
					"schema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"data":            map[string]any{"type": "object", "description": "Values coerced to the schema types; arrays hold one object per repeated group"},
							"fields":          map[string]any{"type": "object", "description": "Per property: ref, score, confidence, source, reason, and for arrays items plus truncated"},
							"missing":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"truncated":       map[string]any{"type": "boolean"},
							"latency_ms":      map[string]any{"type": "integer"},
							"element_count":   map[string]any{"type": "integer"},
							"vocabularyToken": map[string]any{"type": "string", "description": "Ref vocabulary the returned refs belong to; follow-up ref actions are accepted against it"},
							"idpiWarning":     map[string]any{"type": "string"},
						},
					},
				},
			},
		},
		strconv.Itoa(http.StatusBadRequest): map[string]any{"description": "Missing or unsupported schema; the message names the offending path"},
		strconv.Itoa(http.StatusForbidden):  map[string]any{"description": "IDPI strict mode blocked injected content on the page or in the extracted values"},
		strconv.Itoa(http.StatusNotFound):   map[string]any{"description": "Tab not found"},
		strconv.Itoa(http.StatusConflict):   map[string]any{"description": "A JavaScript dialog is blocking the tab"},
	}
	for _, p := range []string{"/extract", "/tabs/{id}/extract"} {
		if op, ok := paths[p]["post"].(map[string]any); ok {
			op["requestBody"] = extractRequestBody
			op["responses"] = extractResponses
		}
	}
	if op, ok := paths["/text"]["get"].(map[string]any); ok {
		op["parameters"] = []map[string]any{
			{"name": "maxChars", "in": "query", "schema": map[string]string{"type": "integer"}},
			{"name": "format", "in": "query", "schema": map[string]string{"type": "string"}},
			{"name": "mode", "in": "query", "schema": map[string]string{"type": "string"}},
			{"name": "frameId", "in": "query", "schema": map[string]string{"type": "string"}},
		}
	}
	for _, p := range []string{"/memory", "/tabs/{id}/memory"} {
		if op, ok := paths[p]["get"].(map[string]any); ok {
			op["parameters"] = []map[string]any{
				{"name": "gc", "in": "query", "description": "Run HeapProfiler.collectGarbage before reading", "schema": map[string]string{"type": "boolean"}},
			}
		}
	}
	if op, ok := paths["/memory/snapshot/{snapshotId}/summary"]["get"].(map[string]any); ok {
		op["parameters"] = []map[string]any{
			{"name": "snapshotId", "in": "path", "required": true, "description": "The id POST /memory/snapshot returned", "schema": map[string]string{"type": "string"}},
			{"name": "top", "in": "query", "description": "Rows per table (default 20, max 200)", "schema": map[string]string{"type": "integer"}},
		}
	}
	if op, ok := paths["/memory/compare"]["get"].(map[string]any); ok {
		op["parameters"] = []map[string]any{
			{"name": "base", "in": "query", "required": true, "description": "Id of the earlier heap snapshot", "schema": map[string]string{"type": "string"}},
			{"name": "head", "in": "query", "required": true, "description": "Id of the later heap snapshot", "schema": map[string]string{"type": "string"}},
			{"name": "top", "in": "query", "description": "Constructor and duplicate-string rows (default 20, max 200)", "schema": map[string]string{"type": "integer"}},
			{"name": "retained", "in": "query", "description": "Add retained sizes from a dominator tree of the head snapshot; costs memory and time proportional to its edges", "schema": map[string]string{"type": "boolean"}},
		}
	}

	info := map[string]any{
		"title":   "Pinchtab API",
		"version": "0.7.x-local",
	}
	if description != "" {
		info["description"] = description
	}
	return map[string]any{
		"openapi":             "3.0.0",
		"info":                info,
		"x-pinchtab-security": security,
		"paths":               paths,
	}
}
