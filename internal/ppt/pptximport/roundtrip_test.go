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

func TestTextBodyPreservesParagraphs(t *testing.T) {
	n, _ := parseTree([]byte(`<sp><txBody><bodyPr/><p><pPr algn="l"/><r><t>A</t></r></p><p><pPr algn="ctr"/><r><t>B</t></r></p></txBody></sp>`))
	_, _, runs, paragraphs := textBodyDetailed(n, nil)
	if len(paragraphs) != 2 || paragraphs[0].Align != "l" || paragraphs[1].Align != "ctr" || runs[1].Paragraph != 1 {
		t.Fatalf("paragraph hierarchy lost: %#v %#v", paragraphs, runs)
	}
}

func TestRunExplicitOffOverridesParagraphDefaults(t *testing.T) {
	n, _ := parseTree([]byte(`<sp><txBody><bodyPr/><p><pPr><defRPr b="1" i="1" u="sng"/></pPr><r><rPr b="0" i="0" u="none"/><t>plain</t></r></p></txBody></sp>`))
	_, _, runs := textBody(n, nil)
	if len(runs) != 1 || runs[0].Style["bold"] != false || runs[0].Style["italic"] != false || runs[0].Style["underline"] != false {
		t.Fatalf("explicit run-off formatting was lost: %#v", runs)
	}
}

func TestAbsoluteLineSpacingUsesParagraphFontSize(t *testing.T) {
	n, err := parseTree([]byte(`<sp><txBody><bodyPr/><p><pPr><defRPr sz="2000"/><lnSpc><spcPts val="2400"/></lnSpc></pPr><r><rPr sz="2000"/><t>text</t></r></p></txBody></sp>`))
	if err != nil {
		t.Fatal(err)
	}
	_, style, _, paragraphs := textBodyDetailed(n, nil)
	if got := style["lineHeight"]; got != 1.2 {
		t.Fatalf("absolute 24pt spacing on 20pt text should be 1.2, got %#v", got)
	}
	if len(paragraphs) != 1 || paragraphs[0].LineHeight != 1.2 {
		t.Fatalf("paragraph line height not preserved: %#v", paragraphs)
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

func TestReadGeneratedBlankSlideUsesEmptyElements(t *testing.T) {
	p := genppt.New().SetTitle("blank")
	p.AddSlide()
	b, err := p.ToBytes()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Read(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Slides) != 1 {
		t.Fatalf("slides=%d, want 1", len(got.Slides))
	}
	if got.Slides[0].Elements == nil {
		t.Fatal("blank slide elements is nil, want an empty slice")
	}
	if len(got.Slides[0].Elements) != 0 {
		t.Fatalf("blank slide elements=%d, want 0", len(got.Slides[0].Elements))
	}
}

func TestReadGeneratedRichText(t *testing.T) {
	p := genppt.New().SetTitle("rich")
	s := p.AddSlide()
	s.AddRichText([]genppt.RichTextRun{
		{Text: "Big", Options: genppt.TextOptions{FontSize: 24, FontColor: "FF0000", Bold: true}, Paragraph: 0},
		{Text: "small", Options: genppt.TextOptions{FontSize: 10, FontColor: "0000FF"}, Paragraph: 0},
		{Text: "next", Options: genppt.TextOptions{FontSize: 12}, Paragraph: 1, Align: genppt.AlignCenter},
	}, genppt.TextOptions{X: 1, Y: 1, Width: 5, Height: 1})
	b, err := p.ToBytes()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Read(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	e := got.Slides[0].Elements[0]
	if len(e.TextRuns) != 3 || e.TextRuns[0].Style["bold"] != true {
		t.Fatalf("rich runs lost: %#v", e.TextRuns)
	}
}

func TestReadGeneratedVerticalAnchors(t *testing.T) {
	p := genppt.New().SetTitle("anchors")
	s := p.AddSlide()
	s.AddText("top", genppt.TextOptions{X: 1, Y: 1, Width: 3, Height: 1, VAlign: genppt.VAlignTop})
	s.AddText("bottom", genppt.TextOptions{X: 1, Y: 2, Width: 3, Height: 1, VAlign: genppt.VAlignBottom})
	s.AddRichText([]genppt.RichTextRun{{Text: "middle", Options: genppt.TextOptions{VAlign: genppt.VAlignMiddle}, Paragraph: 0}}, genppt.TextOptions{X: 1, Y: 3, Width: 3, Height: 1, VAlign: genppt.VAlignMiddle})
	b, err := p.ToBytes()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Read(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Slides[0].Elements) != 3 {
		t.Fatalf("elements=%d", len(got.Slides[0].Elements))
	}
	for i, want := range []string{"t", "b", "ctr"} {
		if got.Slides[0].Elements[i].Text.VerticalAlign != want {
			t.Errorf("%s anchor=%q", want, got.Slides[0].Elements[i].Text.VerticalAlign)
		}
	}
}
