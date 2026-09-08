package pptximport

import (
	"bytes"
	genppt "github.com/CoolBanHub/genppt"
	"testing"
)

func TestTextBodyPreservesRunStyles(t *testing.T) {
	n, err := parseTree([]byte(`<sp><txBody><bodyPr/><p><r><rPr sz="1800" b="1"/><t>标题</t></r><r><rPr sz="900" i="1"/><t>正文</t></r></p></txBody></sp>`))
	if err != nil {
		t.Fatal(err)
	}
	content, _, runs := textBody(n, nil)
	if content != "标题正文" || len(runs) != 2 {
		t.Fatalf("unexpected text runs: content=%q runs=%#v", content, runs)
	}
	if got := runs[0].Style["fontSize"]; got != 32.0 {
		t.Fatalf("first run font size = %#v, want 32", got)
	}
	if got := runs[1].Style["fontSize"]; got != 16.0 {
		t.Fatalf("second run font size = %#v, want 16", got)
	}
	if runs[0].Style["bold"] != true || runs[1].Style["italic"] != true {
		t.Fatalf("run emphasis was not preserved: %#v", runs)
	}
}

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
