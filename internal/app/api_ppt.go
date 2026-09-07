package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	genppt "github.com/CoolBanHub/genppt"
	"image"
	"kuaima_cli/internal/ppt"
	"net/http"
	"strings"
)

// fitImageBox keeps the source aspect ratio when placing an image in PPTX.
// The editor uses object-fit: contain; apply the same contain behavior to the
// exported slide instead of letting Office stretch the bitmap to the box.
func fitImageBox(data []byte, x, y, width, height float64) (float64, float64, float64, float64) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || width <= 0 || height <= 0 {
		return x, y, width, height
	}
	contentRatio := float64(cfg.Width) / float64(cfg.Height)
	boxRatio := width / height
	if contentRatio > boxRatio {
		oldHeight := height
		height = width / contentRatio
		y += (oldHeight - height) / 2
	} else {
		oldWidth := width
		width = height * contentRatio
		x += (oldWidth - width) / 2
	}
	return x, y, width, height
}

// handlePPTDraft returns a deterministic editable starter document. AI services
// can fill the same schema later without changing the editor contract.
func (s *webUIServer) handlePPTDraft(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var in struct {
		Title string `json:"title"`
		Theme string `json:"theme"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	p := ppt.New16x9(in.Title)
	if in.Theme != "" {
		p.Theme.Name = in.Theme
	}
	title := in.Title
	if title == "" {
		title = "产品介绍"
	}
	p.Title = title
	// Deterministic content plan keeps the editor useful even when no AI provider is configured.
	p.Slides = []ppt.Slide{
		{ID: "slide-1", Layout: "title", Elements: []ppt.Element{{ID: "title", Type: "text", X: 80, Y: 180, Width: 1120, Height: 100, Content: title, Style: map[string]any{"fontSize": 48, "fontWeight": 700}}, {ID: "subtitle", Type: "text", X: 80, Y: 300, Width: 1120, Height: 60, Content: "方案概览与行动计划", Style: map[string]any{"fontSize": 24, "color": p.Theme.Accent}}}},
		{ID: "slide-2", Layout: "agenda", Elements: []ppt.Element{{ID: "title", Type: "text", X: 80, Y: 70, Width: 1120, Height: 70, Content: "内容概要", Style: map[string]any{"fontSize": 34, "fontWeight": 700}}, {ID: "body", Type: "text", X: 100, Y: 180, Width: 1080, Height: 300, Content: "背景与机会\n核心方案与价值\n落地计划与下一步", Style: map[string]any{"fontSize": 28, "lineHeight": 1.6}}}},
		{ID: "slide-3", Layout: "content", Elements: []ppt.Element{{ID: "title", Type: "text", X: 80, Y: 70, Width: 1120, Height: 70, Content: "核心方案", Style: map[string]any{"fontSize": 34, "fontWeight": 700}}, {ID: "body", Type: "text", X: 100, Y: 180, Width: 1080, Height: 300, Content: "以用户目标为中心组织信息，\n用清晰的结构帮助团队快速决策。", Style: map[string]any{"fontSize": 28, "lineHeight": 1.6}}}},
		{ID: "slide-4", Layout: "closing", Elements: []ppt.Element{{ID: "title", Type: "text", X: 80, Y: 220, Width: 1120, Height: 90, Content: "下一步行动", Style: map[string]any{"fontSize": 44, "fontWeight": 700}}, {ID: "body", Type: "text", X: 80, Y: 340, Width: 1120, Height: 70, Content: "感谢观看 · 欢迎交流", Style: map[string]any{"fontSize": 24, "color": p.Theme.Accent}}}},
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *webUIServer) handlePPTValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var p ppt.Presentation
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := p.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "diagnostics": p.Diagnostics()})
}

// handlePPTOptimize applies deterministic style presets for editor previews.
func (s *webUIServer) handlePPTOptimize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	var in struct {
		Presentation ppt.Presentation `json:"presentation"`
		Mode         string           `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if in.Mode == "" {
		in.Mode = "quick"
	}
	for si := range in.Presentation.Slides {
		for ei := range in.Presentation.Slides[si].Elements {
			e := &in.Presentation.Slides[si].Elements[ei]
			if e.Style == nil {
				e.Style = map[string]any{}
			}
			switch in.Mode {
			case "professional":
				e.Style["fontFamily"] = "Aptos"
				e.Style["lineHeight"] = 1.35
			case "brand":
				e.Style["fontFamily"] = in.Presentation.Theme.Fonts[0]
				e.Style["color"] = in.Presentation.Theme.Foreground
				e.Style["accent"] = in.Presentation.Theme.Accent
			default:
				e.Style["lineHeight"] = 1.2
			}
		}
	}
	writeJSON(w, 200, in.Presentation)
}

// handlePPTXImport accepts an editable JSON representation (and keeps the
// endpoint compatible with future binary PPTX parsers).
func (s *webUIServer) handlePPTXImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var p ppt.Presentation
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err := p.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, p)
}

func (s *webUIServer) handlePPTXExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	var model ppt.Presentation
	if err := json.NewDecoder(r.Body).Decode(&model); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err := model.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	p := genppt.New().SetTitle(model.Title).SetSlideSize16x9()
	for _, sld := range model.Slides {
		sl := p.AddSlide()
		for _, e := range sld.Elements {
			opts := genppt.ShapeOptions{X: e.X / 128, Y: e.Y / 128, Width: e.Width / 128, Height: e.Height / 128}
			if e.Style != nil {
				if v, ok := e.Style["fill"].(string); ok {
					opts.Fill = v
				}
				if v, ok := e.Style["lineColor"].(string); ok {
					opts.LineColor = v
				}
				if v, ok := e.Style["opacity"].(float64); ok {
					opts.Transparency = (1 - v) * 100
				}
			}
			if e.Type == "text" {
				o := genppt.TextOptions{X: e.X / 128, Y: e.Y / 128, Width: e.Width / 128, Height: e.Height / 128}
				if v, ok := e.Style["fontSize"].(float64); ok {
					o.FontSize = v
				}
				if v, ok := e.Style["color"].(string); ok {
					o.FontColor = v
				}
				if v, ok := e.Style["fontWeight"].(float64); ok {
					o.Bold = v >= 600
				}
				if v, ok := e.Style["background"].(string); ok && v != "transparent" {
					o.Fill = v
				}
				sl.AddText(e.Content, o)
			} else if e.Type == "shape" {
				sl.AddShape(genppt.ShapeRect, opts)
			} else if e.Type == "table" {
				var rows [][]genppt.TableCell
				for _, line := range strings.Split(e.Content, "\n") {
					var row []genppt.TableCell
					for _, c := range strings.Split(line, "\t") {
						row = append(row, genppt.TableCell{Text: c})
					}
					rows = append(rows, row)
				}
				sl.AddTable(rows, genppt.TableOptions{X: opts.X, Y: opts.Y, Width: opts.Width})
			} else if e.Type == "image" && e.AssetURL != "" {
				if i := strings.Index(e.AssetURL, ","); i >= 0 {
					if data, err := base64.StdEncoding.DecodeString(e.AssetURL[i+1:]); err == nil {
						x, y, w, h := fitImageBox(data, e.X/128, e.Y/128, e.Width/128, e.Height/128)
						sl.AddImage(genppt.ImageOptions{X: x, Y: y, Width: w, Height: h, Data: data})
					}
				}
			}
		}
	}
	b, err := p.ToBytes()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.presentationml.presentation")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", "presentation.pptx"))
	w.Write(b)
}
