package app

import "testing"

func TestNormalizePPTOCRRotation(t *testing.T) {
	tests := []struct {
		input, want float64
	}{
		{90, 0},
		{-45, -45},
		{-180, 0},
		{30, 30},
	}
	for _, tt := range tests {
		if got := normalizePPTOCRRotation(tt.input); got != tt.want {
			t.Fatalf("normalizePPTOCRRotation(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
