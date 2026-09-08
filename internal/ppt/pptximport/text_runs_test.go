package pptximport

import (
	"testing"

	"kuaima_cli/internal/ppt"
)

func TestMergeTextRunsKeepsParagraphBoundaries(t *testing.T) {
	runs := []ppt.TextRun{
		{Content: "a", Paragraph: 0, Style: map[string]any{"fontSize": 20.0}},
		{Content: "b", Paragraph: 0, Style: map[string]any{"fontSize": 20.0}},
		{Content: "c", Paragraph: 0, Style: map[string]any{"fontSize": 18.0}},
		{Content: "d", Paragraph: 1, Style: map[string]any{"fontSize": 18.0}},
		{Content: "e", Paragraph: 1, Style: map[string]any{"fontSize": 18.0}},
	}
	got := mergeTextRuns(runs)
	if len(got) != 3 {
		t.Fatalf("got %d runs, want 3: %#v", len(got), got)
	}
	if got[0].Content != "ab" || got[1].Content != "c" || got[2].Content != "de" {
		t.Fatalf("unexpected merged content: %#v", got)
	}
}
