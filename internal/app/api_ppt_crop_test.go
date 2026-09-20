package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"kuaima_cli/internal/ppt"
	genppt "github.com/CoolBanHub/genppt"
)

func TestCustomGeometrySVGEmbedsImageFill(t *testing.T) {
	e := ppt.Element{
		Geometry: &ppt.Geometry{Type: "custom", Path: "M0 0H100V100Z", ViewBox: "0 0 100 100"},
		Fill:     &ppt.Fill{Type: "image", Image: &ppt.ImageFill{AssetURL: "data:image/png;base64,ZmFrZQ=="}},
	}
	data := customGeometrySVG(e, genppt.ShapeOptions{Fill: "transparent"})
	s := string(data)
	if !strings.Contains(s, "clipPath") || !strings.Contains(s, "data:image/png;base64,ZmFrZQ==") {
		t.Fatalf("custom geometry image fill was not embedded: %s", s)
	}
	if !strings.Contains(s, "clip-path=\"url(#ppt-image-clip)\"") {
		t.Fatalf("custom geometry does not clip image to path: %s", s)
	}
}

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
