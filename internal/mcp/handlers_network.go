package mcp

import (
	"context"
	"fmt"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"
)

func handleNetwork(c *Client) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q := url.Values{}
		if tabID := optString(r, "tabId"); tabID != "" {
			q.Set("tabId", tabID)
		}
		if filter := optString(r, "filter"); filter != "" {
			q.Set("filter", filter)
		}
		if method := optString(r, "method"); method != "" {
			q.Set("method", method)
		}
		if status := optString(r, "status"); status != "" {
			q.Set("status", status)
		}
		if typ := optString(r, "type"); typ != "" {
			q.Set("type", typ)
		}
		if limit, ok := optFloat(r, "limit"); ok {
			q.Set("limit", fmt.Sprintf("%d", int(limit)))
		}
		if bufSize, ok := optFloat(r, "bufferSize"); ok {
			q.Set("bufferSize", fmt.Sprintf("%d", int(bufSize)))
		}
		return toolResult(c.Get(ctx, "/network", q))
	}
}

func handleNetworkDetail(c *Client) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		requestID, err := r.RequireString("requestId")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		q := url.Values{}
		if tabID := optString(r, "tabId"); tabID != "" {
			q.Set("tabId", tabID)
		}
		if v, ok := optBool(r, "body"); ok && v {
			q.Set("body", "true")
		}
		path := "/network/" + url.PathEscape(requestID)
		return toolResult(c.Get(ctx, path, q))
	}
}

func handleNetworkClear(c *Client) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q := url.Values{}
		if tabID := optString(r, "tabId"); tabID != "" {
			q.Set("tabId", tabID)
		}
		return toolResult(c.Post(ctx, "/network/clear?"+q.Encode(), nil))
	}
}

func handleNetworkRoute(c *Client) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tabID, err := r.RequireString("tabId")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		pattern, err := r.RequireString("pattern")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		payload := map[string]any{"pattern": pattern}
		if action := optString(r, "action"); action != "" {
			payload["action"] = action
		} else {
			payload["action"] = "continue"
		}
		if body := optString(r, "body"); body != "" {
			payload["body"] = body
		}
		if ct := optString(r, "contentType"); ct != "" {
			payload["contentType"] = ct
		}
		if rt := optString(r, "resourceType"); rt != "" {
			payload["resourceType"] = rt
		}
		if status, ok := optInt(r, "status"); ok {
			payload["status"] = status
		}
		if method := optString(r, "method"); method != "" {
			payload["method"] = method
		}

		path := tabNetworkRoutePath(tabID)
		return toolResult(c.Post(ctx, path, payload))
	}
}

func handleNetworkUnroute(c *Client) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tabID, err := r.RequireString("tabId")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		q := url.Values{}
		if pattern := optString(r, "pattern"); pattern != "" {
			q.Set("pattern", pattern)
		}
		path := tabNetworkRoutePath(tabID)
		return toolResult(c.Delete(ctx, path, q))
	}
}

func handleNetworkRules(c *Client) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tabID, err := r.RequireString("tabId")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		path := tabNetworkRoutePath(tabID)
		return toolResult(c.Get(ctx, path, nil))
	}
}

func tabNetworkRoutePath(tabID string) string {
	return tabRoutePath(tabID, "network/route")
}
