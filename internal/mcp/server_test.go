package mcp

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestNewServer(t *testing.T) {
	s := NewServer("http://localhost:9867", "tok")
	if s == nil {
		t.Fatal("NewServer returned nil")
	}
}

func TestNewServerRegistersAllTools(t *testing.T) {
	_ = NewServer("http://localhost:9867", "")
	tools := allTools()
	if len(tools) != 47 {
		t.Errorf("expected 47 tools, got %d — if a tool was added or removed, update this count and the totals in docs/mcp.md", len(tools))
	}
}

func TestAllToolsHaveUniqueNames(t *testing.T) {
	tools := allTools()
	seen := make(map[string]bool, len(tools))
	for _, tool := range tools {
		if seen[tool.Name] {
			t.Errorf("duplicate tool name: %s", tool.Name)
		}
		seen[tool.Name] = true
	}
}

func TestAllToolsHaveHandlers(t *testing.T) {
	tools := allTools()
	handlers := handlerMap(NewClient("http://localhost:9867", ""))
	for _, tool := range tools {
		if _, ok := handlers[tool.Name]; !ok {
			t.Errorf("tool %q has no handler", tool.Name)
		}
	}
}

func TestVersionDefault(t *testing.T) {
	if Version != "dev" {
		t.Errorf("default Version = %q, want 'dev'", Version)
	}
}

func handleWire(t *testing.T, handle func(context.Context, json.RawMessage) mcp.JSONRPCMessage, request string, into any) {
	t.Helper()
	raw, err := json.Marshal(handle(context.Background(), json.RawMessage(request)))
	if err != nil {
		t.Fatalf("marshal response to %s: %v", request, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("decode response %s: %v", raw, err)
	}
}

func initializeRequest(version string) string {
	return `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` + version + `","capabilities":{},"clientInfo":{"name":"qa","version":"1"}}}`
}

func TestInitializeHandsEveryHandshakeClientTheVersionItAskedFor(t *testing.T) {
	for _, asked := range []string{"2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25"} {
		var got struct {
			Result struct {
				ProtocolVersion string
				Capabilities    struct{ Tools *json.RawMessage }
				ServerInfo      struct{ Name string }
			}
			Error json.RawMessage
		}
		handleWire(t, NewServer("http://localhost:9867", "").HandleMessage, initializeRequest(asked), &got)
		if len(got.Error) > 0 || got.Result.ProtocolVersion != asked {
			t.Errorf("initialize %s negotiated %q (error %s); a client on that revision would be refused or downgraded", asked, got.Result.ProtocolVersion, got.Error)
		}
		if got.Result.Capabilities.Tools == nil || got.Result.ServerInfo.Name != "PinchTab" {
			t.Errorf("initialize %s = %+v, want tools capability from PinchTab", asked, got.Result)
		}
	}

	var unknown struct {
		Result struct{ ProtocolVersion string }
	}
	handleWire(t, NewServer("http://localhost:9867", "").HandleMessage, initializeRequest("1999-01-01"), &unknown)
	if !slices.Contains(mcp.ValidProtocolVersions, unknown.Result.ProtocolVersion) {
		t.Errorf("initialize with an unknown version negotiated %q, want a version the SDK serves", unknown.Result.ProtocolVersion)
	}
}

func TestToolsListOnTheWireIsExactlyTheRegisteredTools(t *testing.T) {
	s := NewServer("http://localhost:9867", "")
	var ignored any
	handleWire(t, s.HandleMessage, initializeRequest("2025-06-18"), &ignored)
	var listed struct {
		Result struct{ Tools []struct{ Name string } }
	}
	handleWire(t, s.HandleMessage, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, &listed)

	var got, want []string
	for _, tool := range listed.Result.Tools {
		got = append(got, tool.Name)
	}
	for _, tool := range allTools() {
		want = append(want, tool.Name)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("tools/list = %v, want the registered tools %v", got, want)
	}
}
