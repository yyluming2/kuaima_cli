package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestCropImageDataAppliesNormalizedCrop(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 10, 10))
	var in bytes.Buffer
	if err := png.Encode(&in, src); err != nil {
		t.Fatal(err)
	}
	out := cropImageData(in.Bytes(), map[string]any{
		"crop": map[string]any{"left": 0.1, "top": 0.2, "right": 0.4, "bottom": 0.1},
	})
	cfg, err := png.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 5 || cfg.Height != 7 {
		t.Fatalf("unexpected cropped dimensions: %dx%d", cfg.Width, cfg.Height)
	}
}

func TestCropImageDataLeavesInvalidCropUnchanged(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	src.Set(0, 0, color.RGBA{R: 255, A: 255})
	var in bytes.Buffer
	if err := png.Encode(&in, src); err != nil {
		t.Fatal(err)
	}
	out := cropImageData(in.Bytes(), map[string]any{"crop": map[string]any{"left": 1.0}})
	if !bytes.Equal(out, in.Bytes()) {
		t.Fatal("expected invalid crop to preserve source bytes")
	}
}
