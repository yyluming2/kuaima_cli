package ppt

import (
	"encoding/json"
	"testing"
)

func TestNew16x9AndValidate(t *testing.T) {
	p := New16x9("demo")
	p.Slides = []Slide{{ID: "slide-1", Elements: []Element{{ID: "title", Type: "text", Width: 500, Height: 80}}}}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsEmptySlides(t *testing.T) {
	p := New16x9("demo")
	if err := p.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestStructuredDrawingMLFieldsJSON(t *testing.T) {
	p := New16x9("structured")
	p.Slides = []Slide{{ID: "s", Elements: []Element{{ID: "shape", Type: "shape", Geometry: &Geometry{Type: "custom", Path: "M 0 0 L 1 1 Z"}, Fill: &Fill{Type: "solid", Color: &Color{Type: "rgb", RGB: "#fff"}}, Line: &Line{Width: 2, Dash: "dash"}, FlipH: true}}}}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var got Presentation
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	e := got.Slides[0].Elements[0]
	if e.Geometry == nil || e.Geometry.Type != "custom" || !e.FlipH || e.Line.Dash != "dash" {
		t.Fatalf("structured fields lost: %+v", e)
	}
}
