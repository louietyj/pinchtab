package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

const extractProductEnvelope = `{"data":{"inStock":true,"name":"Sony WH-1000XM5 Wireless Headphones","price":1299,"rating":4.7},"fields":{"name":{"ref":"e2","score":1,"confidence":"high","source":"hint"},"price":{"ref":"e3","score":0.71,"confidence":"medium","source":"text"}},"missing":[],"truncated":false,"latency_ms":2,"element_count":9,"vocabularyToken":"vocab-extract"}`

type extractRecorder struct {
	paths  []string
	bodies []map[string]any
	status int
	reply  string
}

func (x *extractRecorder) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		x.paths = append(x.paths, r.URL.Path)
		x.bodies = append(x.bodies, body)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/extract" {
			w.Header().Set(vocabHeader, "vocab-extract")
			status := x.status
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(x.reply))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"path": r.URL.Path, "body": body})
	}))
}

func productSchemaArg() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []any{"name", "price"},
		"properties": map[string]any{
			"name":  map[string]any{"type": "string", "x-pinchtab-hint": "role:heading"},
			"price": map[string]any{"type": "number", "description": "product price"},
			"items": map[string]any{"type": "array", "maxItems": 5.0, "items": map[string]any{"type": "object", "properties": map[string]any{"sku": map[string]any{"type": "string"}}}},
		},
	}
}

func TestExtractPostsTheSchemaUnchangedWithTabScopeAndMaxItems(t *testing.T) {
	rec := &extractRecorder{reply: extractProductEnvelope}
	srv := rec.server()
	defer srv.Close()

	schema := productSchemaArg()
	callTool(t, "pinchtab_extract", map[string]any{"schema": schema, "tabId": "t1", "scope": " role:main ", "maxItems": 3.0}, srv)

	if len(rec.bodies) != 1 || rec.paths[0] != "/extract" {
		t.Fatalf("requests = %v, want one POST /extract", rec.paths)
	}
	body := rec.bodies[0]
	if !reflect.DeepEqual(body["schema"], schema) {
		t.Errorf("schema sent as %#v, want the argument unchanged %#v", body["schema"], schema)
	}
	if body["tabId"] != "t1" || body["scope"] != "role:main" || body["maxItems"] != 3.0 {
		t.Errorf("tabId/scope/maxItems = %v/%v/%v, want t1/role:main/3", body["tabId"], body["scope"], body["maxItems"])
	}
}

func TestExtractSendsNoOptionalFieldTheCallerLeftOut(t *testing.T) {
	rec := &extractRecorder{reply: extractProductEnvelope}
	srv := rec.server()
	defer srv.Close()

	callTool(t, "pinchtab_extract", map[string]any{"schema": productSchemaArg()}, srv)
	for _, key := range []string{"tabId", "scope", "maxItems"} {
		if _, ok := rec.bodies[0][key]; ok {
			t.Errorf("body carries %q although the call left it out: %v", key, rec.bodies[0])
		}
	}
}

func TestExtractReturnsTheTypedProductObject(t *testing.T) {
	rec := &extractRecorder{reply: extractProductEnvelope}
	srv := rec.server()
	defer srv.Close()

	r := callTool(t, "pinchtab_extract", map[string]any{"schema": productSchemaArg()}, srv)
	if r.IsError {
		t.Fatalf("extract failed: %s", resultText(t, r))
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	text := lastText(t, r)
	if err := json.Unmarshal([]byte(text), &envelope); err != nil {
		t.Fatalf("result is not the envelope: %v\n%s", err, text)
	}
	want := map[string]any{"inStock": true, "name": "Sony WH-1000XM5 Wireless Headphones", "price": 1299.0, "rating": 4.7}
	if !reflect.DeepEqual(envelope.Data, want) {
		t.Errorf("data = %#v, want %#v", envelope.Data, want)
	}
}

func TestExtractWithoutASchemaIsRefusedBeforeCallingUpstream(t *testing.T) {
	rec := &extractRecorder{reply: extractProductEnvelope}
	srv := rec.server()
	defer srv.Close()

	r := callTool(t, "pinchtab_extract", map[string]any{"tabId": "t1"}, srv)
	if !r.IsError || !strings.Contains(resultText(t, r), "schema") {
		t.Fatalf("want an error naming schema, got %+v", r.Content)
	}
	if len(rec.paths) != 0 {
		t.Errorf("upstream was called: %v", rec.paths)
	}
}

func TestExtractSurfacesTheServersSchemaRefusal(t *testing.T) {
	rec := &extractRecorder{status: http.StatusBadRequest, reply: `{"error":"schema properties.price.type: object is not supported"}`}
	srv := rec.server()
	defer srv.Close()

	r := callTool(t, "pinchtab_extract", map[string]any{"schema": map[string]any{"type": "object", "properties": map[string]any{"price": map[string]any{"type": "object"}}}}, srv)
	if !r.IsError || !strings.Contains(resultText(t, r), "properties.price.type") {
		t.Fatalf("want the 400 naming the path, got %+v", r.Content)
	}
}

func TestExtractStoresTheVocabularyItsResponseCarriesSoTheNextClickEchoesIt(t *testing.T) {
	for _, tabID := range []string{"t1", ""} {
		t.Run("tab="+tabID, func(t *testing.T) {
			rec := &extractRecorder{reply: extractProductEnvelope}
			srv := rec.server()
			defer srv.Close()
			c := NewClient(srv.URL, "")

			args := map[string]any{"schema": productSchemaArg()}
			clickArgs := map[string]any{"ref": "e3"}
			if tabID != "" {
				args["tabId"] = tabID
				clickArgs["tabId"] = tabID
			}
			callSharedTool(t, c, "pinchtab_extract", args)
			if got := c.VocabToken(tabID); got != "vocab-extract" {
				t.Fatalf("store holds %q for tab %q after extract, want the token extract returned", got, tabID)
			}
			got, ok := actionVocabSent(t, callSharedTool(t, c, "pinchtab_click", clickArgs))
			if !ok || got != "vocab-extract" {
				t.Errorf("click after extract echoed vocab %q (sent=%v), want vocab-extract", got, ok)
			}
		})
	}
}

func lastText(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	if len(r.Content) == 0 {
		t.Fatal("no content in result")
	}
	tc, ok := r.Content[len(r.Content)-1].(mcp.TextContent)
	if !ok {
		t.Fatalf("last content is %T, not TextContent", r.Content[len(r.Content)-1])
	}
	return tc.Text
}
