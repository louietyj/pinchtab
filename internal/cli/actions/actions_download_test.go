package actions

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// downloadServer answers a URL download as begun, then finished (or still
// under way), then its bytes.
func downloadServer(state string) *mockServer {
	m := newMockServer()
	dl := `{"guid":"g1","filename":"f.bin","state":"` + state + `","receivedBytes":10,"totalBytes":30}`
	m.setResponse("GET", "/download", 200, `{"state":"inProgress","download":`+dl+`}`)
	m.setResponse("GET", "/tabs/tab1/download", 200, `{"state":"inProgress","download":`+dl+`}`)
	m.setResponse("GET", "/download/wait", 200, `{"state":"`+state+`","download":`+dl+`}`)
	m.setResponse("GET", "/download/content", 200, "0123456789")
	return m
}

func TestDownloadCollectsTheFileByItsHandle(t *testing.T) {
	m := downloadServer("completed")
	defer m.close()
	out := filepath.Join(t.TempDir(), "d.bin")

	got := captureStdout(t, func() {
		Download(m.server.Client(), m.base(), "", "https://example.com/f.bin", out, "", 0, false)
	})

	if b, err := os.ReadFile(out); err != nil || string(b) != "0123456789" {
		t.Fatalf("saved %q, %v", b, err)
	}
	if strings.TrimSpace(got) != "Saved "+out+" (10 bytes)" {
		t.Errorf("printed %q", got)
	}
	var paths []string
	for _, r := range m.requests {
		paths = append(paths, r.Path+"?"+r.Query)
	}
	joined := strings.Join(paths, " ")
	for _, want := range []string{"/download?url=", "/download/wait?guid=g1", "/download/content?guid=g1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("requests %v lack %s", paths, want)
		}
	}
}

func TestDownloadInBackgroundPrintsTheHandleAndWaitsForNothing(t *testing.T) {
	m := downloadServer("completed")
	defer m.close()

	got := captureStdout(t, func() {
		Download(m.server.Client(), m.base(), "", "https://example.com/f.bin", "/tmp/x y.bin", "", 0, true)
	})

	if want := "pinchtab wait --download g1 -o '/tmp/x y.bin'"; !strings.Contains(got, want) {
		t.Errorf("printed %q, want the collect command %q", got, want)
	}
	if len(m.requests) != 1 {
		t.Errorf("made %d requests, want only the one that starts the download", len(m.requests))
	}
}

// Past the timeout the command leaves the download running, prints its handle
// and exits 6. os.Exit ends the process, so this runs in a child.
func TestADownloadPastItsTimeoutExitsSixWithItsHandle(t *testing.T) {
	if os.Getenv("PT_DOWNLOAD_CHILD") == "1" {
		m := downloadServer("inProgress")
		defer m.close()
		Download(m.server.Client(), m.base(), "", "https://example.com/f.bin", "", "", 1, false)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestADownloadPastItsTimeoutExitsSixWithItsHandle$")
	cmd.Env = append(os.Environ(), "PT_DOWNLOAD_CHILD=1", "PINCHTAB_NAME=pinchtab-cloud")
	out, err := cmd.Output()
	exit, _ := err.(*exec.ExitError)
	if exit == nil || exit.ExitCode() != 6 {
		t.Fatalf("exit %v, want 6; output %q", err, out)
	}
	for _, want := range []string{"f.bin still downloading", "10 bytes of 30 bytes", "pinchtab-cloud wait --download g1"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output %q lacks %q", out, want)
		}
	}
}

func TestDownloadTimeoutDefaultsTo30sAndStopsAt2m(t *testing.T) {
	for ms, want := range map[int]string{0: "30s", 5000: "5s", 600000: "2m0s"} {
		if got := downloadTimeout(ms).String(); got != want {
			t.Errorf("downloadTimeout(%d) = %s, want %s", ms, got, want)
		}
	}
}

func TestTheSaveConfirmationIsTheSameSentenceEverywhere(t *testing.T) {
	dir := t.TempDir()

	download := func() string {
		m := downloadServer("completed")
		defer m.close()
		return captureStdout(t, func() {
			Download(m.server.Client(), m.base(), "", "https://example.com/f.bin", filepath.Join(dir, "d.bin"), "", 0, false)
		})
	}
	screenshot := func() string {
		m := newMockServer()
		defer m.close()
		m.response = "0123456789"
		cmd := screenshotCmd()
		_ = cmd.Flags().Set("output", filepath.Join(dir, "s.jpg"))
		return captureStdout(t, func() { Screenshot(m.server.Client(), m.base(), "", cmd) })
	}
	pdf := func() string {
		m := newMockServer()
		defer m.close()
		m.response = "0123456789"
		cmd := pdfCmd()
		_ = cmd.Flags().Set("output", filepath.Join(dir, "p.pdf"))
		return captureStdout(t, func() { PDF(m.server.Client(), m.base(), "", cmd) })
	}

	want := "Saved " + filepath.Join(dir, "d.bin") + " (10 bytes)"
	if got := strings.TrimSpace(download()); got != want {
		t.Errorf("download printed %q, want %q", got, want)
	}
	for name, got := range map[string]string{
		"screenshot": strings.TrimSpace(screenshot()),
		"pdf":        strings.TrimSpace(pdf()),
	} {
		if !strings.HasPrefix(got, "Saved ") || !strings.HasSuffix(got, " (10 bytes)") {
			t.Errorf("%s printed %q, which is not the shared sentence", name, got)
		}
	}
}

func TestTheSaveConfirmationHasOneOwner(t *testing.T) {
	const sentence = `"Saved %s (%d bytes)"`

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	var owner string
	var others []string
	var scanned int
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(name) // #nosec G304 -- files listed from this package's own directory.
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		if !strings.Contains(string(body), sentence) {
			continue
		}
		if strings.Contains(string(body), "func printSaved(") {
			owner = name
			continue
		}
		others = append(others, name)
	}

	if scanned < 2 {
		t.Fatalf("scanned %d files; this census would pass vacuously", scanned)
	}
	if owner == "" {
		t.Fatalf("no file declares printSaved alongside %s; the sentence has no owner, so this census guards nothing", sentence)
	}
	if len(others) > 0 {
		t.Errorf("%s is open-coded in %v as well as its owner %s; route those through printSaved", sentence, others, owner)
	}
}

func TestDownloadTabScopedPath(t *testing.T) {
	m := downloadServer("completed")
	defer m.close()

	captureStdout(t, func() {
		Download(m.server.Client(), m.base(), "", "https://example.com/f.txt", filepath.Join(t.TempDir(), "f"), "tab1", 0, false)
	})

	if len(m.requests) == 0 || m.requests[0].Path != "/tabs/tab1/download" {
		t.Fatalf("expected /tabs/tab1/download first, got %+v", m.requests)
	}
}
