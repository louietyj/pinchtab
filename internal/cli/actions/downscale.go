package actions

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/pinchtab/pinchtab/internal/api/types"
)

// fitFactor returns the factor that brings an encoded image within maxPixels,
// or 1 when it already fits or can't be decoded.
func fitFactor(data []byte, maxPixels int) float64 {
	w, h := imageSize(data)
	if w == 0 || h == 0 || w*h <= maxPixels {
		return 1
	}
	return math.Sqrt(float64(maxPixels) / float64(w*h))
}

func imageSize(data []byte) (int, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// viewportCSSWidth reads the server's X-PinchTab-Viewport header ("<w>x<h>",
// CSS pixels); 0 when absent.
func viewportCSSWidth(h http.Header) float64 {
	w, _, _ := strings.Cut(h.Get(types.HeaderViewport), "x")
	v, err := strconv.ParseFloat(w, 64)
	if err != nil {
		return 0
	}
	return v
}

// printImageScale prints a saved image's size and, when the CSS viewport is
// known, its image pixels per CSS pixel.
func printImageScale(path string, data []byte, cssWidth float64) {
	w, h := imageSize(data)
	if cssWidth > 0 && w > 0 {
		fmt.Printf("Image %s %dx%d, scale %.4g (css px = image px / scale)\n", path, w, h, float64(w)/cssWidth)
		return
	}
	fmt.Printf("Image %s %dx%d\n", path, w, h)
}

// downscaleImage shrinks an encoded JPEG or PNG by factor (0-1), keeping its
// format. Each output pixel averages the source pixels it covers, so text stays
// legible where nearest-neighbour sampling would drop strokes.
func downscaleImage(data []byte, factor float64) ([]byte, error) {
	src, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), src, b.Min, draw.Src)

	w := max(1, int(float64(b.Dx())*factor))
	h := max(1, int(float64(b.Dy())*factor))
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		y0, y1 := y*b.Dy()/h, max((y+1)*b.Dy()/h, y*b.Dy()/h+1)
		for x := 0; x < w; x++ {
			x0, x1 := x*b.Dx()/w, max((x+1)*b.Dx()/w, x*b.Dx()/w+1)
			var sum [4]int
			for sy := y0; sy < y1; sy++ {
				row := rgba.Pix[sy*rgba.Stride:]
				for sx := x0; sx < x1; sx++ {
					for c := 0; c < 4; c++ {
						sum[c] += int(row[sx*4+c])
					}
				}
			}
			n := (y1 - y0) * (x1 - x0)
			o := dst.PixOffset(x, y)
			for c := 0; c < 4; c++ {
				dst.Pix[o+c] = uint8(sum[c] / n)
			}
		}
	}

	var out bytes.Buffer
	if format == "png" {
		err = png.Encode(&out, dst)
	} else {
		err = jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85})
	}
	return out.Bytes(), err
}
