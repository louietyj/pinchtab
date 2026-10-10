package apiclient

import (
	"net/http"
	"os"
	"strings"
)

// ExtraHeadersEnv names headers the CLI adds to every request, one
// "Name: value" per line. It exists for servers behind an authenticating
// proxy (e.g. Cloudflare Access service-token headers), which the CLI's own
// Authorization header cannot satisfy.
const ExtraHeadersEnv = "PINCHTAB_HEADERS"

// ExtraHeaders parses PINCHTAB_HEADERS. Malformed lines are skipped.
func ExtraHeaders() http.Header {
	h := http.Header{}
	for _, line := range strings.Split(os.Getenv(ExtraHeadersEnv), "\n") {
		name, value, ok := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			continue
		}
		h.Add(name, strings.TrimSpace(value))
	}
	return h
}

// ApplyExtraHeaders sets the PINCHTAB_HEADERS headers on h.
func ApplyExtraHeaders(h http.Header) {
	for name, values := range ExtraHeaders() {
		h[name] = values
	}
}

// HeaderTransport adds the PINCHTAB_HEADERS headers to every request, for
// the CLI's ad hoc clients that do not go through the main CLI transport.
type HeaderTransport struct {
	Base http.RoundTripper
}

func (t HeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	cloned := req.Clone(req.Context())
	cloned.Header = req.Header.Clone()
	ApplyExtraHeaders(cloned.Header)
	return base.RoundTrip(cloned)
}
