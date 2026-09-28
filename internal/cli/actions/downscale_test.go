package actions

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestDownscaleImageAveragesEachBlock(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 2))
	// Left 2x2 block: black and white columns, averaging to mid grey.
	// Right 2x2 block: solid red.
	for y := 0; y < 2; y++ {
		src.Set(0, y, color.RGBA{0, 0, 0, 255})
		src.Set(1, y, color.RGBA{254, 254, 254, 255})
		src.Set(2, y, color.RGBA{200, 0, 0, 255})
		src.Set(3, y, color.RGBA{200, 0, 0, 255})
	}
	var in bytes.Buffer
	if err := png.Encode(&in, src); err != nil {
		t.Fatal(err)
	}

	out, err := downscaleImage(in.Bytes(), 0.5)
	if err != nil {
		t.Fatal(err)
	}
	got, format, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if format != "png" {
		t.Fatalf("format = %s, want png kept", format)
	}
	if got.Bounds().Dx() != 2 || got.Bounds().Dy() != 1 {
		t.Fatalf("size = %v, want 2x1", got.Bounds())
	}
	if c := color.RGBAModel.Convert(got.At(0, 0)).(color.RGBA); c != (color.RGBA{127, 127, 127, 255}) {
		t.Fatalf("left pixel = %v, want the block's average grey", c)
	}
	if c := color.RGBAModel.Convert(got.At(1, 0)).(color.RGBA); c != (color.RGBA{200, 0, 0, 255}) {
		t.Fatalf("right pixel = %v, want red", c)
	}
}

func TestDownscaleImageKeepsJPEG(t *testing.T) {
	var in bytes.Buffer
	if err := jpeg.Encode(&in, image.NewRGBA(image.Rect(0, 0, 1440, 779)), nil); err != nil {
		t.Fatal(err)
	}
	out, err := downscaleImage(in.Bytes(), 0.5)
	if err != nil {
		t.Fatal(err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if format != "jpeg" || cfg.Width != 720 || cfg.Height != 389 {
		t.Fatalf("got %s %dx%d, want jpeg 720x389", format, cfg.Width, cfg.Height)
	}
}
