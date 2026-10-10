package actions

import (
	"bytes"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestScreenshot(t *testing.T) {
	m := newMockServer()
	m.response = "FAKEJPEGDATA"
	defer m.close()
	client := m.server.Client()

	outFile := filepath.Join(t.TempDir(), "test.jpg")
	cmd := &cobra.Command{}
	cmd.Flags().String("output", outFile, "")
	cmd.Flags().String("quality", "50", "")
	cmd.Flags().String("selector", "#target", "")
	cmd.Flags().String("scale", "0.5", "")
	cmd.Flags().String("tab", "", "")
	Screenshot(client, m.base(), "", cmd)
	if m.lastPath != "/screenshot" {
		t.Errorf("expected /screenshot, got %s", m.lastPath)
	}
	if !strings.Contains(m.lastQuery, "quality=50") {
		t.Errorf("expected quality=50, got %s", m.lastQuery)
	}
	if !strings.Contains(m.lastQuery, "selector=%23target") {
		t.Errorf("expected selector query, got %s", m.lastQuery)
	}
	if !strings.Contains(m.lastQuery, "scale=0.5") {
		t.Errorf("expected scale=0.5, got %s", m.lastQuery)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("file not written: %v", err)
	}
	if string(data) != "FAKEJPEGDATA" {
		t.Errorf("unexpected content: %s", string(data))
	}
}

// A 2000x1000 capture of a 1000x500 CSS viewport (devicePixelRatio 2): the
// printed scale is to CSS pixels, not device pixels.
func TestScreenshotFitsMaxPixelsAndReportsCSSScale(t *testing.T) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2000, 1000)), nil); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-PinchTab-Viewport", "1000x500")
		_, _ = w.Write(buf.Bytes())
	}))
	defer srv.Close()

	outFile := filepath.Join(t.TempDir(), "shot.jpg")
	cmd := &cobra.Command{}
	cmd.Flags().String("output", outFile, "")
	cmd.Flags().Int("max-pixels", 500_000, "")
	cmd.Flags().Int("also-max-pixels", 125_000, "")
	out := captureStdout(t, func() { Screenshot(srv.Client(), srv.URL, "", cmd) })

	for path, want := range map[string][2]int{outFile: {1000, 500}, strings.TrimSuffix(outFile, ".jpg") + "-small.jpg": {500, 250}} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s not written: %v", path, err)
		}
		if w, h := imageSize(data); w != want[0] || h != want[1] {
			t.Errorf("%s is %dx%d, want %dx%d", path, w, h, want[0], want[1])
		}
	}
	for _, want := range []string{"1000x500, scale 1 ", "500x250, scale 0.5 "} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestScreenshotWithinMaxPixelsIsUntouched(t *testing.T) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 800, 600)), nil); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(buf.Bytes())
	}))
	defer srv.Close()

	outFile := filepath.Join(t.TempDir(), "shot.jpg")
	cmd := &cobra.Command{}
	cmd.Flags().String("output", outFile, "")
	cmd.Flags().Int("max-pixels", 1_000_000, "")
	out := captureStdout(t, func() { Screenshot(srv.Client(), srv.URL, "", cmd) })

	data, _ := os.ReadFile(outFile)
	if !bytes.Equal(data, buf.Bytes()) {
		t.Error("an image within the budget was re-encoded")
	}
	if !strings.Contains(out, "Image "+outFile+" 800x600\n") {
		t.Errorf("without a viewport header the line should carry no scale:\n%s", out)
	}
}

func TestScreenshotInfersPNGFromOutputExtension(t *testing.T) {
	m := newMockServer()
	m.response = "FAKEPNGDATA"
	defer m.close()

	outFile := filepath.Join(t.TempDir(), "screenshot.png")
	cmd := &cobra.Command{}
	cmd.Flags().String("output", outFile, "")
	cmd.Flags().String("format", "", "")
	Screenshot(m.server.Client(), m.base(), "", cmd)

	if !strings.Contains(m.lastQuery, "format=png") {
		t.Fatalf("query = %q, want format=png inferred from output extension", m.lastQuery)
	}
}
