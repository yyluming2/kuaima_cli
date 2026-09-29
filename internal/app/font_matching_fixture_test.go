package app

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestFontMatchingSyntheticMask(t *testing.T) {
	fonts := scanInstalledFonts()
	const text = "风险识别与安全治疗"
	targets := []struct {
		family   string
		bold     bool
		bg0, bg1 color.RGBA
		ink      color.RGBA
	}{
		{"SimHei", false, color.RGBA{245, 238, 220, 255}, color.RGBA{22, 63, 53, 255}, color.RGBA{22, 63, 53, 255}},
		{"NSimSun", false, color.RGBA{218, 235, 245, 255}, color.RGBA{235, 247, 250, 255}, color.RGBA{35, 65, 90, 255}},
		{"Microsoft YaHei", false, color.RGBA{232, 220, 242, 255}, color.RGBA{250, 242, 255, 255}, color.RGBA{62, 28, 78, 255}},
		{"", true, color.RGBA{238, 231, 205, 255}, color.RGBA{205, 220, 214, 255}, color.RGBA{24, 58, 50, 255}}, // first installed CJK bold face
	}
	run := 0
	fixtureDir := filepath.Join("testdata", "font_matching")
	if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, want := range targets {
		var target installedFont
		for _, f := range fonts {
			familyOK := want.family == "" || f.Family == want.family
			if familyOK && (f.Weight >= 600) == want.bold && fontSupportsText(f, text) {
				target = f
				break
			}
		}
		if target.Family == "" {
			continue
		}
		run++
		const width, height = 1000, 180
		mask, ok := renderFontMask(target, text, width, height, 104)
		if !ok {
			t.Fatalf("failed to render synthetic %s bold=%v", want.family, want.bold)
		}
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				bg := want.bg0
				if width > 1 {
					t := float64(x) / float64(width-1)
					bg = color.RGBA{R: uint8(float64(want.bg0.R)*(1-t) + float64(want.bg1.R)*t), G: uint8(float64(want.bg0.G)*(1-t) + float64(want.bg1.G)*t), B: uint8(float64(want.bg0.B)*(1-t) + float64(want.bg1.B)*t), A: 255}
				}
				if mask[y*width+x] {
					img.SetRGBA(x, y, want.ink)
				} else {
					img.SetRGBA(x, y, bg)
				}
			}
		}
		// Exercise the real image path: encode the rendered text, decode it
		// again, then run font matching on the resulting raster image.
		name := target.Family
		if want.bold {
			name += "-bold"
		}
		path := filepath.Join(fixtureDir, name+".png")
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, img); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		readBack, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		decoded, _, err := image.Decode(readBack)
		_ = readBack.Close()
		if err != nil {
			t.Fatal(err)
		}
		family, bold, _ := matchOCRFont(decoded, text, 104, nil)
		if family != target.Family || bold != want.bold {
			t.Errorf("synthetic %s bold=%v matched %q bold=%v", want.family, want.bold, family, bold)
		} else {
			t.Logf("synthetic %s bold=%v => %s bold=%v", target.Family, want.bold, family, bold)
		}
	}
	if run == 0 {
		t.Skip("common CJK fonts are not installed")
	}
}
