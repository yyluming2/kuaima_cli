package main

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	ort "github.com/yalue/onnxruntime_go"
)

func TestConcreteImageShape(t *testing.T) {
	tests := []struct {
		in, want ort.Shape
		nchw     bool
	}{
		{ort.NewShape(1, 3, 320, 320), ort.NewShape(1, 3, 320, 320), true},
		{ort.NewShape(-1, 3, -1, -1), ort.NewShape(1, 3, 1024, 1024), true},
		{ort.NewShape(1, 512, 512, 3), ort.NewShape(1, 512, 512, 3), false},
	}
	for _, test := range tests {
		got, nchw, err := concreteImageShape(test.in)
		if err != nil || !got.Equals(test.want) || nchw != test.nchw {
			t.Fatalf("shape %s => %s, %v, %v", test.in, got, nchw, err)
		}
	}
}

func TestResolveModelStaysInModelsDirectory(t *testing.T) {
	dir := t.TempDir()
	models := filepath.Join(dir, "models")
	if err := os.Mkdir(models, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(models, "local.onnx"), []byte("onnx"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveModel(dir, "local"); err != nil || got != filepath.Join(models, "local.onnx") {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := resolveModel(dir, "../local"); err == nil {
		t.Fatal("path traversal must be rejected")
	}
}

func TestComposePreservesRGBAndAppliesMask(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	source.SetNRGBA(0, 0, color.NRGBA{10, 20, 30, 255})
	source.SetNRGBA(1, 0, color.NRGBA{40, 50, 60, 128})
	got := compose(source, []float32{0, 1}, 2, 1, "none")
	if got.NRGBAAt(0, 0).A != 0 || got.NRGBAAt(1, 0).A != 128 || got.NRGBAAt(1, 0).R != 40 {
		t.Fatalf("unexpected pixels: %#v %#v", got.NRGBAAt(0, 0), got.NRGBAAt(1, 0))
	}
}
