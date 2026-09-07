// Package ppt defines the editable, renderer-agnostic presentation model.
// It is shared by AI generation, the web editor, PPTX import and export.
package ppt

import (
	"fmt"
	"strings"
)

type Presentation struct {
	ID     string  `json:"id,omitempty"`
	Title  string  `json:"title"`
	Width  float64 `json:"width"` // logical pixels; 16:9 defaults to 1280x720
	Height float64 `json:"height"`
	Theme  Theme   `json:"theme"`
	Slides []Slide `json:"slides"`
}

type Theme struct {
	Name       string   `json:"name"`
	Background string   `json:"background"`
	Foreground string   `json:"foreground"`
	Accent     string   `json:"accent"`
	Fonts      []string `json:"fonts,omitempty"`
}

type Slide struct {
	ID       string    `json:"id"`
	Layout   string    `json:"layout,omitempty"`
	Notes    string    `json:"notes,omitempty"`
	Elements []Element `json:"elements"`
}

type Element struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"` // text, image, shape, table, chart
	X        float64        `json:"x"`
	Y        float64        `json:"y"`
	Width    float64        `json:"width"`
	Height   float64        `json:"height"`
	Content  string         `json:"content,omitempty"`
	AssetURL string         `json:"assetUrl,omitempty"`
	Style    map[string]any `json:"style,omitempty"`
	Locked   bool           `json:"locked,omitempty"`
	ZIndex   int            `json:"zIndex,omitempty"`
	Rotation float64        `json:"rotation,omitempty"`
	Opacity  float64        `json:"opacity,omitempty"`
}

func (p *Presentation) Validate() error {
	if p.Width <= 0 || p.Height <= 0 {
		return fmt.Errorf("presentation dimensions must be positive")
	}
	if len(p.Slides) == 0 {
		return fmt.Errorf("presentation must contain at least one slide")
	}
	for i, s := range p.Slides {
		if s.ID == "" {
			return fmt.Errorf("slide %d has no id", i)
		}
		for j, e := range s.Elements {
			if e.ID == "" || e.Type == "" {
				return fmt.Errorf("slide %s element %d requires id and type", s.ID, j)
			}
			if e.Width < 0 || e.Height < 0 {
				return fmt.Errorf("element %s has negative size", e.ID)
			}
		}
	}
	return nil
}

// Diagnostics returns non-fatal layout warnings suitable for the editor.
func (p *Presentation) Diagnostics() []string {
	var out []string
	for _, s := range p.Slides {
		for _, e := range s.Elements {
			if e.X < 0 || e.Y < 0 || e.X+e.Width > p.Width || e.Y+e.Height > p.Height {
				out = append(out, s.ID+"/"+e.ID+": element exceeds slide bounds")
			}
			if e.Type == "text" && len([]rune(e.Content)) > 180 {
				out = append(out, s.ID+"/"+e.ID+": text may overflow")
			}
			if e.Type == "image" && e.AssetURL == "" {
				out = append(out, s.ID+"/"+e.ID+": image has no source")
			}
			if c, ok := e.Style["color"].(string); ok && c == "" {
				out = append(out, s.ID+"/"+e.ID+": empty text color")
			}
			if e.Type == "text" {
				if fs, ok := e.Style["fontSize"].(float64); ok && fs < 10 {
					out = append(out, s.ID+"/"+e.ID+": font size is too small")
				}
				if strings.TrimSpace(e.Content) == "" {
					out = append(out, s.ID+"/"+e.ID+": empty text")
				}
			}
			if e.Style != nil {
				if c, ok := e.Style["color"].(string); ok && len(c) > 0 && !strings.HasPrefix(c, "#") {
					out = append(out, s.ID+"/"+e.ID+": color should use hex format")
				}
				if ff, ok := e.Style["fontFamily"].(string); ok && ff != "" && len(p.Theme.Fonts) > 0 {
					found := false
					for _, tf := range p.Theme.Fonts {
						if strings.EqualFold(tf, ff) {
							found = true
						}
					}
					if !found {
						out = append(out, s.ID+"/"+e.ID+": font differs from theme")
					}
				}
			}
		}
	}
	return out
}

func New16x9(title string) Presentation {
	return Presentation{Title: title, Width: 1280, Height: 720,
		Theme: Theme{Name: "商务蓝", Background: "#FFFFFF", Foreground: "#172033", Accent: "#2563EB", Fonts: []string{"Aptos", "Microsoft YaHei"}}}
}
