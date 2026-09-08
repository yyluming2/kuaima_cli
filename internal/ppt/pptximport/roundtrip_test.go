package pptximport

import (
	"bytes"
	genppt "github.com/CoolBanHub/genppt"
	"testing"
)

func TestReadGenerated(t *testing.T) {
	p := genppt.New().SetTitle("x")
	s := p.AddSlide()
	s.AddText("hello", genppt.TextOptions{X: 1, Y: 1, Width: 5, Height: 1})
	b, e := p.ToBytes()
	if e != nil {
		t.Fatal(e)
	}
	got, e := Read(bytes.NewReader(b), int64(len(b)))
	if e != nil {
		t.Fatal(e)
	}
	if len(got.Slides) == 0 || len(got.Slides[0].Elements) == 0 {
		t.Fatalf("empty: %#v", got)
	}
}
