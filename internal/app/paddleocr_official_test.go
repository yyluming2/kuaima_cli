package app

import (
	"image"
	"image/color"
	"testing"
)

// Regression data captured from PaddleOCR's official PP-OCRv6 webpage for
// 图片1.png (1672x941). These are the regions our local post-processing must
// preserve: title, subtitle, the rotated decorative mark, and four labels.
func TestOfficialPaddleOCRImage1Geometry(t *testing.T) {
	want := []struct {
		text string
		box  image.Rectangle
	}{
		{"肺栓塞病人的护理", image.Rect(68, 228, 873, 343)},
		{"从快速识别风险，到安全治疗与出院管理", image.Rect(76, 355, 847, 403)},
		{"+", image.Rect(711, 510, 739, 539)},
		{"IH", image.Rect(489, 489, 567, 567)},
		{"风险识别", image.Rect(77, 583, 189, 616)},
		{"生命体征", image.Rect(273, 583, 385, 616)},
		{"抗凝治疗", image.Rect(471, 584, 583, 616)},
		{"安全出院", image.Rect(669, 583, 781, 616)},
	}
	for _, item := range want {
		if item.text == "" || item.box.Empty() {
			t.Fatalf("invalid official result: %#v", item)
		}
		if item.box.Min.X < 0 || item.box.Min.Y < 0 || item.box.Max.X > 1672 || item.box.Max.Y > 941 {
			t.Fatalf("official box outside 图片1 bounds: %q: %v", item.text, item.box)
		}
	}
	// Ensure line merging does not join separate official rows or the rotated
	// decorative mark into neighboring text.
	boxes := make([]image.Rectangle, 0, len(want))
	for _, item := range want {
		boxes = append(boxes, item.box)
	}
	got := mergePPTOCRBoxes(boxes)
	if len(got) != len(want) {
		t.Fatalf("merged official regions: got %d, want %d (%v)", len(got), len(want), got)
	}
}

func TestRotatePPTOCRImageArbitraryAngle(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 40))
	for _, angle := range []int{-45, -15, 15, 45, 90} {
		rot := rotatePPTOCRImage(src, angle)
		if rot.Bounds().Dx() < 40 || rot.Bounds().Dy() < 20 {
			t.Fatalf("angle %d clipped image: %v", angle, rot.Bounds())
		}
	}
}

func TestEstimatePPTOCRRotation(t *testing.T) {
	im := image.NewRGBA(image.Rect(0, 0, 120, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 120; x++ {
			im.Set(x, y, color.White)
		}
	}
	for i := 0; i < 35; i++ {
		x, y := 35+i, 12+i/2
		im.Set(x, y, color.Black)
		im.Set(x+1, y, color.Black)
	}
	angle := estimatePPTOCRRotation(im)
	if angle < 5 || angle > 25 {
		t.Fatalf("estimated diagonal text angle %.2f", angle)
	}
}
