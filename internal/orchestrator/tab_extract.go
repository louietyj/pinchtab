package orchestrator

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/pinchtab/pinchtab/internal/httpx"
)

// maxBodyPeek caps how much of an inbound JSON request body the orchestrator
// will buffer to look for an explicit "tabId" field. Bodies larger than this
// (or with no Content-Length, or non-JSON content type) are passed through
// without inspection.
const maxBodyPeek = 64 * 1024

// TabIDSource identifies where the explicit tab id was found.
type TabIDSource string

const (
	TabIDSourceNone  TabIDSource = ""
	TabIDSourcePath  TabIDSource = "path"
	TabIDSourceQuery TabIDSource = "query"
	TabIDSourceBody  TabIDSource = "body"
)

// ExtractExplicitTabID inspects the request for a caller-supplied tab id in
// (in order) the routed path value `id`, the `tabId` query parameter, and
// finally a JSON body containing a `tabId` field. The first non-empty value
// wins.
//
// Body inspection is restricted to requests whose Content-Type begins with
// `application/json` and whose declared Content-Length is positive and at
// most maxBodyPeek bytes. Streaming, multipart, oversized, or
// unknown-length bodies are skipped — the body is left untouched. After a
// successful peek, the body is rewound so downstream handlers see it intact.
func ExtractExplicitTabID(r *http.Request) (string, TabIDSource) {
	if r == nil {
		return "", TabIDSourceNone
	}
	if id := strings.TrimSpace(r.PathValue("id")); id != "" {
		return id, TabIDSourcePath
	}
	if id := strings.TrimSpace(r.URL.Query().Get("tabId")); id != "" {
		return id, TabIDSourceQuery
	}
	if id := peekBodyTabID(r); id != "" {
		return id, TabIDSourceBody
	}
	return "", TabIDSourceNone
}

func ExtractRequestedBrowser(r *http.Request) string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.URL.Query().Get("browser"))
}

func peekBodyTabID(r *http.Request) string {
	return peekBodyStringField(r, "tabId")
}

func peekBodyStringField(r *http.Request, field string) string {
	if r == nil || !httpx.MayHaveBody(r) || r.ContentLength > maxBodyPeek {
		return ""
	}
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		return ""
	}
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	if !strings.EqualFold(strings.TrimSpace(ct), "application/json") {
		return ""
	}

	original := r.Body
	buf, err := io.ReadAll(io.LimitReader(original, maxBodyPeek+1))
	r.Body = httpx.ReplayBody(buf, original)
	if err != nil || len(buf) > maxBodyPeek {
		return ""
	}

	var probe map[string]any
	if err := json.Unmarshal(buf, &probe); err != nil {
		return ""
	}
	raw, ok := probe[field]
	if !ok {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}
