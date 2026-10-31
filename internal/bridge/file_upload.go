package bridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/pinchtab/pinchtab/internal/cdptk"
)

// FileChooser is a file picker the page opened during an action. BackendNodeID
// is the <input type=file> behind it, or 0 for showOpenFilePicker, which has
// no input to fill.
type FileChooser struct {
	Mode          string `json:"mode"`
	BackendNodeID int64  `json:"backendNodeId,omitempty"`
}

// WatchFileChooser intercepts file pickers on tabID until the returned stop is
// called, which reports the picker opened meanwhile, if any. Interception is
// kept to the action's span so it never swallows a picker the user opens in a
// tab of their own browser.
func (b *Bridge) WatchFileChooser(tabCtx context.Context, tabID string) func() *FileChooser {
	if err := chromedp.Run(tabCtx, page.SetInterceptFileChooserDialog(true)); err != nil {
		return func() *FileChooser { return nil }
	}
	lctx, cancel := context.WithCancel(tabCtx)
	opened := make(chan FileChooser, 1)
	chromedp.ListenTarget(lctx, func(ev any) {
		if e, ok := ev.(*page.EventFileChooserOpened); ok {
			select {
			case opened <- FileChooser{Mode: string(e.Mode), BackendNodeID: int64(e.BackendNodeID)}:
			default:
			}
		}
	})
	return func() *FileChooser {
		cancel()
		offCtx, offCancel := context.WithTimeout(context.WithoutCancel(tabCtx), 2*time.Second)
		_ = chromedp.Run(offCtx, page.SetInterceptFileChooserDialog(false))
		offCancel()
		select {
		case fc := <-opened:
			b.choosersMu.Lock()
			if b.choosers == nil {
				b.choosers = map[string]FileChooser{}
			}
			b.choosers[tabID] = fc
			b.choosersMu.Unlock()
			return &fc
		default:
			return nil
		}
	}
}

// TakeFileChooser returns and forgets the picker last opened on tabID.
func (b *Bridge) TakeFileChooser(tabID string) *FileChooser {
	b.choosersMu.Lock()
	defer b.choosersMu.Unlock()
	fc, ok := b.choosers[tabID]
	if !ok {
		return nil
	}
	delete(b.choosers, tabID)
	return &fc
}

// browserRefusesPath matches Browser Use's guard on DOM.setFileInputFiles,
// which only takes files already in its downloads directory.
func browserRefusesPath(err error) bool {
	return err != nil && strings.Contains(err.Error(), "must exist in the downloads directory")
}

// setFilesViaDownloads gets local files onto a browser that will not read our
// disk: the page downloads each one into the browser's downloads directory,
// and the input is given those copies, so the page still sees real files.
func (b *Bridge) setFilesViaDownloads(ctx context.Context, backendNodeID int64, paths []string) error {
	d := b.downloadsTracker()
	if d == nil {
		return errors.New("the browser takes uploads only through its downloads, and page downloads are off (instanceDefaults.downloadBehavior)")
	}
	mark := d.mark()
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		files, err := pushFilesToPage(ctx, paths)
		if err != nil {
			return err
		}
		input, err := IsolatedNodeObjectID(ctx, backendNodeID)
		if err != nil {
			return err
		}
		_, exc, err := cdpruntime.CallFunctionOn(`function(files) {
  const doc = this.ownerDocument || document;
  for (const f of files) {
    const a = doc.createElement('a');
    a.href = URL.createObjectURL(f);
    a.download = f.name;
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 60000);
  }
}`).WithObjectID(cdpruntime.RemoteObjectID(input)).
			WithArguments([]*cdpruntime.CallArgument{{ObjectID: files}}).Do(ctx)
		if exc != nil {
			return fmt.Errorf("start the downloads: %s", exc.Text)
		}
		return err
	})); err != nil {
		return err
	}

	landed := make([]string, 0, len(paths))
	for _, p := range paths {
		pd, err := d.waitFinished(ctx, mark, filepath.Base(p))
		if err != nil {
			return fmt.Errorf("copy %s onto the browser: %w", filepath.Base(p), err)
		}
		landed = append(landed, pd.FilePath)
	}
	return chromedp.Run(ctx, dom.SetFileInputFiles(landed).WithBackendNodeID(cdp.BackendNodeID(backendNodeID)))
}

// DropFiles drops local files onto the element as a drop zone sees them: a
// dragenter, dragover and drop carrying a DataTransfer of those files.
func (b *Bridge) DropFiles(ctx context.Context, backendNodeID int64, paths []string) error {
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		files, err := pushFilesToPage(ctx, paths)
		if err != nil {
			return err
		}
		target, err := IsolatedNodeObjectID(ctx, backendNodeID)
		if err != nil {
			return err
		}
		_, exc, err := cdpruntime.CallFunctionOn(`function(files) {
  const dt = new DataTransfer();
  for (const f of files) dt.items.add(f);
  const r = this.getBoundingClientRect();
  const at = {clientX: r.left + r.width / 2, clientY: r.top + r.height / 2};
  for (const type of ['dragenter', 'dragover', 'drop']) {
    this.dispatchEvent(new DragEvent(type, {bubbles: true, cancelable: true, composed: true, dataTransfer: dt, ...at}));
  }
}`).WithObjectID(cdpruntime.RemoteObjectID(target)).
			WithArguments([]*cdpruntime.CallArgument{{ObjectID: files}}).Do(ctx)
		if exc != nil {
			return fmt.Errorf("drop: %s", exc.Text)
		}
		return err
	}))
}

// pushChunk is how much of a file one CDP call carries into the page, before
// base64.
const pushChunk = 4 << 20

// pushFilesToPage builds File objects from local files in the tab's isolated
// world, out of the page's reach, and returns the array holding them.
func pushFilesToPage(ctx context.Context, paths []string) (cdpruntime.RemoteObjectID, error) {
	execID, err := cdptk.IsolatedContextID(ctx, "")
	if err != nil {
		return "", fmt.Errorf("isolated world: %w", err)
	}
	eval := func(expr string) (*cdpruntime.RemoteObject, error) {
		res, exc, err := cdpruntime.Evaluate(expr).WithContextID(cdpruntime.ExecutionContextID(execID)).Do(ctx)
		if err != nil {
			return nil, err
		}
		if exc != nil {
			return nil, fmt.Errorf("build files in the page: %s", exc.Text)
		}
		return res, nil
	}
	if _, err := eval(`globalThis.__ptFiles = []; 0`); err != nil {
		return "", err
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		if _, err := eval(`globalThis.__ptBuf = []; 0`); err != nil {
			return "", err
		}
		for off := 0; off < len(data); off += pushChunk {
			b64, _ := json.Marshal(base64.StdEncoding.EncodeToString(data[off:min(off+pushChunk, len(data))]))
			if _, err := eval(fmt.Sprintf(`__ptBuf.push(Uint8Array.from(atob(%s), c => c.charCodeAt(0))); 0`, b64)); err != nil {
				return "", err
			}
		}
		name, _ := json.Marshal(filepath.Base(p))
		typ, _ := json.Marshal(mime.TypeByExtension(filepath.Ext(p)))
		if _, err := eval(fmt.Sprintf(`__ptFiles.push(new File(__ptBuf, %s, {type: %s})); __ptBuf = null; 0`, name, typ)); err != nil {
			return "", err
		}
	}
	res, err := eval(`__ptFiles`)
	if err != nil {
		return "", err
	}
	return res.ObjectID, nil
}
