package mcp

import "github.com/mark3labs/mcp-go/mcp"

// Parameters that more than one tool declares live here, so a reword is one
// edit with a uniform effect on every tool that carries it. A wording that
// describes genuinely different behaviour for the same parameter name — a tabId
// whose empty value clears every tab rather than selecting the current one —
// stays spelled out at its tool and is deliberately not folded in here.

func browserParam() mcp.ToolOption {
	return mcp.WithString("browser",
		mcp.Description("Browser to use for this request (e.g. chrome, cloak, ghost-chrome)."))
}

func tabIDParam() mcp.ToolOption {
	return mcp.WithString("tabId", mcp.Description("Target tab ID (optional, uses current tab if empty)"))
}

func requiredTabIDParam() mcp.ToolOption {
	return mcp.WithString("tabId", mcp.Required(), mcp.Description("Target tab ID"))
}

func selectorParam() mcp.ToolOption {
	return mcp.WithString("selector", mcp.Description("Unified selector: ref (e.g. 'e5'), CSS, XPath, text, or semantic. Non-ref selectors resolve in the current frame scope."))
}

var selectorArgKeys = []string{"selector", "ref", "element", "target"}

func selectorAliasParams() mcp.ToolOption {
	return func(tool *mcp.Tool) {
		for _, alias := range selectorArgKeys[1:] {
			stringAliasParam(alias, selectorArgKeys[0])(tool)
		}
	}
}

func stringAliasParam(alias, canonical string) mcp.ToolOption {
	return mcp.WithString(alias, aliasDescription(canonical))
}

func numberAliasParam(alias, canonical string) mcp.ToolOption {
	return mcp.WithNumber(alias, aliasDescription(canonical))
}

func aliasDescription(canonical string) mcp.PropertyOption {
	return mcp.Description("(deprecated) alias of '" + canonical + "'; send '" + canonical + "' instead")
}

func nodeIDParam() mcp.ToolOption {
	return mcp.WithNumber("nodeId", mcp.Description("Optional backend node ID to target directly"))
}

func queryParam() mcp.ToolOption {
	return mcp.WithString("query", mcp.Description("Alias for semantic targeting when selector is omitted"))
}

func timeoutMsParam() mcp.ToolOption {
	return mcp.WithNumber("timeoutMs", mcp.Description("Timeout in milliseconds (default: 10000, max: 30000)"))
}

func snapParam() mcp.ToolOption {
	return mcp.WithBoolean("snap", mcp.Description("Return interactive compact snapshot after the navigation (saves a round-trip)"))
}

func humanizeParam() mcp.ToolOption {
	return mcp.WithBoolean("humanize", mcp.Description("Use the humanized input path for this request — bezier pointer path, per-step jitter, pre-press delays. Overrides the instance default; omit to inherit it."))
}

func textToTypeParam() mcp.ToolOption {
	return mcp.WithString("text", mcp.Required(), mcp.Description("Text to type"))
}

func expressionParam(name string) mcp.ToolOption {
	return mcp.WithString(name, mcp.Required(), mcp.Description("JavaScript expression to evaluate"))
}

func imageFormatParam() mcp.ToolOption {
	return mcp.WithString("format", mcp.Description("Image format: 'jpeg' (default) or 'png'"))
}
