package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"kuaima_cli/internal/ppt"
	"kuaima_cli/internal/ppt/pptximport"
	"net/http"
	"net/url"
	"strings"

	genppt "github.com/CoolBanHub/genppt"
)

type pptPlanPage struct {
	Title       string   `json:"title"`
	Bullets     []string `json:"bullets,omitempty"`
	Layout      string   `json:"layout,omitempty"`
	ImagePrompt string   `json:"imagePrompt,omitempty"`
}
type pptPlan struct {
	Title      string        `json:"title"`
	Style      string        `json:"style"`
	TextModel  string        `json:"textModel,omitempty"`
	ImageModel string        `json:"imageModel,omitempty"`
	Audience   string        `json:"audience,omitempty"`
	References []string      `json:"references,omitempty"`
	Images     []string      `json:"images,omitempty"`
	Pages      []pptPlanPage `json:"pages"`
}

// handlePPTPlan creates a confirmation-ready brief with the selected text model.
func (s *webUIServer) handlePPTPlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var in struct {
		Title, Style, Audience string
		TextModel              string `json:"textModel"`
		ImageModel             string `json:"imageModel"`
		References, Images     []string
		PageCount              int `json:"pageCount"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&in); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = "产品介绍"
	}
	style := strings.TrimSpace(in.Style)
	if style == "" {
		style = "现代商务"
	}
	n := in.PageCount
	if n < 0 {
		n = 1
	}
	if n > 50 {
		n = 50
	}
	model := strings.TrimSpace(in.TextModel)
	if model == "" && s.clientOpts.model != nil {
		model = strings.TrimSpace(*s.clientOpts.model)
	}
	if model == "" {
		writeError(w, http.StatusBadRequest, "textModel is required")
		return
	}
	c, err := s.clientOpts.newClient()
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer c.Close()
	prompt := fmt.Sprintf(`请为“%s”设计一份 PPT 大纲。内容要求：%s。目标受众：%s。页数：%d 页。视觉风格：%s。
只返回严格 JSON，不要 Markdown 代码块或额外文字。格式：{"pages":[{"title":"页面标题","bullets":["要点1","要点2"],"layout":"title|content","imagePrompt":"配图描述"}]}。必须恰好返回 %d 页，第一页为 title 布局。`, title, strings.TrimSpace(strings.Join(in.References, "\n")), strings.TrimSpace(in.Audience), n, style, n)
	resp, err := c.createResponse(r.Context(), responseRequest{Model: model, Input: prompt})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if resp == nil {
		writeError(w, http.StatusBadGateway, "text model returned empty response")
		return
	}
	var generated struct {
		Pages []pptPlanPage `json:"pages"`
	}
	text := strings.TrimSpace(resp.text())
	text = strings.TrimPrefix(strings.TrimSuffix(text, "```"), "```json")
	if start, end := strings.Index(text, "{"), strings.LastIndex(text, "}"); start >= 0 && end > start {
		text = text[start : end+1]
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &generated); err != nil || len(generated.Pages) == 0 {
		writeError(w, http.StatusBadGateway, "text model returned invalid PPT outline")
		return
	}
	if len(generated.Pages) > n {
		generated.Pages = generated.Pages[:n]
	}
	for i := range generated.Pages {
		if strings.TrimSpace(generated.Pages[i].Title) == "" {
			generated.Pages[i].Title = fmt.Sprintf("第 %d 页", i+1)
		}
		if generated.Pages[i].ImagePrompt == "" {
			generated.Pages[i].ImagePrompt = generated.Pages[i].Title + "，" + style + "风格的简洁插画"
		}
		if i == 0 {
			generated.Pages[i].Layout = "title"
		}
	}
	pages := generated.Pages
	writeJSON(w, 200, pptPlan{Title: title, Style: style, TextModel: model, ImageModel: strings.TrimSpace(in.ImageModel), Audience: strings.TrimSpace(in.Audience), References: in.References, Images: in.Images, Pages: pages})
}

func illustrationDataURL(label string) string {
	label = strings.ReplaceAll(strings.ReplaceAll(label, "&", "&amp;"), "<", "&lt;")
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="800" height="450"><defs><linearGradient id="g" x1="0" x2="1"><stop stop-color="#2563eb"/><stop offset="1" stop-color="#7c3aed"/></linearGradient></defs><rect width="800" height="450" rx="28" fill="url(#g)"/><circle cx="650" cy="110" r="90" fill="#fff" opacity=".18"/><circle cx="710" cy="300" r="140" fill="#fff" opacity=".12"/><text x="48" y="370" fill="white" font-family="Arial,sans-serif" font-size="34">%s</text></svg>`, label)
	return "data:image/svg+xml," + url.PathEscape(svg)
}

// handlePPTGenerate turns an approved plan into the editor's canonical model.
func (s *webUIServer) handlePPTGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var plan pptPlan
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&plan); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(plan.Title) == "" || len(plan.Pages) == 0 {
		writeError(w, 400, "title and pages are required")
		return
	}
	p := ppt.New16x9(plan.Title)
	p.Theme.Name = plan.Style
	if strings.Contains(strings.ToLower(plan.Style), "深") {
		p.Theme.Background = "#0F172A"
		p.Theme.Foreground = "#F8FAFC"
		p.Theme.Accent = "#38BDF8"
	}
	for i, page := range plan.Pages {
		elems := []ppt.Element{{ID: fmt.Sprintf("slide-%d-title", i+1), Type: "text", X: 72, Y: 56, Width: 1136, Height: 72, Content: page.Title, Style: map[string]any{"fontSize": 36, "fontWeight": 700, "color": p.Theme.Foreground}}}
		body := strings.Join(page.Bullets, "\n")
		elems = append(elems, ppt.Element{ID: fmt.Sprintf("slide-%d-body", i+1), Type: "text", X: 92, Y: 164, Width: 650, Height: 360, Content: body, Style: map[string]any{"fontSize": 26, "lineHeight": 1.5, "color": p.Theme.Foreground}})
		asset := ""
		if i < len(plan.Images) {
			asset = plan.Images[i]
		}
		if asset == "" && page.ImagePrompt != "" {
			asset = illustrationDataURL(page.ImagePrompt)
		}
		if asset != "" {
			elems = append(elems, ppt.Element{ID: fmt.Sprintf("slide-%d-image", i+1), Type: "image", X: 790, Y: 170, Width: 400, Height: 300, AssetURL: asset, Style: map[string]any{"background": "transparent"}})
		}
		p.Slides = append(p.Slides, ppt.Slide{ID: fmt.Sprintf("slide-%d", i+1), Layout: page.Layout, Elements: elems})
	}
	writeJSON(w, 200, p)
}

func styleString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
func styleColor(m map[string]any, key string) string {
	if v := styleString(m, key); v != "" {
		return v
	}
	if m != nil {
		if o, ok := m[key].(map[string]any); ok {
			if c, ok := o["color"].(string); ok {
				return c
			}
			if c, ok := o["rgb"].(string); ok {
				return c
			}
		}
	}
	return ""
}
func styleFloat(m map[string]any, key string) (float64, bool) {
	if m == nil {
		return 0, false
	}
	switch v := m[key].(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}
func styleBool(m map[string]any, key string) bool { v, _ := m[key].(bool); return v }
func colorFrom(c *ppt.Color) string {
	if c == nil {
		return ""
	}
	if c.RGB != "" {
		return c.RGB
	}
	return ""
}
func cssColor(v string) (string, float64) {
	v = strings.TrimSpace(v)
	if strings.Contains(strings.ToLower(v), "gradient(") { // genppt solid-fill fallback: retain the first stop color
		if i := strings.Index(v, "#"); i >= 0 {
			end := i + 1
			for end < len(v) && strings.ContainsRune("0123456789abcdefABCDEF", rune(v[end])) {
				end++
			}
			return v[i:end], 0
		}
	}
	if strings.HasPrefix(strings.ToLower(v), "rgba(") {
		parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(v[5:], "("), ")"), ",")
		if len(parts) == 4 {
			var r, g, b int
			var a float64
			if _, err := fmt.Sscanf(strings.TrimSpace(parts[0]), "%d", &r); err == nil {
				_, err = fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &g)
				if err == nil {
					_, err = fmt.Sscanf(strings.TrimSpace(parts[2]), "%d", &b)
				}
				if err == nil {
					_, err = fmt.Sscanf(strings.TrimSpace(parts[3]), "%f", &a)
				}
				if err == nil {
					return fmt.Sprintf("#%02X%02X%02X", r, g, b), (1 - a) * 100
				}
			}
		}
	}
	return v, 0
}
func borderStyleValue(v string) genppt.BorderStyle {
	switch strings.ToLower(v) {
	case "dash", "dashed":
		return genppt.BorderDash
	case "dot", "dotted":
		return genppt.BorderDot
	case "none":
		return genppt.BorderNone
	default:
		return genppt.BorderSolid
	}
}
func alignValue(v string) genppt.Align {
	switch strings.ToLower(v) {
	case "center", "ctr":
		return genppt.AlignCenter
	case "right", "r":
		return genppt.AlignRight
	case "justify", "just":
		return genppt.AlignJustify
	default:
		return genppt.AlignLeft
	}
}
func valignValue(v string) genppt.VerticalAlign {
	switch strings.ToLower(v) {
	case "middle", "center", "ctr":
		return genppt.VAlignMiddle
	case "bottom", "b":
		return genppt.VAlignBottom
	default:
		return genppt.VAlignTop
	}
}

// cropImageData applies the editor's normalized crop values to the bitmap
// before handing it to genppt. genppt writes an image into a shape but does
// not expose DrawingML srcRect/crop support, so cropping the source pixels is
// the portable equivalent for exported PPTX files.
func cropImageData(data []byte, style map[string]any) []byte {
	if len(data) == 0 || style == nil {
		return data
	}
	var raw map[string]any
	switch v := style["crop"].(type) {
	case map[string]any:
		raw = v
	case map[string]float64:
		raw = make(map[string]any, len(v))
		for k, n := range v {
			raw[k] = n
		}
	}
	if raw == nil {
		return data
	}
	value := func(key string) float64 {
		switch v := raw[key].(type) {
		case float64:
			return v
		case float32:
			return float64(v)
		case int:
			return float64(v)
		case json.Number:
			n, _ := v.Float64()
			return n
		default:
			return 0
		}
	}
	left, top, right, bottom := value("left"), value("top"), value("right"), value("bottom")
	clamp := func(v float64) float64 {
		if v < 0 {
			return 0
		}
		if v > 1 {
			return 1
		}
		return v
	}
	left, top, right, bottom = clamp(left), clamp(top), clamp(right), clamp(bottom)
	if left == 0 && top == 0 && right == 0 && bottom == 0 {
		return data
	}
	if left+right >= 1 || top+bottom >= 1 {
		return data
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data
	}
	b := src.Bounds()
	x0 := b.Min.X + int(float64(b.Dx())*left)
	y0 := b.Min.Y + int(float64(b.Dy())*top)
	x1 := b.Max.X - int(float64(b.Dx())*right)
	y1 := b.Max.Y - int(float64(b.Dy())*bottom)
	if x1 <= x0 || y1 <= y0 {
		return data
	}
	out := image.NewNRGBA(image.Rect(0, 0, x1-x0, y1-y0))
	draw.Draw(out, out.Bounds(), src, image.Point{X: x0, Y: y0}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return data
	}
	return buf.Bytes()
}

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
		Title      string   `json:"title"`
		Theme      string   `json:"theme"`
		References []string `json:"references"`
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
	// Accept a binary OOXML .pptx upload. Keep JSON support for callers that
	// already have the editable presentation model.
	body, err := io.ReadAll(io.LimitReader(r.Body, 100<<20))
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	var p ppt.Presentation
	if len(body) >= 2 && body[0] == 'P' && body[1] == 'K' {
		p, err = pptximport.Read(bytes.NewReader(body), int64(len(body)))
	} else {
		err = json.Unmarshal(body, &p)
	}
	if err != nil {
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
	p := genppt.New().SetTitle(model.Title)
	if model.Width > 0 && model.Height > 0 {
		p.SetSlideSize(model.Width/ppt.LogicalDPI, model.Height/ppt.LogicalDPI)
	} else {
		p.SetSlideSize16x9()
	}
	for _, sld := range model.Slides {
		sl := p.AddSlide()
		if model.Theme.Background != "" {
			sl.SetBackground(genppt.BackgroundOptions{Color: model.Theme.Background})
		}
		for _, e := range sld.Elements {
			opts := genppt.ShapeOptions{X: e.X / ppt.LogicalDPI, Y: e.Y / ppt.LogicalDPI, Width: e.Width / ppt.LogicalDPI, Height: e.Height / ppt.LogicalDPI}
			if e.Style != nil {
				opts.Fill = styleColor(e.Style, "fill")
				if opts.Fill == "" {
					v := styleString(e.Style, "background")
					if strings.EqualFold(v, "transparent") || strings.EqualFold(v, "none") {
						opts.Fill = "transparent"
					} else if v != "" {
						opts.Fill = v
					}
				}
				opts.LineColor = styleColor(e.Style, "lineColor")
				if v, ok := styleFloat(e.Style, "lineWidth"); ok {
					opts.LineWidth = v * 72 / ppt.LogicalDPI
				}
				if b := styleString(e.Style, "border"); b != "" {
					parts := strings.Fields(b)
					if len(parts) > 0 {
						var n float64
						if _, err := fmt.Sscanf(parts[0], "%fpx", &n); err == nil {
							opts.LineWidth = n * 72 / ppt.LogicalDPI
						}
					}
					for _, part := range parts[1:] {
						if strings.HasPrefix(part, "#") {
							opts.LineColor = part
						} else {
							opts.LineStyle = borderStyleValue(part)
						}
					}
				}
				if e.Style != nil {
					if bo, ok := e.Style["border"].(map[string]any); ok {
						if c, ok := bo["color"].(string); ok {
							opts.LineColor = c
						}
						if w, ok := styleFloat(bo, "width"); ok {
							opts.LineWidth = w * 72 / ppt.LogicalDPI
						}
						if d, ok := bo["style"].(string); ok {
							opts.LineStyle = borderStyleValue(d)
						}
					}
				}
				if v, ok := styleFloat(e.Style, "rotation"); ok {
					opts.Rotate = v
				}
				if v, ok := styleFloat(e.Style, "opacity"); ok {
					opts.Transparency = (1 - v) * 100
				}
			}
			if opts.Fill == "" && e.Fill != nil && e.Fill.Type == "solid" {
				opts.Fill = colorFrom(e.Fill.Color)
				if e.Fill.Color != nil && e.Fill.Color.Alpha > 0 && e.Fill.Color.Alpha < 1 {
					opts.Transparency = (1 - e.Fill.Color.Alpha) * 100
				}
			}
			if opts.Fill == "" && e.Fill != nil && e.Fill.Type == "none" {
				opts.Fill = "transparent"
			}
			if c, alpha := cssColor(opts.LineColor); c != opts.LineColor {
				opts.LineColor = c
				if alpha > 0 && opts.Transparency == 0 {
					opts.Transparency = alpha
				}
			}
			if c, alpha := cssColor(opts.Fill); c != opts.Fill {
				opts.Fill, opts.Transparency = c, alpha
			}
			if strings.EqualFold(opts.Fill, "transparent") || strings.EqualFold(opts.Fill, "none") {
				opts.Fill = "transparent"
			}
			if opts.LineColor == "" && e.Line != nil {
				opts.LineColor = colorFrom(e.Line.Color)
				opts.LineWidth = e.Line.Width * 72 / ppt.LogicalDPI
				opts.LineStyle = borderStyleValue(e.Line.Dash)
			}
			if opts.Rotate == 0 {
				opts.Rotate = e.Rotation
			}
			if e.Opacity > 0 && e.Opacity < 1 && opts.Transparency == 0 {
				opts.Transparency = (1 - e.Opacity) * 100
			}
			if e.Effects != nil && e.Effects.Shadow != "" {
				opts.Shadow = true
			}
			if e.Type == "text" {
				o := genppt.TextOptions{X: e.X / ppt.LogicalDPI, Y: e.Y / ppt.LogicalDPI, Width: e.Width / ppt.LogicalDPI, Height: e.Height / ppt.LogicalDPI}
				if v, ok := styleFloat(e.Style, "fontSize"); ok {
					// genppt expects points; the editor model uses 128 logical px/in.
					o.FontSize = v * 72 / ppt.LogicalDPI
				}
				o.FontColor = styleString(e.Style, "color")
				if o.FontColor == "" {
					o.FontColor = model.Theme.Foreground
				}
				if v, ok := styleFloat(e.Style, "fontWeight"); ok {
					o.Bold = v >= 600
				}
				o.Bold = o.Bold || styleBool(e.Style, "bold")
				o.Italic = styleBool(e.Style, "italic")
				o.Underline = styleBool(e.Style, "underline")
				o.FontFace = styleString(e.Style, "fontFamily")
				o.Rotate = opts.Rotate
				if v, ok := styleFloat(e.Style, "lineHeight"); ok {
					o.LineSpacing = v
				}
				if v, ok := styleFloat(e.Style, "letterSpacing"); ok {
					o.CharSpacing = v * 72 / ppt.LogicalDPI
				}
				o.Align = alignValue(styleString(e.Style, "align"))
				o.VAlign = valignValue(styleString(e.Style, "verticalAlign"))
				if e.Text != nil {
					if o.FontFace == "" {
						o.FontFace = e.Text.FontFamily
					}
					if o.FontSize == 0 {
						o.FontSize = e.Text.FontSize * 72 / ppt.LogicalDPI
					}
					o.Bold = o.Bold || e.Text.Bold
					o.Italic = o.Italic || e.Text.Italic
					o.Underline = o.Underline || e.Text.Underline
					if styleString(e.Style, "align") == "" {
						o.Align = alignValue(e.Text.Align)
					}
					if styleString(e.Style, "verticalAlign") == "" {
						o.VAlign = valignValue(e.Text.VerticalAlign)
					}
					if o.LineSpacing == 0 {
						o.LineSpacing = e.Text.LineHeight
					}
				}
				if v := styleString(e.Style, "background"); v != "" && v != "transparent" {
					o.Fill, _ = cssColor(v)
				}
				if o.Fill == "" && opts.Fill != "" && !strings.EqualFold(opts.Fill, "transparent") && !strings.EqualFold(opts.Fill, "none") {
					o.Fill = opts.Fill
				}
				if len(e.TextRuns) > 0 {
					runs := make([]genppt.RichTextRun, 0, len(e.TextRuns))
					paraAlign := map[int]string{}
					paraLine := map[int]float64{}
					for i, pinfo := range e.TextParagraphs {
						paraAlign[i] = pinfo.Align
						paraLine[i] = pinfo.LineHeight
					}
					if a := paraAlign[e.TextRuns[0].Paragraph]; a != "" {
						o.Align = alignValue(a)
					}
					for _, run := range e.TextRuns {
						r := genppt.RichTextRun{Text: run.Content, Options: o, Paragraph: run.Paragraph}
						if a := paraAlign[run.Paragraph]; a != "" {
							r.Options.Align = alignValue(a)
						}
						if paraLine[run.Paragraph] > 0 {
							r.Options.LineSpacing = paraLine[run.Paragraph]
						}
						if v, ok := styleFloat(run.Style, "fontSize"); ok {
							r.Options.FontSize = v * 72 / ppt.LogicalDPI
						}
						if v := styleString(run.Style, "fontColor"); v != "" {
							r.Options.FontColor = v
						}
						if v := styleString(run.Style, "color"); v != "" {
							r.Options.FontColor = v
						}
						if v := styleString(run.Style, "fontFamily"); v != "" {
							r.Options.FontFace = v
						}
						r.Options.Bold = r.Options.Bold || styleBool(run.Style, "bold")
						r.Options.Italic = r.Options.Italic || styleBool(run.Style, "italic")
						r.Options.Underline = r.Options.Underline || styleBool(run.Style, "underline")
						runs = append(runs, r)
					}
					sl.AddRichText(runs, o)
				} else {
					sl.AddText(e.Content, o)
				}
			} else if e.Type == "shape" {
				shape := genppt.ShapeRect
				shapeName := styleString(e.Style, "shape")
				if shapeName == "" && e.Geometry != nil {
					shapeName = e.Geometry.Preset
				}
				if shapeName != "" {
					switch strings.ToLower(shapeName) {
					case "ellipse", "oval":
						shape = genppt.ShapeEllipse
					case "roundrect":
						shape = genppt.ShapeRoundRect
					case "triangle":
						shape = genppt.ShapeTriangle
					case "diamond":
						shape = genppt.ShapeDiamond
					case "rightarrow":
						shape = genppt.ShapeArrowRight
					case "leftarrow":
						shape = genppt.ShapeArrowLeft
					case "uparrow":
						shape = genppt.ShapeArrowUp
					case "downarrow":
						shape = genppt.ShapeArrowDown
					case "star5":
						shape = genppt.ShapeStar5
					case "heart":
						shape = genppt.ShapeHeart
					case "hexagon":
						shape = genppt.ShapeHexagon
					case "pentagon":
						shape = genppt.ShapePentagon
					case "parallelogram":
						shape = genppt.ShapeParallelogram
					case "righttriangle", "rttriangle":
						shape = genppt.ShapeRightTriangle
					case "line":
						shape = genppt.ShapeLine
					}
				}
				sl.AddShape(shape, opts)
			} else if e.Type == "table" {
				var rows [][]genppt.TableCell
				for _, line := range strings.Split(e.Content, "\n") {
					var row []genppt.TableCell
					for _, c := range strings.Split(line, "\t") {
						row = append(row, genppt.TableCell{Text: c})
					}
					rows = append(rows, row)
				}
				if strings.EqualFold(opts.Fill, "transparent") || strings.EqualFold(opts.Fill, "none") {
					opts.Fill = ""
				}
				to := genppt.TableOptions{X: opts.X, Y: opts.Y, Width: opts.Width, Fill: opts.Fill, FontColor: styleString(e.Style, "color"), Border: genppt.Border{Color: opts.LineColor, Width: opts.LineWidth}}
				if v, ok := styleFloat(e.Style, "fontSize"); ok {
					to.FontSize = v * 72 / ppt.LogicalDPI
				}
				to.FontFace = styleString(e.Style, "fontFamily")
				to.FirstRowBold = styleBool(e.Style, "firstRowBold")
				to.FirstRowFill = styleString(e.Style, "firstRowFill")
				sl.AddTable(rows, to)
			} else if e.Type == "image" && e.AssetURL != "" {
				if i := strings.Index(e.AssetURL, ","); i >= 0 {
					if data, err := base64.StdEncoding.DecodeString(e.AssetURL[i+1:]); err == nil {
						original := data
						data = cropImageData(data, e.Style)
						x, y, w, h := e.X/ppt.LogicalDPI, e.Y/ppt.LogicalDPI, e.Width/ppt.LogicalDPI, e.Height/ppt.LogicalDPI
						// Cropped images are already normalized to the visible frame;
						// preserve that frame exactly. For uncropped images retain the
						// editor's contain behavior.
						if bytes.Equal(original, data) {
							x, y, w, h = fitImageBox(data, x, y, w, h)
						}
						sl.AddImage(genppt.ImageOptions{X: x, Y: y, Width: w, Height: h, Data: data, Rotate: e.Rotation})
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
