package external

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif" // registers GIF for DecodeConfig: ocr_gif takes animated GIFs
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"time"
)

// VisionModules are the CapSolver Vision Engine models, each naming the inputs
// it needs.
var VisionModules = map[string]struct{ Background, Question bool }{
	"slider_1": {Background: true},
	"rotate_1": {Background: true},
	"rotate_2": {},
	"shein":    {Question: true},
	"ocr_gif":  {},
}

// VisionTask is one Vision Engine request. Images are raw encoded bytes.
type VisionTask struct {
	Module     string
	Image      []byte
	Background []byte
	Question   string
	WebsiteURL string
}

// VisionSolution is the answer; which field is set depends on the module.
type VisionSolution struct {
	Distance *float64 `json:"distance,omitempty"`
	Angle    *float64 `json:"angle,omitempty"`
	Text     string   `json:"text,omitempty"`
	// Rects are shein's matches as [x1, y1, x2, y2] in image pixels.
	Rects [][4]float64    `json:"rects,omitempty"`
	Raw   json.RawMessage `json:"raw"`
}

// minVisionImageSide rejects crops too small to be the puzzle: Vision Engine
// answers even a 3-byte image with a confident angle of 0, and bills for it.
const minVisionImageSide = 16

// ImageSize reports an image's pixel dimensions, or an error when the bytes are
// not an image Vision Engine can read (PNG, JPEG, GIF, WebP).
func ImageSize(b []byte) (width, height int, err error) {
	if w, h, ok := webpSize(b); ok {
		return w, h, nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return 0, 0, fmt.Errorf("not a PNG, JPEG, GIF or WebP image (%d bytes): %w", len(b), err)
	}
	return cfg.Width, cfg.Height, nil
}

// webpSize reads a WebP header by hand: the standard library has no decoder.
func webpSize(b []byte) (int, int, bool) {
	if len(b) < 30 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return 0, 0, false
	}
	switch string(b[12:16]) {
	case "VP8 ":
		return int(binary.LittleEndian.Uint16(b[26:28]) & 0x3fff), int(binary.LittleEndian.Uint16(b[28:30]) & 0x3fff), true
	case "VP8L":
		bits := binary.LittleEndian.Uint32(b[21:25])
		return int(bits&0x3fff) + 1, int((bits>>14)&0x3fff) + 1, true
	case "VP8X":
		w := int(b[24]) | int(b[25])<<8 | int(b[26])<<16
		h := int(b[27]) | int(b[28])<<8 | int(b[29])<<16
		return w + 1, h + 1, true
	}
	return 0, 0, false
}

func checkVisionImage(name string, b []byte) error {
	w, h, err := ImageSize(b)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if w < minVisionImageSide || h < minVisionImageSide {
		return fmt.Errorf("%s is %dx%d; too small to be the puzzle", name, w, h)
	}
	return nil
}

// SolveVision runs one Vision Engine task. Every input is checked first, since
// the engine validates nothing and bills a nonsense answer to a bad crop.
func SolveVision(ctx context.Context, apiKey, baseURL string, task VisionTask) (*VisionSolution, error) {
	needs, ok := VisionModules[task.Module]
	if !ok {
		return nil, fmt.Errorf("unknown vision module %q", task.Module)
	}
	if apiKey == "" {
		return nil, fmt.Errorf("capsolver API key not configured")
	}
	if err := checkVisionImage("image", task.Image); err != nil {
		return nil, err
	}
	if needs.Background {
		if err := checkVisionImage("background", task.Background); err != nil {
			return nil, err
		}
	}
	if needs.Question && task.Question == "" {
		return nil, fmt.Errorf("module %s needs a question", task.Module)
	}

	body := map[string]any{
		"type":   "VisionEngine",
		"module": task.Module,
		"image":  base64.StdEncoding.EncodeToString(task.Image),
	}
	if needs.Background {
		body["imageBackground"] = base64.StdEncoding.EncodeToString(task.Background)
	}
	if task.Question != "" {
		body["question"] = task.Question
	}
	if task.WebsiteURL != "" {
		body["websiteURL"] = task.WebsiteURL
	}
	if baseURL == "" {
		baseURL = "https://api.capsolver.com"
	}
	api := &taskAPI{label: "capsolver", baseURL: baseURL, apiKey: apiKey, pollInterval: time.Second, client: &http.Client{Timeout: 60 * time.Second}}
	raw, err := api.solve(ctx, body)
	if err != nil {
		return nil, err
	}
	return parseVisionSolution(raw)
}

func parseVisionSolution(raw json.RawMessage) (*VisionSolution, error) {
	var loose struct {
		Distance *float64        `json:"distance"`
		Angle    *float64        `json:"angle"`
		Text     string          `json:"text"`
		Rects    json.RawMessage `json:"rects"`
	}
	if err := json.Unmarshal(raw, &loose); err != nil {
		return nil, fmt.Errorf("decode vision solution: %w", err)
	}
	sol := &VisionSolution{Distance: loose.Distance, Angle: loose.Angle, Text: loose.Text, Raw: raw}
	if len(loose.Rects) > 0 {
		// Documented as [{x1,y1,x2,y2}]; accept bare arrays too.
		var objs []struct{ X1, Y1, X2, Y2 float64 }
		if json.Unmarshal(loose.Rects, &objs) == nil {
			for _, r := range objs {
				sol.Rects = append(sol.Rects, [4]float64{r.X1, r.Y1, r.X2, r.Y2})
			}
		} else if err := json.Unmarshal(loose.Rects, &sol.Rects); err != nil {
			return nil, fmt.Errorf("decode vision rects %s: %w", loose.Rects, err)
		}
	}
	return sol, nil
}
