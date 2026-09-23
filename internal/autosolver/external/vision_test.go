package external

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Vision Engine answered 3 bytes of garbage with "angle 0" and billed for it.
func TestSolveVisionRefusesUnusableImagesWithoutCallingTheAPI(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer srv.Close()

	for name, task := range map[string]VisionTask{
		"garbage":            {Module: "rotate_2", Image: []byte("AAAA")},
		"tiny crop":          {Module: "rotate_2", Image: pngOf(t, 4, 4)},
		"missing background": {Module: "slider_1", Image: pngOf(t, 60, 60)},
		"missing question":   {Module: "shein", Image: pngOf(t, 60, 60)},
		"unknown module":     {Module: "slider_9", Image: pngOf(t, 60, 60)},
	} {
		if _, err := SolveVision(context.Background(), "k", srv.URL, task); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if called {
		t.Error("an unusable task reached the API and would have been billed")
	}
}

func TestSolveVisionReturnsTheSynchronousAnswer(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Task map[string]any `json:"task"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		got = req.Task
		_, _ = w.Write([]byte(`{"errorId":0,"status":"ready","taskId":"t","solution":{"distance":142}}`))
	}))
	defer srv.Close()

	sol, err := SolveVision(context.Background(), "k", srv.URL, VisionTask{Module: "slider_1", Image: pngOf(t, 50, 50), Background: pngOf(t, 300, 150)})
	if err != nil {
		t.Fatal(err)
	}
	if sol.Distance == nil || *sol.Distance != 142 {
		t.Errorf("distance = %v", sol.Distance)
	}
	if got["type"] != "VisionEngine" || got["module"] != "slider_1" || got["imageBackground"] == nil {
		t.Errorf("task = %v", got)
	}
	if s, _ := got["image"].(string); strings.HasPrefix(s, "data:") {
		t.Error("image sent with a data: prefix; the engine wants raw base64")
	}
}

func TestImageSizeReadsWebP(t *testing.T) {
	// VP8X header for a 320x160 canvas.
	b := make([]byte, 30)
	copy(b, "RIFF")
	copy(b[8:], "WEBPVP8X")
	b[24], b[27] = 319&0xff, 159
	b[25] = 319 >> 8
	if w, h, err := ImageSize(b); err != nil || w != 320 || h != 160 {
		t.Errorf("ImageSize(webp) = %d,%d,%v", w, h, err)
	}
}

func TestParseVisionRectsInBothShapes(t *testing.T) {
	for _, raw := range []string{
		`{"rects":[{"x1":1,"y1":2,"x2":3,"y2":4}]}`,
		`{"rects":[[1,2,3,4]]}`,
	} {
		sol, err := parseVisionSolution(json.RawMessage(raw))
		if err != nil || len(sol.Rects) != 1 || sol.Rects[0] != [4]float64{1, 2, 3, 4} {
			t.Errorf("%s: rects=%v err=%v", raw, sol, err)
		}
	}
}
