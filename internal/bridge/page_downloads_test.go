package bridge

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/cdproto/browser"
)

// A staged download moved to a folder that already holds its name takes the
// next "name (n).ext" Chrome would give it, and never replaces the old file.
func TestMoveUniqueRenamesInsteadOfOverwriting(t *testing.T) {
	stage, dest := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, "report.pdf"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"report (1).pdf", "report (2).pdf"} {
		src := filepath.Join(stage, "guid-"+want)
		if err := os.WriteFile(src, []byte{byte('a' + i)}, 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := moveUnique(src, dest, "report.pdf")
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(got) != want {
			t.Errorf("moved to %s, want %s", filepath.Base(got), want)
		}
		if _, err := os.Stat(src); !os.IsNotExist(err) {
			t.Errorf("staged file %s still there", src)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "report.pdf")); string(b) != "old" {
		t.Errorf("the existing report.pdf was overwritten: %q", b)
	}
}

// A `download <url>` is collected by its guid alone: `wait --download` on a
// tab never takes it, it stays out of ~/Downloads on a personal browser, and
// its file is deleted once copied back.
func TestAURLDownloadStaysPinchtabsOwn(t *testing.T) {
	stage, home := t.TempDir(), t.TempDir()
	d := newPageDownloads()
	d.stageDir, d.moveTo = stage, home

	d.expect("frame-url")
	d.begin(&browser.EventDownloadWillBegin{GUID: "g-url", FrameID: "frame-url", SuggestedFilename: "a.zip"})
	d.unexpect("frame-url")
	d.begin(&browser.EventDownloadWillBegin{GUID: "g-page", FrameID: "frame-page", SuggestedFilename: "b.zip"})

	staged := filepath.Join(stage, "g-url")
	if err := os.WriteFile(staged, []byte("zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	d.progress(&browser.EventDownloadProgress{GUID: "g-url", State: browser.DownloadProgressStateCompleted, ReceivedBytes: 3, FilePath: staged})

	b := &Bridge{pageDownloads: d}
	pd, ok := b.FindPageDownload("g-url")
	if !ok || pd.FilePath != staged {
		t.Fatalf("the URL download is at %+v, want it left at %s, not moved to ~/Downloads", pd, staged)
	}
	if got, _ := d.waitBegun(context.Background(), "frame-url"); got == nil || got.GUID != "g-url" {
		t.Fatalf("waitBegun found %+v, want g-url", got)
	}
	if all, _ := d.snapshot(); !all[0].claimed || all[1].claimed {
		t.Fatalf("claimed = %v/%v, want the URL download claimed and the page's open", all[0].claimed, all[1].claimed)
	}

	var buf bytes.Buffer
	if _, err := b.CopyPageDownload(nil, pd, &buf); err != nil || buf.String() != "zip" {
		t.Fatalf("copy = %q, %v", buf.String(), err)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Error("the URL download's file is still there after it was copied back")
	}
}

// Waiting on a download by guid ends with it as it stands, so a slow one comes
// back in progress for the caller to collect later.
func TestAwaitPageDownloadReturnsADownloadStillUnderWay(t *testing.T) {
	d := newPageDownloads()
	d.begin(&browser.EventDownloadWillBegin{GUID: "g", SuggestedFilename: "big.zip"})
	b := &Bridge{pageDownloads: d}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	pd, err := b.AwaitPageDownload(ctx, "g")
	if err != nil || pd.State != PageDownloadInProgress {
		t.Fatalf("got %+v, %v; want the download in progress", pd, err)
	}
	if _, err := b.AwaitPageDownload(context.Background(), "nope"); !errors.Is(err, ErrUnknownPageDownload) {
		t.Fatalf("unknown guid: %v, want ErrUnknownPageDownload", err)
	}

	go d.progress(&browser.EventDownloadProgress{GUID: "g", State: browser.DownloadProgressStateCompleted, ReceivedBytes: 9})
	pd, err = b.AwaitPageDownload(context.Background(), "g")
	if err != nil || pd.State != PageDownloadCompleted {
		t.Fatalf("got %+v, %v; want it completed", pd, err)
	}
}
