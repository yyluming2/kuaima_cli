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
	// Structured DrawingML information.  The legacy Style map remains the
	// compatibility surface for existing clients; these fields allow renderers
	// to choose HTML/CSS or SVG without reparsing opaque style values.
	Geometry *Geometry  `json:"geometry,omitempty"`
	Fill     *Fill      `json:"fill,omitempty"`
	Line     *Line      `json:"line,omitempty"`
	Effects  *Effects   `json:"effects,omitempty"`
	Text     *TextStyle `json:"textStyle,omitempty"`
	FlipH    bool       `json:"flipH,omitempty"`
	FlipV    bool       `json:"flipV,omitempty"`
}

type Geometry struct {
	Type    string `json:"type"` // rect, roundRect, ellipse, preset, custom, line
	Preset  string `json:"preset,omitempty"`
	Path    string `json:"path,omitempty"` // SVG path for custom geometry
	ViewBox string `json:"viewBox,omitempty"`
}

type Color struct {
	Type   string  `json:"type,omitempty"`
	RGB    string  `json:"rgb,omitempty"`
	Scheme string  `json:"scheme,omitempty"`
	Alpha  float64 `json:"alpha,omitempty"`
	LumMod float64 `json:"lumMod,omitempty"`
	LumOff float64 `json:"lumOff,omitempty"`
	Tint   float64 `json:"tint,omitempty"`
	Shade  float64 `json:"shade,omitempty"`
}

type GradientStop struct {
	Position float64 `json:"position"`
	Color    Color   `json:"color"`
}
type Gradient struct {
	Type  string         `json:"type,omitempty"`
	Angle float64        `json:"angle,omitempty"`
	Stops []GradientStop `json:"stops,omitempty"`
}
type Fill struct {
	Type     string       `json:"type"`
	Color    *Color       `json:"color,omitempty"`
	Gradient *Gradient    `json:"gradient,omitempty"`
	Image    *ImageFill   `json:"image,omitempty"`
	Pattern  *PatternFill `json:"pattern,omitempty"`
}
type ImageFill struct {
	AssetURL        string `json:"assetUrl,omitempty"`
	RotateWithShape bool   `json:"rotateWithShape,omitempty"`
}
type PatternFill struct {
	Preset     string `json:"preset,omitempty"`
	Foreground *Color `json:"foreground,omitempty"`
	Background *Color `json:"background,omitempty"`
}
type Line struct {
	Width      float64 `json:"width,omitempty"`
	Color      *Color  `json:"color,omitempty"`
	Dash       string  `json:"dash,omitempty"`
	BeginArrow string  `json:"beginArrow,omitempty"`
	EndArrow   string  `json:"endArrow,omitempty"`
}
type Effects struct {
	Shadow      string  `json:"shadow,omitempty"`
	InnerShadow string  `json:"innerShadow,omitempty"`
	Glow        string  `json:"glow,omitempty"`
	SoftEdge    float64 `json:"softEdge,omitempty"`
	Reflection  bool    `json:"reflection,omitempty"`
}
type TextStyle struct {
	FontFamily    string  `json:"fontFamily,omitempty"`
	FontSize      float64 `json:"fontSize,omitempty"`
	Bold          bool    `json:"bold,omitempty"`
	Italic        bool    `json:"italic,omitempty"`
	Underline     bool    `json:"underline,omitempty"`
	Align         string  `json:"align,omitempty"`
	VerticalAlign string  `json:"verticalAlign,omitempty"`
	MarginLeft    float64 `json:"marginLeft,omitempty"`
	MarginRight   float64 `json:"marginRight,omitempty"`
	MarginTop     float64 `json:"marginTop,omitempty"`
	MarginBottom  float64 `json:"marginBottom,omitempty"`
	LineHeight    float64 `json:"lineHeight,omitempty"`
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
