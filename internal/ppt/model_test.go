package ppt

import "testing"

func TestNew16x9AndValidate(t *testing.T) {
	p := New16x9("demo")
	p.Slides = []Slide{{ID: "slide-1", Elements: []Element{{ID: "title", Type: "text", Width: 500, Height: 80}}}}
	if err := p.Validate(); err != nil { t.Fatal(err) }
}

func TestValidateRejectsEmptySlides(t *testing.T) {
	p := New16x9("demo")
	if err := p.Validate(); err == nil { t.Fatal("expected validation error") }
}
