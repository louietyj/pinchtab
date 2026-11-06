package bridge

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	bridgecdpops "github.com/pinchtab/pinchtab/internal/bridge/cdpops"
)

// ResourceBlocking is what a tab refuses to load. Ads are matched by URL, since
// a host is what makes a request an ad; images and media by the type Chrome
// gives the request, whatever its URL.
type ResourceBlocking struct {
	// URLPatterns are ad and tracker URL globs (Network.setBlockedURLs).
	URLPatterns []string
	Images      bool
	// Media also hides Media Source Extensions: streaming players fetch video
	// as plain XHR and feed it to a MediaSource, so nothing on those requests
	// marks them as video. The page can see that MediaSource is missing.
	Media bool
}

// ResourceBlockingFor builds the policy for the blockAds/blockImages/blockMedia
// settings. Media implies Images.
func ResourceBlockingFor(ads, images, media bool) ResourceBlocking {
	rb := ResourceBlocking{Images: images || media, Media: media}
	if ads {
		rb.URLPatterns = AdBlockPatterns
	}
	return rb
}

// IsZero reports whether the policy blocks nothing.
func (rb ResourceBlocking) IsZero() bool {
	return len(rb.URLPatterns) == 0 && !rb.Images && !rb.Media
}

func (rb ResourceBlocking) resourceTypes() []network.ResourceType {
	var types []network.ResourceType
	if rb.Images || rb.Media {
		types = append(types, network.ResourceTypeImage)
	}
	if rb.Media {
		types = append(types, network.ResourceTypeMedia)
	}
	return types
}

const hideMediaSourceScript = `delete self.MediaSource; delete self.ManagedMediaSource;`

// blockedTypePatterns pauses only the blocked types, unless proxy auth needs
// every request intercepted: Chrome raises auth challenges only for those.
func blockedTypePatterns(types []network.ResourceType, proxyAuth bool) []*fetch.RequestPattern {
	if proxyAuth {
		return []*fetch.RequestPattern{{URLPattern: "*"}}
	}
	patterns := make([]*fetch.RequestPattern, 0, len(types))
	for _, t := range types {
		patterns = append(patterns, &fetch.RequestPattern{URLPattern: "*", ResourceType: t})
	}
	return patterns
}

// SetResourceBlocking replaces the tab's blocking policy. It lives here because
// type blocking shares the tab's Fetch domain with rules and proxy auth. As for
// AddRule, ctx must live as long as the tab: once the listener's context ends,
// blocked requests stay paused.
func (rm *RouteManager) SetResourceBlocking(ctx context.Context, tabID string, rb ResourceBlocking) error {
	if rm == nil {
		return fmt.Errorf("route manager not initialized")
	}
	types := rb.resourceTypes()
	proxyAuth := rm.proxyAuthOn()

	rm.mu.Lock()
	state := rm.perTab[tabID]
	if state == nil {
		if len(types) == 0 {
			rm.mu.Unlock()
			return setBlockedURLs(ctx, rb.URLPatterns)
		}
		state = &tabRouteState{}
		rm.perTab[tabID] = state
	}
	typesChanged := !slices.Equal(state.blockTypes, types)
	priorTypes, priorFetchEnabled, priorNarrowFetch := state.blockTypes, state.fetchEnabled, state.narrowFetch
	state.blockTypes = types
	hasRules := len(state.rules) > 0

	needRegister := len(types) > 0 && state.listenCancel == nil
	if needRegister {
		state.listenCtx, state.listenCancel = context.WithCancel(ctx)
	}
	listenCtx := state.listenCtx

	// Rules already intercept every request, so only a tab without them needs
	// its Fetch patterns changed.
	var enable []*fetch.RequestPattern
	teardown := false
	wasEnabled := state.fetchEnabled
	cancel := state.listenCancel
	switch {
	case hasRules:
	case len(types) > 0 && (typesChanged || !state.fetchEnabled):
		enable = blockedTypePatterns(types, proxyAuth)
		state.fetchEnabled = true
		state.narrowFetch = !proxyAuth
	case len(types) == 0:
		teardown = true
		state.fetchEnabled = false
		state.narrowFetch = false
		state.listenCancel = nil
		state.listenCtx = nil
	}

	oldScript := state.mseScriptID
	addScript := rb.Media && oldScript == ""
	removeScript := !rb.Media && oldScript != ""
	if removeScript {
		state.mseScriptID = ""
	}
	if teardown {
		delete(rm.perTab, tabID)
	}
	rm.mu.Unlock()

	if needRegister {
		rm.registerListener(listenCtx, tabID)
	}
	if enable != nil {
		rm.suppressPause(tabID, true)
		if err := chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
			return fetch.Enable().WithPatterns(enable).WithHandleAuthRequests(proxyAuth).Do(c)
		})); err != nil {
			var orphan context.CancelFunc
			rm.mu.Lock()
			if s := rm.perTab[tabID]; s != nil {
				s.blockTypes, s.fetchEnabled, s.narrowFetch = priorTypes, priorFetchEnabled, priorNarrowFetch
				if len(s.rules) == 0 && len(s.blockTypes) == 0 && s.mseScriptID == "" {
					orphan = s.listenCancel
					delete(rm.perTab, tabID)
				}
			}
			rm.mu.Unlock()
			if orphan != nil {
				orphan()
			}
			if !priorFetchEnabled {
				rm.suppressPause(tabID, false)
			}
			return fmt.Errorf("fetch.enable: %w", err)
		}
	}
	if teardown {
		rm.releaseFetch(ctx, tabID, wasEnabled, cancel)
	}
	if err := setBlockedURLs(ctx, rb.URLPatterns); err != nil {
		return err
	}

	if removeScript {
		if err := chromedp.Run(ctx, page.RemoveScriptToEvaluateOnNewDocument(oldScript)); err != nil {
			slog.Debug("remove media-source script failed", "tabId", tabID, "err", err)
		}
	}
	if addScript {
		var id page.ScriptIdentifier
		if err := chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
			var err error
			id, err = page.AddScriptToEvaluateOnNewDocument(hideMediaSourceScript).Do(c)
			return err
		})); err != nil {
			return fmt.Errorf("hide media source: %w", err)
		}
		rm.mu.Lock()
		if s := rm.perTab[tabID]; s != nil {
			s.mseScriptID = id
		}
		rm.mu.Unlock()
	}
	return nil
}

func setBlockedURLs(ctx context.Context, patterns []string) error {
	if err := bridgecdpops.SetBlockedURLs(ctx, patterns); err != nil {
		return fmt.Errorf("set blocked urls: %w", err)
	}
	return nil
}

// blocksType reports whether the tab fails requests of resourceType outright.
// Callers hold rm.mu.
func (s *tabRouteState) blocksType(resourceType string) bool {
	return slices.ContainsFunc(s.blockTypes, func(t network.ResourceType) bool {
		return strings.EqualFold(string(t), resourceType)
	})
}
