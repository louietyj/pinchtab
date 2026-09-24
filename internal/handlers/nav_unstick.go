package handlers

import (
	"context"
	"log/slog"
	"net/url"
	"regexp"
	"time"

	"github.com/pinchtab/pinchtab/internal/bridge"
)

// stuckRedirectPages are hand-off pages meant to forward to the URL in their
// query, which sometimes never do. AliExpress sends a fresh profile's first
// visit through login.aliexpress.com/sync_cookie_read.htm?xman_goto=<target>,
// and in claude.ai runs it stalled there until the nav was repeated.
var stuckRedirectPages = []struct {
	re    *regexp.Regexp
	param string
}{
	{regexp.MustCompile(`^https://login\.aliexpress\.[a-z.]+/sync_cookie_read\.htm\?`), "xman_goto"},
}

// stuckRedirectTarget is where a hand-off page at landed should have gone, if
// it is one and the target is on requested's host. "" otherwise.
func stuckRedirectTarget(landed, requested string) string {
	for _, p := range stuckRedirectPages {
		if !p.re.MatchString(landed) {
			continue
		}
		u, err := url.Parse(landed)
		if err != nil {
			return ""
		}
		target, err := url.Parse(u.Query().Get(p.param))
		if err != nil || target.Host == "" {
			return ""
		}
		// Only ever finish the trip the caller asked for.
		if req, err := url.Parse(requested); err != nil || req.Host != target.Host {
			return ""
		}
		return target.String()
	}
	return ""
}

// stuckRedirectGrace is how long a hand-off page gets to forward by itself.
const stuckRedirectGrace = 4 * time.Second

// unstickRedirect finishes a navigation that stopped on a hand-off page.
func (h *Handlers) unstickRedirect(ctx context.Context, requested string, maxRedirects int) {
	landed, _ := h.Bridge.CurrentURL(ctx)
	target := stuckRedirectTarget(landed, requested)
	if target == "" {
		return
	}
	deadline := time.Now().Add(stuckRedirectGrace)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
		if now, _ := h.Bridge.CurrentURL(ctx); stuckRedirectTarget(now, requested) == "" {
			return
		}
	}
	slog.Info("navigation stalled on a hand-off page; going to its target", "landed", landed, "target", target)
	if _, err := h.Bridge.Navigate(ctx, target, bridge.NavigateParams{MaxRedirects: maxRedirects}); err != nil {
		slog.Warn("finishing a stalled hand-off failed", "target", target, "err", err)
	}
}
