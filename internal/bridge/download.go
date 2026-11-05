package bridge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	bridgeruntime "github.com/pinchtab/pinchtab/internal/bridge/runtime"
)

// Texts match the historical messages so anything still string-matching keeps working.
var (
	ErrDownloadTimeout = errors.New("download timed out")
	// ErrPageDownloadsOff is returned when instanceDefaults.downloadBehavior is
	// "off": every download goes through the browser's download manager.
	ErrPageDownloadsOff = errors.New("page downloads are off (instanceDefaults.downloadBehavior)")
)

// StartURLDownload has the browser download dlURL into its download manager
// from a tab of its own, so it is collected like a download a page started. A
// response the browser would show (a PDF, JSON, a page) is marked an
// attachment so it downloads too. ctx bounds only the wait for the server to
// start answering: the transfer outlives this call and the tab.
func (b *Bridge) StartURLDownload(ctx context.Context, dlURL string, opts DownloadOpts) (*PageDownload, error) {
	d := b.downloadsTracker()
	if d == nil {
		return nil, ErrPageDownloadsOff
	}
	// A personal browser would ask where to save.
	b.QuietDownloads()

	tabCtx, tabCancel := chromedp.NewContext(b.BrowserContext())
	defer tabCancel()
	tCtx, tCancel := context.WithCancel(tabCtx)
	defer tCancel()
	stop := context.AfterFunc(ctx, tCancel)
	defer stop()

	// Run with no actions opens the tab, so its id (its top frame's) is known
	// before anything loads, and the download is recognised as this one's.
	if err := chromedp.Run(tCtx); err != nil {
		return nil, fmt.Errorf("open a tab for the download: %w", err)
	}
	frameID := string(chromedp.FromContext(tCtx).Target.TargetID)
	d.expect(frameID)
	defer d.unexpect(frameID)

	// mu guards the fields below, written from the event-dispatch goroutine.
	var mu sync.Mutex
	var blockedErr, statusErr error
	var mainRequest network.RequestID
	note := func(dst *error, err error) {
		mu.Lock()
		if *dst == nil {
			*dst = err
		}
		mu.Unlock()
	}
	isMain := func(id network.RequestID) bool {
		mu.Lock()
		defer mu.Unlock()
		return mainRequest != "" && id == mainRequest
	}
	firstErr := func() error {
		mu.Lock()
		defer mu.Unlock()
		if blockedErr != nil {
			return blockedErr
		}
		return statusErr
	}

	// The tab bypasses TabManager's tabSetup, so proxy auth is answered here.
	proxyAuth := b.Config != nil && bridgeruntime.ProxyAuthEnabled(b.Config.Proxy)
	enableFetch := fetch.Enable()
	if proxyAuth {
		enableFetch = enableFetch.WithHandleAuthRequests(true)
	}
	if err := chromedp.Run(tCtx, enableFetch); err != nil {
		return nil, fmt.Errorf("fetch enable: %w", err)
	}

	exec := func() context.Context {
		return cdp.WithExecutor(tCtx, chromedp.FromContext(tCtx).Target)
	}
	onResponse := func(e *fetch.EventRequestPaused) {
		status := int(e.ResponseStatusCode)
		if !isMain(e.NetworkID) || e.ResponseErrorReason != "" || (status >= 300 && status < 400) {
			_ = fetch.ContinueRequest(e.RequestID).Do(exec())
			return
		}
		if status >= 400 {
			note(&statusErr, &HTTPStatusError{StatusCode: status})
			_ = fetch.FailRequest(e.RequestID, network.ErrorReasonBlockedByClient).Do(exec())
			return
		}
		headers := make([]*fetch.HeaderEntry, 0, len(e.ResponseHeaders)+1)
		for _, h := range e.ResponseHeaders {
			if strings.EqualFold(h.Name, "Content-Disposition") {
				if strings.HasPrefix(strings.ToLower(strings.TrimSpace(h.Value)), "attachment") {
					_ = fetch.ContinueRequest(e.RequestID).Do(exec())
					return
				}
				continue
			}
			headers = append(headers, h)
		}
		headers = append(headers, &fetch.HeaderEntry{Name: "Content-Disposition", Value: "attachment"})
		_ = fetch.ContinueResponse(e.RequestID).WithResponseCode(int64(status)).
			WithResponseHeaders(headers).Do(exec())
	}

	chromedp.ListenTarget(tCtx, func(ev any) {
		switch e := ev.(type) {
		case *fetch.EventAuthRequired:
			if !proxyAuth {
				return
			}
			go func() {
				resp := bridgeruntime.AuthChallengeResponse(e, b.Config.Proxy.Username, b.Config.Proxy.Password)
				_ = fetch.ContinueWithAuth(e.RequestID, resp).Do(exec())
			}()
		case *fetch.EventRequestPaused:
			if e.ResponseStatusCode != 0 || e.ResponseErrorReason != "" {
				go onResponse(e)
				return
			}
			go func() {
				if opts.ValidateURL != nil {
					if err := opts.ValidateURL(e.Request.URL, e.RedirectedRequestID != ""); err != nil {
						note(&blockedErr, err)
						_ = fetch.FailRequest(e.RequestID, network.ErrorReasonBlockedByClient).Do(exec())
						return
					}
				}
				_ = fetch.ContinueRequest(e.RequestID).
					WithInterceptResponse(e.ResourceType == network.ResourceTypeDocument).
					Do(exec())
			}()
		case *network.EventRequestWillBeSent:
			if e.Type == network.ResourceTypeDocument && string(e.FrameID) == frameID {
				mu.Lock()
				mainRequest = e.RequestID
				mu.Unlock()
			}
		case *network.EventResponseReceived:
			// The remote address is known only once the response has come, by
			// which time the download may have begun; it is canceled below.
			if isMain(e.RequestID) && opts.ValidateRemoteIP != nil &&
				(opts.IsDomainAllowed == nil || !opts.IsDomainAllowed(e.Response.URL)) {
				if err := opts.ValidateRemoteIP(e.Response.RemoteIPAddress); err != nil {
					note(&blockedErr, err)
				}
			}
		}
	})

	if err := chromedp.Run(tCtx, network.Enable()); err != nil {
		return nil, fmt.Errorf("network enable: %w", err)
	}

	var errorText string
	var isDownload bool
	navErr := chromedp.Run(tCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		_, _, errorText, isDownload, err = page.Navigate(dlURL).Do(ctx)
		return err
	}))
	if err := firstErr(); err != nil {
		return nil, err
	}
	if navErr != nil {
		if ctx.Err() != nil {
			return nil, ErrDownloadTimeout
		}
		return nil, fmt.Errorf("navigate to download URL: %w", navErr)
	}
	if !isDownload {
		if errorText != "" {
			return nil, fmt.Errorf("navigate to download URL: %s", errorText)
		}
		return nil, errors.New("the URL opened as a page instead of downloading")
	}

	pd, err := d.waitBegun(tCtx, frameID)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ErrDownloadTimeout
		}
		return nil, err
	}
	if err := firstErr(); err != nil {
		b.cancelDownload(pd.GUID)
		return nil, err
	}
	return pd, nil
}

// HTTPStatusError is a download the server answered with an error status.
type HTTPStatusError struct{ StatusCode int }

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("remote server returned HTTP %d", e.StatusCode)
}

func (b *Bridge) cancelDownload(guid string) {
	b.downloadsMu.Lock()
	ctx := b.downloadsCtx
	b.downloadsMu.Unlock()
	if ctx == nil {
		return
	}
	_ = runOnBrowser(ctx, func(ctx context.Context) error {
		return browser.CancelDownload(guid).Do(ctx)
	})
}
