package bridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/pinchtab/pinchtab/internal/cdptk"
	"github.com/pinchtab/pinchtab/internal/config"
)

// Downloads the page starts itself (a form POST answered with an attachment, a
// blob or data URL) go to Chrome's download manager, which drops them under CDP
// unless pointed at a directory. The bridge does that, follows each download by
// its events, and copies a finished one back off whichever disk it landed on.

const (
	PageDownloadInProgress = "inProgress"
	PageDownloadCompleted  = "completed"
	PageDownloadCanceled   = "canceled"
)

// ErrNoPageDownload is returned when no download began on the tab in time.
var ErrNoPageDownload = errors.New("no download started on this tab")

// PageDownload is one download as Chrome reported it.
type PageDownload struct {
	GUID          string    `json:"guid"`
	TabID         string    `json:"tabId,omitempty"`
	URL           string    `json:"url"`
	Filename      string    `json:"filename"`
	State         string    `json:"state"`
	ReceivedBytes int64     `json:"receivedBytes"`
	TotalBytes    int64     `json:"totalBytes"`
	FilePath      string    `json:"filePath,omitempty"`
	Error         string    `json:"error,omitempty"`
	Started       time.Time `json:"started"`

	frameID string
	seq     uint64
	claimed bool
}

type pageDownloads struct {
	mu       sync.Mutex
	byGUID   map[string]*PageDownload
	order    []*PageDownload
	seq      uint64
	changed  chan struct{}
	maxBytes int64
}

func newPageDownloads() *pageDownloads {
	return &pageDownloads{byGUID: map[string]*PageDownload{}, changed: make(chan struct{})}
}

// notifyLocked wakes every waiter. Callers hold mu.
func (d *pageDownloads) notifyLocked() {
	close(d.changed)
	d.changed = make(chan struct{})
}

// pageDownloadsKeep bounds the history, dropping the oldest.
const pageDownloadsKeep = 100

func (d *pageDownloads) begin(ev *browser.EventDownloadWillBegin) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seq++
	pd := &PageDownload{
		GUID:     ev.GUID,
		URL:      ev.URL,
		Filename: ev.SuggestedFilename,
		State:    PageDownloadInProgress,
		Started:  time.Now(),
		frameID:  string(ev.FrameID),
		seq:      d.seq,
	}
	d.byGUID[ev.GUID] = pd
	d.order = append(d.order, pd)
	if len(d.order) > pageDownloadsKeep {
		drop := d.order[0]
		d.order = d.order[1:]
		delete(d.byGUID, drop.GUID)
	}
	d.notifyLocked()
}

// progress records ev and reports whether the download has gone past the cap
// and must be canceled.
func (d *pageDownloads) progress(ev *browser.EventDownloadProgress) (overCap bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	pd := d.byGUID[ev.GUID]
	if pd == nil || pd.State != PageDownloadInProgress {
		return false
	}
	pd.ReceivedBytes = int64(ev.ReceivedBytes)
	pd.TotalBytes = int64(ev.TotalBytes)
	switch ev.State {
	case browser.DownloadProgressStateCompleted:
		pd.State = PageDownloadCompleted
		pd.FilePath = ev.FilePath
	case browser.DownloadProgressStateCanceled:
		pd.State = PageDownloadCanceled
		if pd.Error == "" {
			pd.Error = "the browser canceled the download"
		}
	default:
		if d.maxBytes > 0 && (pd.ReceivedBytes > d.maxBytes || pd.TotalBytes > d.maxBytes) {
			pd.Error = fmt.Sprintf("the download is larger than instanceDefaults.downloadFileMaxBytes (%d bytes)", d.maxBytes)
			overCap = true
		}
	}
	d.notifyLocked()
	return overCap
}

// mark is the sequence number to pass to since: downloads that begin after it.
func (d *pageDownloads) mark() uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.seq
}

func (d *pageDownloads) snapshot() ([]PageDownload, <-chan struct{}) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]PageDownload, 0, len(d.order))
	for _, pd := range d.order {
		out = append(out, *pd)
	}
	return out, d.changed
}

func (d *pageDownloads) claim(guid string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if pd := d.byGUID[guid]; pd != nil {
		pd.claimed = true
	}
}

// enablePageDownloads runs on every browser start or attach; the setting can
// be changed on a live connection.
func (b *Bridge) enablePageDownloads(browserCtx context.Context) {
	if b == nil || browserCtx == nil || b.Config == nil {
		return
	}
	attached := b.externalAttachMode()
	behavior := b.Config.EffectiveDownloadBehavior(attached)
	if behavior == config.DownloadOff {
		return
	}
	dir, err := b.pageDownloadDir(behavior)
	if err != nil {
		slog.Warn("page downloads: no download directory", "err", err)
		return
	}
	cdpBehavior := browser.SetDownloadBehaviorBehaviorAllowAndName
	if behavior == config.DownloadUser {
		cdpBehavior = browser.SetDownloadBehaviorBehaviorAllow
	}
	if err := runOnBrowser(browserCtx, func(ctx context.Context) error {
		return browser.SetDownloadBehavior(cdpBehavior).WithDownloadPath(dir).WithEventsEnabled(true).Do(ctx)
	}); err != nil {
		slog.Warn("page downloads: setDownloadBehavior failed", "err", err)
		return
	}

	d := newPageDownloads()
	d.maxBytes = int64(b.Config.EffectiveDownloadFileMaxBytes())
	b.downloadsMu.Lock()
	b.pageDownloads = d
	b.downloadsMu.Unlock()

	chromedp.ListenBrowser(browserCtx, func(ev any) {
		switch e := ev.(type) {
		case *browser.EventDownloadWillBegin:
			d.begin(e)
		case *browser.EventDownloadProgress:
			if d.progress(e) {
				guid := e.GUID
				go func() {
					_ = runOnBrowser(browserCtx, func(ctx context.Context) error {
						return browser.CancelDownload(guid).Do(ctx)
					})
				}()
			}
		}
	})
	slog.Info("page downloads enabled", "behavior", behavior, "dir", dir)
}

func (b *Bridge) pageDownloadDir(behavior string) (string, error) {
	if dir := b.Config.DownloadDir; dir != "" {
		return dir, nil
	}
	if behavior == config.DownloadUser {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Downloads"), nil
	}
	if b.Config.StateDir == "" {
		return "", errors.New("no state directory")
	}
	dir := filepath.Join(b.Config.StateDir, "downloads")
	return dir, os.MkdirAll(dir, 0o750)
}

func (b *Bridge) downloadsTracker() *pageDownloads {
	b.downloadsMu.Lock()
	defer b.downloadsMu.Unlock()
	return b.pageDownloads
}

// PageDownloadMark returns a mark for PageDownloadsSince.
func (b *Bridge) PageDownloadMark() uint64 {
	if d := b.downloadsTracker(); d != nil {
		return d.mark()
	}
	return 0
}

// PageDownloadsSince lists the downloads tabID began after mark.
func (b *Bridge) PageDownloadsSince(tabID string, mark uint64) []PageDownload {
	d := b.downloadsTracker()
	if d == nil {
		return nil
	}
	all, _ := d.snapshot()
	var out []PageDownload
	for _, pd := range all {
		if pd.seq > mark && b.downloadOnTab(pd, tabID) {
			pd.TabID = tabID
			out = append(out, pd)
		}
	}
	return out
}

// WaitPageDownload waits until tabID's oldest download not yet collected has
// finished, waiting for one to begin if there is none, and claims it. It
// returns the download as it stands when ctx ends, or ErrNoPageDownload when
// none began.
func (b *Bridge) WaitPageDownload(ctx context.Context, tabID string) (*PageDownload, error) {
	d := b.downloadsTracker()
	if d == nil {
		return nil, errors.New("page downloads are off (instanceDefaults.downloadBehavior)")
	}
	for {
		all, changed := d.snapshot()
		var pending *PageDownload
		for i := range all {
			pd := all[i]
			if pd.claimed || !b.downloadOnTab(pd, tabID) {
				continue
			}
			pd.TabID = tabID
			if pd.State != PageDownloadInProgress || pd.Error != "" {
				d.claim(pd.GUID)
				return &pd, nil
			}
			pending = &pd
			break
		}
		select {
		case <-changed:
		case <-ctx.Done():
			if pending != nil {
				return pending, ctx.Err()
			}
			return nil, ErrNoPageDownload
		}
	}
}

// downloadOnTab reports whether pd began in tabID: in its top frame, whose id
// is the tab's, or in a frame inside it.
func (b *Bridge) downloadOnTab(pd PageDownload, tabID string) bool {
	if pd.frameID == "" || pd.frameID == tabID {
		return true
	}
	tm := b.TabManager
	if tm == nil {
		return false
	}
	ctx, ok := tm.LiveTabContexts()[tabID]
	if !ok {
		return false
	}
	fctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var tree *page.FrameTree
	if err := chromedp.Run(fctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		tree, err = page.GetFrameTree().Do(ctx)
		return err
	})); err != nil {
		return false
	}
	return frameTreeHas(tree, cdp.FrameID(pd.frameID))
}

func frameTreeHas(t *page.FrameTree, id cdp.FrameID) bool {
	if t == nil {
		return false
	}
	if t.Frame != nil && t.Frame.ID == id {
		return true
	}
	for _, c := range t.ChildFrames {
		if frameTreeHas(c, id) {
			return true
		}
	}
	return false
}

// pageDownloadChunk is how much of a remote file one CDP round trip carries,
// before base64.
const pageDownloadChunk = 8 << 20

// CopyPageDownload writes the finished download pd to w. tabCtx is a live tab
// of the same browser, needed only when the file is not on this machine.
func (b *Bridge) CopyPageDownload(tabCtx context.Context, pd *PageDownload, w io.Writer) (int64, error) {
	if pd.State != PageDownloadCompleted || pd.FilePath == "" {
		return 0, fmt.Errorf("download %s is %s, not completed", pd.Filename, pd.State)
	}
	// The bridge shares a disk with the browser it launched, and with Louie's
	// Chrome on his laptop; never with a cloud browser.
	if f, err := os.Open(pd.FilePath); err == nil {
		defer f.Close()
		if st, err := f.Stat(); err == nil && st.Size() == pd.ReceivedBytes {
			return io.Copy(w, f)
		}
	}
	return copyBrowserFile(tabCtx, pd.FilePath, pd.ReceivedBytes, w)
}

// copyBrowserFile reads a file on the browser's machine through a file input
// that never joins the page: DOM.setFileInputFiles takes a path on that
// machine, and File.slice reads it back in chunks. It runs in an isolated
// world, so the page sees nothing.
func copyBrowserFile(tabCtx context.Context, path string, size int64, w io.Writer) (int64, error) {
	var written int64
	err := chromedp.Run(tabCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		execID, err := cdptk.IsolatedContextID(ctx, "")
		if err != nil {
			return fmt.Errorf("isolated world: %w", err)
		}
		input, exc, err := cdpruntime.Evaluate(`(() => { const i = document.createElement('input'); i.type = 'file'; return i })()`).
			WithContextID(cdpruntime.ExecutionContextID(execID)).Do(ctx)
		if err != nil {
			return err
		}
		if exc != nil || input == nil || input.ObjectID == "" {
			return errors.New("could not create a file input")
		}
		defer func() { _ = cdpruntime.ReleaseObject(input.ObjectID).Do(ctx) }()
		if err := dom.SetFileInputFiles([]string{path}).WithObjectID(input.ObjectID).Do(ctx); err != nil {
			return fmt.Errorf("open %s in the browser: %w", path, err)
		}
		for off := int64(0); off < size; off += pageDownloadChunk {
			fn := fmt.Sprintf(`async function() {
  const b = new Uint8Array(await this.files[0].slice(%d, %d).arrayBuffer());
  let s = '';
  for (let i = 0; i < b.length; i += 32768) s += String.fromCharCode.apply(null, b.subarray(i, i + 32768));
  return btoa(s);
}`, off, off+pageDownloadChunk)
			res, exc, err := cdpruntime.CallFunctionOn(fn).WithObjectID(input.ObjectID).
				WithAwaitPromise(true).WithReturnByValue(true).Do(ctx)
			if err != nil {
				return err
			}
			if exc != nil {
				return fmt.Errorf("read %s in the browser: %s", path, exc.Text)
			}
			var b64 string
			if err := json.Unmarshal(res.Value, &b64); err != nil {
				return err
			}
			chunk, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				return err
			}
			if len(chunk) == 0 {
				return fmt.Errorf("read %s in the browser: ended at %d of %d bytes", path, written, size)
			}
			n, err := w.Write(chunk)
			written += int64(n)
			if err != nil {
				return err
			}
		}
		return nil
	}))
	return written, err
}
