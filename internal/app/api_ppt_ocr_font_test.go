package app

import (
	"math"
	"testing"
)

func TestVerticalOCRFontSizeKeepsInset(t *testing.T) {
	if got := verticalOCRFontSize(40, 40); math.Abs(got-38) > 1e-9 {
		t.Fatalf("verticalOCRFontSize(40, 40) = %v, want 38", got)
	}
	if got := verticalOCRFontSize(4, 40); math.Abs(got-2) > 1e-9 {
		t.Fatalf("verticalOCRFontSize(4, 40) = %v, want 2", got)
	}
}

func TestVerticalOCRFontSizeFitsTotalTextHeight(t *testing.T) {
	if got := verticalOCRFontSizeForText(40, 100, "竖排文字", 40); math.Abs(got-24.5) > 1e-9 {
		t.Fatalf("verticalOCRFontSizeForText() = %v, want 24.5", got)
	}
	if got := verticalOCRFontSizeForText(40, 100, "字", 40); math.Abs(got-38) > 1e-9 {
		t.Fatalf("single glyph should retain width-limited size, got %v", got)
	}
}
