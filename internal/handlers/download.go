package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pinchtab/pinchtab/internal/activity"
	"github.com/pinchtab/pinchtab/internal/authn"
	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/httpx"
	"github.com/pinchtab/pinchtab/internal/routes"
)

// The wait for a server to start sending the file: an export built on demand
// can take a while before its first byte.
const (
	downloadStartWaitDefault = 30 * time.Second
	downloadStartWaitMax     = 2 * time.Minute
)

// HandleDownload has the browser download a URL with its session (cookies,
// fingerprint) into its download manager, and answers once the download has
// begun. The file is collected like one a page started: /download/wait?guid=
// until it finishes, then /download/content?guid= streams it back.
//
// GET /download?url=<url>[&tabId=<id>][&waitMs=<ms>]
func (h *Handlers) HandleDownload(w http.ResponseWriter, r *http.Request) {
	if !h.Config.AllowDownload {
		h.writeCapabilityDisabled(w, routes.CapDownload)
		return
	}
	q := r.URL.Query()
	dlURL := q.Get("url")
	if dlURL == "" {
		httpx.Error(w, 400, fmt.Errorf("url parameter required"))
		return
	}
	if q.Get("output") != "" || q.Get("raw") != "" {
		httpx.ErrorCode(w, 400, "download_form_removed",
			"/download no longer returns the file: it answers with the download, whose bytes come from /download/content?guid=<guid> once /download/wait?guid=<guid> says it finished", false, nil)
		return
	}

	// Download allowlist: prefer the explicit per-feature list, but fall
	// back to the unified security.allowedDomains. This way operators only
	// need to configure trusted domains once.
	allowed := h.Config.DownloadAllowedDomains
	if len(allowed) == 0 {
		allowed = h.Config.AllowedDomains
	}
	validator := newDownloadURLGuard(allowed)
	if err := validator.Validate(dlURL); err != nil {
		writeDownloadGuardError(w, fmt.Errorf("unsafe URL: %w", err))
		return
	}

	if tabID := strings.TrimSpace(q.Get("tabId")); tabID != "" {
		if !h.enforceDownloadTabPolicy(w, r, tabID, dlURL) {
			return
		}
	}

	// Re-check scheme before navigation (validateDownloadURL already enforces this,
	// but inline check satisfies CodeQL SSRF analysis).
	if parsed, err := url.Parse(dlURL); err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		httpx.Error(w, 400, fmt.Errorf("invalid download URL scheme"))
		return
	}

	wait := queryMillis(q.Get("waitMs"), downloadStartWaitDefault, downloadStartWaitMax)
	ctx, cancel := context.WithTimeout(r.Context(), wait)
	defer cancel()

	requestGuard := newDownloadRequestGuard(validator, h.Config.MaxRedirects)
	pd, err := h.Bridge.StartURLDownload(ctx, dlURL, bridge.DownloadOpts{
		ValidateURL: func(rawURL string, isRedirect bool) error {
			return requestGuard.Validate(rawURL, isRedirect)
		},
		ValidateRemoteIP: validateDownloadRemoteIPAddress,
		IsDomainAllowed:  validator.isDomainAllowed,
	})
	if err != nil {
		writeDownloadError(w, err, wait)
		return
	}
	h.recordActivity(r, activity.Update{Action: "download", URL: dlURL})
	httpx.JSON(w, 200, map[string]any{"state": pd.State, "download": pd})
}

// enforceDownloadTabPolicy applies tab-scoped policy (lease, current-tab domain
// policy, and cookie-auth download-scope) for a tab-targeted download. It writes
// the error response and returns false on any failure.
func (h *Handlers) enforceDownloadTabPolicy(w http.ResponseWriter, r *http.Request, tabID, dlURL string) bool {
	ctx, resolvedTabID, err := h.tabContext(r, tabID)
	if err != nil {
		WriteTabContextError(w, err, 404)
		return false
	}
	owner := resolveOwner(r, "")
	if err := h.enforceTabLease(resolvedTabID, owner); err != nil {
		httpx.ErrorCode(w, http.StatusLocked, "tab_locked", err.Error(), false, nil)
		return false
	}
	currentURL, ok := h.applyTabGuards(w, r, ctx, resolvedTabID, guardDialogBlocked|guardDomainPolicy|guardHandoffPause)
	if !ok {
		return false
	}
	if currentURL == "" {
		if provider, ok := bridgeAs[tabPolicyStateProvider](h.Bridge); ok {
			if state, ok := provider.GetTabPolicyState(resolvedTabID); ok && state.CurrentURL != "" {
				currentURL = state.CurrentURL
			}
		}
	}
	if currentURL == "" {
		lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		var lookupErr error
		currentURL, lookupErr = h.Bridge.CurrentURL(lookupCtx)
		if lookupErr != nil {
			httpx.Error(w, 500, fmt.Errorf("resolve current tab url: %w", lookupErr))
			return false
		}
	}
	if authn.CredentialsFromRequest(r).Method == authn.MethodCookie {
		if err := validateTabScopedDownloadURL(currentURL, dlURL); err != nil {
			httpx.ErrorCode(w, http.StatusForbidden, "download_scope_forbidden", err.Error(), false, map[string]any{
				"currentURL":   currentURL,
				"requestedURL": dlURL,
			})
			return false
		}
	}
	return true
}

// writeDownloadError maps a StartURLDownload error to an HTTP response.
func writeDownloadError(w http.ResponseWriter, err error, wait time.Duration) {
	var status *bridge.HTTPStatusError
	switch {
	case errors.Is(err, bridge.ErrTooManyRedirects):
		httpx.Error(w, 422, fmt.Errorf("download: %w", err))
	case errors.Is(err, bridge.ErrDownloadTimeout):
		httpx.Error(w, 504, fmt.Errorf("the server had not started sending the file after %s", wait.Round(time.Second)))
	case errors.Is(err, bridge.ErrPageDownloadsOff):
		httpx.ErrorCode(w, 409, "downloads_off", err.Error(), false, nil)
	case errors.As(err, &status):
		httpx.Error(w, 502, err)
	default:
		errMsg := err.Error()
		if strings.Contains(errMsg, "blocked") || strings.Contains(errMsg, "private") || strings.Contains(errMsg, "unsafe") {
			httpx.Error(w, 400, fmt.Errorf("unsafe browser request: %w", err))
			return
		}
		httpx.Error(w, 502, err)
	}
}

// @Endpoint GET /tabs/{id}/download
func (h *Handlers) HandleTabDownload(w http.ResponseWriter, r *http.Request) {
	tabID, ok := requirePathTabID(w, r)
	if !ok {
		return
	}
	if _, _, err := h.tabContext(r, tabID); err != nil {
		WriteTabContextError(w, err, 404)
		return
	}
	h.HandleDownload(w, cloneWithTabIDQuery(r, tabID))
}
