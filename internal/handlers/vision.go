package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/pinchtab/pinchtab/internal/activity"
	"github.com/pinchtab/pinchtab/internal/autosolver/external"
	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/httpx"
)

// visionRequest names the puzzle's elements by unified selector. Image and
// Background are read from the page; Handle (and Track, for rotate) are what
// the answer is acted on through.
type visionRequest struct {
	TabID      string `json:"tabId"`
	Module     string `json:"module"`
	Image      string `json:"image"`
	Background string `json:"background"`
	Question   string `json:"question"`
	Handle     string `json:"handle"`
	Track      string `json:"track"`
	// Ratio scales a slider drag when the handle and the piece move at
	// different rates; 0 means 1.
	Ratio float64 `json:"ratio"`
	// Click taps each shein match.
	Click bool `json:"click"`
}

// HandleVision solves a visual puzzle on the page with CapSolver's Vision
// Engine and, given the controls, acts on the answer: drags a slider or rotate
// handle with a humanized path, or clicks the matched regions.
//
// @Endpoint POST /vision
func (h *Handlers) HandleVision(w http.ResponseWriter, r *http.Request) {
	var req visionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodySize)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		httpx.Error(w, 400, fmt.Errorf("decode: %w", err))
		return
	}
	if _, ok := external.VisionModules[req.Module]; !ok {
		httpx.Error(w, 400, fmt.Errorf("module must be one of slider_1, rotate_1, rotate_2, shein, ocr_gif"))
		return
	}
	if req.Image == "" {
		httpx.Error(w, 400, fmt.Errorf("image selector required"))
		return
	}
	ctx, tabID, err := h.tabContext(r, req.TabID)
	if err != nil {
		WriteTabContextError(w, err, 404)
		return
	}
	if err := h.enforceTabLease(tabID, resolveOwner(r, "")); err != nil {
		httpx.ErrorCode(w, 423, "tab_locked", err.Error(), false, nil)
		return
	}
	// No handoff-pause guard: vision is what an agent reaches for after an
	// auto-solve failed and parked the tab.
	if _, ok := h.applyTabGuards(w, r, ctx, tabID, guardDomainPolicy); !ok {
		return
	}
	if h.Config == nil || h.Config.AutoSolver.CapsolverKey == "" {
		httpx.ErrorCode(w, 400, "solver_key_missing", "vision needs a CapSolver key (autoSolver.external.capsolverKey)", false, nil)
		return
	}
	h.recordActivity(r, activity.Update{Action: "vision:" + req.Module, TabID: tabID})

	out, status, err := h.runVision(ctx, tabID, req)
	if err != nil {
		httpx.Error(w, status, err)
		return
	}
	httpx.JSON(w, 200, out)
}

// @Endpoint POST /tabs/{id}/vision
func (h *Handlers) HandleTabVision(w http.ResponseWriter, r *http.Request) {
	h.withPathTabIDBody(w, r, h.HandleVision)
}

func (h *Handlers) runVision(ctx context.Context, tabID string, req visionRequest) (map[string]any, int, error) {
	image, err := h.visionImage(ctx, tabID, req.Image, req.Module != "slider_1")
	if err != nil {
		return nil, 400, fmt.Errorf("image %s: %w", req.Image, err)
	}
	task := external.VisionTask{Module: req.Module, Image: image.bytes, Question: req.Question}
	var background *visionElement
	if req.Background != "" {
		if background, err = h.visionImage(ctx, tabID, req.Background, req.Module != "slider_1"); err != nil {
			return nil, 400, fmt.Errorf("background %s: %w", req.Background, err)
		}
		task.Background = background.bytes
	}
	var pageURL string
	if err := h.Bridge.Evaluate(ctx, "location.href", &pageURL, bridge.EvalOpts{}); err == nil {
		task.WebsiteURL = pageURL
	}

	sol, err := external.SolveVision(ctx, h.Config.AutoSolver.CapsolverKey, "", task)
	if err != nil {
		return nil, 502, err
	}
	out := map[string]any{"module": req.Module, "solution": sol, "imageSource": image.source}

	switch {
	case req.Module == "slider_1" && req.Handle != "":
		if sol.Distance == nil || background == nil {
			return nil, 502, fmt.Errorf("slider answer carried no distance: %s", sol.Raw)
		}
		drag, err := h.visionSlide(ctx, tabID, req, image, background, *sol.Distance)
		if err != nil {
			return nil, 500, err
		}
		out["drag"] = drag
	case strings.HasPrefix(req.Module, "rotate_") && req.Handle != "":
		if sol.Angle == nil || req.Track == "" {
			return nil, 400, fmt.Errorf("rotate needs an angle answer and a track selector to act on")
		}
		drag, err := h.visionRotate(ctx, tabID, req, *sol.Angle)
		if err != nil {
			return nil, 500, err
		}
		out["drag"] = drag
	case req.Module == "shein" && req.Click:
		clicked, err := h.visionClickRects(ctx, image, sol.Rects)
		if err != nil {
			return nil, 500, err
		}
		out["clicked"] = clicked
	}
	return out, 200, nil
}

// visionElement is a puzzle image as the page shows it: its bytes, their pixel
// size, and the CSS box they are drawn into.
type visionElement struct {
	bytes              []byte
	naturalW, naturalH int
	box                *boundingBox
	// source is where the bytes came from: data-url, cache, or screenshot.
	source string
}

// visionImageInfoJS reports where an element's image comes from: an <img>
// source, a canvas's pixels, or a CSS background-image URL.
const visionImageInfoJS = `function() {
  var el = this, r = {};
  if (el.tagName === 'IMG') { r.src = el.currentSrc || el.src; }
  else if (el.tagName === 'CANVAS') {
    try {
      r.dataURL = el.toDataURL('image/png');
      // A slider piece is often drawn into a transparent canvas the size of the
      // whole puzzle (GeeTest v3). The engine wants the piece alone: crop to
      // the opaque pixels when they cover well under half the canvas.
      var w = el.width, h = el.height, d = el.getContext('2d').getImageData(0, 0, w, h).data;
      var x0 = w, y0 = h, x1 = -1, y1 = -1;
      for (var y = 0; y < h; y++) for (var x = 0; x < w; x++) {
        if (d[(y * w + x) * 4 + 3] > 16) { if (x < x0) x0 = x; if (x > x1) x1 = x; if (y < y0) y0 = y; if (y > y1) y1 = y; }
      }
      if (x1 >= x0 && (x1 - x0 + 1) * (y1 - y0 + 1) < 0.4 * w * h) {
        var c = document.createElement('canvas'); c.width = x1 - x0 + 1; c.height = y1 - y0 + 1;
        c.getContext('2d').drawImage(el, x0, y0, c.width, c.height, 0, 0, c.width, c.height);
        r.dataURL = c.toDataURL('image/png'); r.cropped = true;
      }
    } catch (e) { r.tainted = true; }
  }
  else {
    var m = (getComputedStyle(el).backgroundImage || '').match(/url\(["']?(.*?)["']?\)/);
    if (m) { r.src = new URL(m[1], document.baseURI).href; }
  }
  if (r.src && r.src.indexOf('data:') === 0) { r.dataURL = r.src; r.src = ''; }
  return r;
}`

// visionImage reads an element's image bytes: a data: URL or untainted canvas
// in the page, otherwise the loaded resource from Chrome's cache. A screenshot
// of the element is the last resort, and only where allowShot: a slider's
// piece sits over its background, so a crop of either holds both and the
// engine would answer for the wrong image.
func (h *Handlers) visionImage(ctx context.Context, tabID, sel string, allowShot bool) (*visionElement, error) {
	nodeID, err := h.resolveElementNodeID(ctx, tabID, sel)
	if err != nil {
		return nil, err
	}
	box, err := h.getElementBox(ctx, tabID, sel)
	if err != nil {
		return nil, err
	}
	el := &visionElement{box: box}

	var info struct {
		Src     string `json:"src"`
		DataURL string `json:"dataURL"`
	}
	if err := h.Bridge.CallFunctionOnNode(ctx, nodeID, visionImageInfoJS, nil, &info); err != nil {
		return nil, fmt.Errorf("read image source: %w", err)
	}
	switch {
	case info.DataURL != "":
		if _, data, ok := strings.Cut(info.DataURL, ","); ok {
			el.bytes, err = base64.StdEncoding.DecodeString(data)
			el.source = "data-url"
		}
	case info.Src != "":
		el.bytes, err = bridge.ResourceContent(ctx, info.Src)
		el.source = "cache"
	}
	if len(el.bytes) == 0 || err != nil {
		if !allowShot {
			return nil, fmt.Errorf("could not read its image (tainted canvas or uncached source) and a screenshot would include the piece over it")
		}
		clip, clipErr := bridge.ScreenshotClipForNode(ctx, nodeID)
		if clipErr != nil {
			return nil, fmt.Errorf("screenshot fallback: %w", clipErr)
		}
		shot := bridge.ScreenshotOpts{Format: bridge.ScreenshotFormatPng, Clip: clip, DisableActivation: !h.Config.CaptureAllowActivation}
		if el.bytes, err = bridge.CaptureScreenshot(ctx, shot); err != nil {
			return nil, fmt.Errorf("screenshot fallback: %w", err)
		}
		el.source = "screenshot"
	}
	if el.naturalW, el.naturalH, err = external.ImageSize(el.bytes); err != nil {
		return nil, err
	}
	return el, nil
}

// visionSlide drags the handle so the piece lands where the engine placed the
// gap. The answer is how far the piece travels, in background-image pixels,
// measured from where it starts: on DingXiang, whose piece starts 20px in,
// subtracting that start again left every drag short.
func (h *Handlers) visionSlide(ctx context.Context, tabID string, req visionRequest, piece, bg *visionElement, distance float64) (map[string]any, error) {
	handle, err := h.getElementBox(ctx, tabID, req.Handle)
	if err != nil {
		return nil, fmt.Errorf("handle %s: %w", req.Handle, err)
	}
	scale := bg.box.Width / float64(bg.naturalW)
	target := distance * scale
	ratio := req.Ratio
	if ratio == 0 {
		ratio = 1
	}
	hx, hy := handle.Left+handle.Width/2, handle.Top+handle.Height/2
	if err := bridge.HumanDragBetweenPoints(ctx, hx, hy, hx+target*ratio, hy, ""); err != nil {
		return nil, fmt.Errorf("drag: %w", err)
	}
	moved := 0.0
	if after, err := h.getElementBox(ctx, tabID, req.Image); err == nil {
		moved = after.Left - piece.box.Left
	}
	return map[string]any{"scale": scale, "targetPx": target, "handleMovedPx": target * ratio, "pieceMovedPx": moved}, nil
}

// visionRotate drags a rotate control across its track in proportion to the
// angle the engine read.
func (h *Handlers) visionRotate(ctx context.Context, tabID string, req visionRequest, angle float64) (map[string]any, error) {
	handle, err := h.getElementBox(ctx, tabID, req.Handle)
	if err != nil {
		return nil, fmt.Errorf("handle %s: %w", req.Handle, err)
	}
	track, err := h.getElementBox(ctx, tabID, req.Track)
	if err != nil {
		return nil, fmt.Errorf("track %s: %w", req.Track, err)
	}
	travel := (track.Width - handle.Width) * angle / 360
	hx, hy := handle.Left+handle.Width/2, handle.Top+handle.Height/2
	if err := bridge.HumanDragBetweenPoints(ctx, hx, hy, hx+travel, hy, ""); err != nil {
		return nil, fmt.Errorf("drag: %w", err)
	}
	return map[string]any{"angle": angle, "travelPx": travel}, nil
}

// visionClickRects clicks the centre of each matched region, scaled from image
// pixels to the image's CSS box.
func (h *Handlers) visionClickRects(ctx context.Context, img *visionElement, rects [][4]float64) (int, error) {
	sx := img.box.Width / float64(img.naturalW)
	sy := img.box.Height / float64(img.naturalH)
	for i, r := range rects {
		x := img.box.Left + (r[0]+r[2])/2*sx
		y := img.box.Top + (r[1]+r[3])/2*sy
		if err := bridge.ClickByCoordinate(ctx, x, y, 0); err != nil {
			return i, fmt.Errorf("click %d: %w", i, err)
		}
	}
	return len(rects), nil
}
