package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

const memorySnapshotMCPTimeout = 3 * time.Minute

func memoryTool() mcp.Tool {
	return mcp.NewTool("pinchtab_memory",
		mcp.WithDescription("JavaScript heap usage and DOM counters (documents, nodes, listeners, frames) for the tab. gc=true collects garbage first, so reads around an action show retained memory."),
		tabIDParam(),
		mcp.WithBoolean("gc", mcp.Description("Run a garbage collection before reading")),
		browserParam(),
	)
}

func memorySnapshotTool() mcp.Tool {
	return mcp.NewTool("pinchtab_memory_snapshot",
		mcp.WithDescription("Take a V8 heap snapshot to a server-side file; returns its path and a summary (top constructors by size and count, duplicate strings). Needs security.allowMemory."),
		tabIDParam(),
		mcp.WithNumber("top", mcp.Description("Rows per table (default 20)")),
		browserParam(),
	)
}

func memoryCompareTool() mcp.Tool {
	return mcp.NewTool("pinchtab_memory_compare",
		mcp.WithDescription("Compare two pinchtab_memory_snapshot ids: per-constructor count and self-size deltas, largest first, and new duplicate strings. Snapshot, act, snapshot, compare to find a leak. Needs security.allowMemory."),
		mcp.WithString("base", mcp.Required(), mcp.Description("Earlier snapshot id")),
		mcp.WithString("head", mcp.Required(), mcp.Description("Later snapshot id")),
		mcp.WithNumber("top", mcp.Description("Rows (default 20)")),
		mcp.WithBoolean("retained", mcp.Description("Add retained sizes (slower)")),
		browserParam(),
	)
}

func handleMemoryCompare(c *Client) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		base, head := optString(r, "base"), optString(r, "head")
		if base == "" || head == "" {
			return mcp.NewToolResultError("base and head are required: pass two ids pinchtab_memory_snapshot returned"), nil
		}
		q := url.Values{}
		q.Set("base", base)
		q.Set("head", head)
		if top, ok := optInt(r, "top"); ok && top > 0 {
			q.Set("top", strconv.Itoa(top))
		}
		if retained, ok := optBool(r, "retained"); ok && retained {
			q.Set("retained", "true")
		}
		return toolResult(c.withTimeout(memorySnapshotMCPTimeout).Get(ctx, "/memory/compare", routedQuery(r, q)))
	}
}

func handleMemory(c *Client) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q := url.Values{}
		if tabID := optString(r, "tabId"); tabID != "" {
			q.Set("tabId", tabID)
		}
		if gc, ok := optBool(r, "gc"); ok && gc {
			q.Set("gc", "true")
		}
		return toolResult(c.Get(ctx, "/memory", routedQuery(r, q)))
	}
}

func handleMemorySnapshot(c *Client) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		payload := map[string]any{}
		if tabID := optString(r, "tabId"); tabID != "" {
			payload["tabId"] = tabID
		}
		body, code, err := c.withTimeout(memorySnapshotMCPTimeout).Post(ctx, routedPath(r, "/memory/snapshot"), payload)
		if err != nil || code >= 400 {
			return toolResult(body, code, err)
		}
		var snap struct {
			ID    string `json:"id"`
			Path  string `json:"path"`
			Bytes int64  `json:"bytes"`
		}
		if err := json.Unmarshal(body, &snap); err != nil || snap.ID == "" {
			return mcp.NewToolResultError(fmt.Sprintf("heap snapshot response carried no id: %s", string(body))), nil
		}

		q := url.Values{}
		if top, ok := optInt(r, "top"); ok && top > 0 {
			q.Set("top", strconv.Itoa(top))
		}
		summaryBody, code, err := c.withTimeout(memorySnapshotMCPTimeout).Get(ctx, "/memory/snapshot/"+url.PathEscape(snap.ID)+"/summary", routedQuery(r, q))
		if err != nil || code >= 400 {
			return toolResult(summaryBody, code, err)
		}
		var summary map[string]any
		if err := json.Unmarshal(summaryBody, &summary); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("decode heap snapshot summary: %v", err)), nil
		}
		delete(summary, "id")
		delete(summary, "path")
		return jsonResult(map[string]any{
			"id":      snap.ID,
			"path":    snap.Path,
			"bytes":   snap.Bytes,
			"summary": summary,
		})
	}
}
