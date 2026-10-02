package cdpops

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
)

func TestNavigationLifecycleWaiterUsesMainFrameNavigation(t *testing.T) {
	w := &navigationLifecycleWaiter{
		mainFrame: "main",
		ready:     make(chan struct{}),
	}

	w.onEvent(&page.EventLifecycleEvent{FrameID: "main", Name: "DOMContentLoaded"})
	w.onEvent(&page.EventFrameNavigated{Frame: &cdp.Frame{ID: "child", ParentID: "main"}})
	w.onEvent(&page.EventLifecycleEvent{FrameID: "child", Name: "load"})
	select {
	case <-w.ready:
		t.Fatal("child-frame lifecycle marked the navigation ready")
	default:
	}

	w.onEvent(&page.EventFrameNavigated{Frame: &cdp.Frame{ID: "main"}})
	w.onEvent(&page.EventLifecycleEvent{FrameID: "main", Name: "DOMContentLoaded"})
	select {
	case <-w.ready:
	default:
		t.Fatal("main-frame DOMContentLoaded did not mark the navigation ready")
	}
}

func TestNavigationLifecycleWaiterAcceptsSameDocumentNavigation(t *testing.T) {
	w := &navigationLifecycleWaiter{
		mainFrame: "main",
		ready:     make(chan struct{}),
	}
	w.onEvent(&page.EventNavigatedWithinDocument{FrameID: "main"})

	select {
	case <-w.ready:
	default:
		t.Fatal("same-document navigation did not mark the navigation ready")
	}
}

func TestNavigationLifecycleWaiterReportsDownload(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mime     string
		headers  network.Headers
		commit   bool
		download bool
	}{
		{"attachment", "text/plain", network.Headers{"content-disposition": "attachment; filename=x.txt"}, false, true},
		{"unrenderable type", "application/zip", nil, false, true},
		{"superseded html", "text/html", nil, false, false},
		{"failed after commit", "application/zip", nil, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &navigationLifecycleWaiter{mainFrame: "main", ready: make(chan struct{})}
			w.onEvent(&network.EventResponseReceived{
				RequestID: "doc", FrameID: "main", Type: network.ResourceTypeDocument,
				Response: &network.Response{MimeType: tc.mime, Headers: tc.headers},
			})
			if tc.commit {
				w.onEvent(&page.EventFrameNavigated{Frame: &cdp.Frame{ID: "main"}})
			}
			w.onEvent(&network.EventLoadingFailed{RequestID: "doc", ErrorText: "net::ERR_ABORTED", Canceled: true})

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			err := w.wait(ctx)
			var dlErr *DownloadNavigationError
			if tc.download {
				if !errors.As(err, &dlErr) || dlErr.MIMEType != tc.mime || !errors.Is(err, ErrNavigationIsDownload) {
					t.Fatalf("wait error = %v, want a download of %s", err, tc.mime)
				}
				return
			}
			if errors.As(err, &dlErr) {
				t.Fatalf("wait error = %v, want no download", err)
			}
		})
	}
}

func TestNavigationLifecycleWaiterReportsLastTargetState(t *testing.T) {
	w := &navigationLifecycleWaiter{
		mainFrame: "frame-1",
		targetID:  "target-1",
		lastEvent: "init",
		ready:     make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := w.wait(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("wait error = %v, want context cancellation", err)
	}
	for _, detail := range []string{"target-1", "frame-1", `last lifecycle event "init"`} {
		if !strings.Contains(err.Error(), detail) {
			t.Fatalf("wait error %q does not contain %q", err, detail)
		}
	}
}
