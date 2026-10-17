package mcp

import (
	"context"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"
)

// a11yAuditTool declares the pinchtab_a11y_audit tool: an accessibility audit of
// the current page, native by default or axe-core with engine=axe.
func a11yAuditTool() mcp.Tool {
	return mcp.NewTool("pinchtab_a11y_audit",
		mcp.WithDescription("Audit the current page for accessibility issues. Default 'native' engine scores the accessibility snapshot. engine='axe' runs vendored axe-core in the page's isolated world and returns industry rule ids (image-alt, label, color-contrast, …) with WCAG tags, help URLs, a comparable score, and a snapshot 'ref' for each failing element so you can act on it with pinchtab_click/fill."),
		tabIDParam(),
		mcp.WithString("engine", mcp.Description("Audit engine: 'native' (default) or 'axe'. Any other value is a 400.")),
		mcp.WithString("tags", mcp.Description("axe: comma list of WCAG tags to run, e.g. 'wcag2a,wcag2aa,best-practice'. best-practice runs only when named.")),
		mcp.WithString("rules", mcp.Description("axe: comma allowlist of rule ids to run; wins over tags.")),
		mcp.WithBoolean("includeIncomplete", mcp.Description("axe: include rules axe could not decide (the incomplete set)")),
		browserParam(),
	)
}

// handleA11yAudit maps the tool arguments onto GET /a11y/audit query params.
func handleA11yAudit(c *Client) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q := url.Values{}
		if tabID := optString(r, "tabId"); tabID != "" {
			q.Set("tabId", tabID)
		}
		if engine := optString(r, "engine"); engine != "" {
			q.Set("engine", engine)
		}
		if tags := optString(r, "tags"); tags != "" {
			q.Set("tags", tags)
		}
		if rules := optString(r, "rules"); rules != "" {
			q.Set("rules", rules)
		}
		if v, ok := optBool(r, "includeIncomplete"); ok && v {
			q.Set("includeIncomplete", "true")
		}
		// The axe engine re-epochs the tab's ref cache and publishes a fresh
		// vocabulary token. Capture it under the request's tab key (as snapshot
		// does) so a later pinchtab_click/fill on a returned ref echoes that token
		// rather than a stale one the server refuses 409. The native engine sends
		// no token, so this is a no-op there.
		return toolResult(c.GetCapturingVocab(ctx, "/a11y/audit", routedQuery(r, q), optString(r, "tabId")))
	}
}
