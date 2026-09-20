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
	"os"
	"path/filepath"
	"strings"

	genppt "github.com/CoolBanHub/genppt"
)

type pptPlanPage struct {
	Title       string         `json:"title"`
	Bullets     []string       `json:"bullets,omitempty"`
	Layout      string         `json:"layout,omitempty"`
	ImagePrompt string         `json:"imagePrompt,omitempty"`
	Blocks      []pptPlanBlock `json:"blocks,omitempty"`
}
type pptPlanBlock struct {
	Type        string  `json:"type"` // text or image
	Role        string  `json:"role,omitempty"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Width       float64 `json:"width"`
	Height      float64 `json:"height"`
	Content     string  `json:"content,omitempty"`
	ImagePrompt string  `json:"imagePrompt,omitempty"`
}
type pptPlan struct {
	Title             string        `json:"title"`
	Style             string        `json:"style"`
	TextModel         string        `json:"textModel,omitempty"`
	ImageModel        string        `json:"imageModel,omitempty"`
	Audience          string        `json:"audience,omitempty"`
	References        []string      `json:"references,omitempty"`
	Images            []string      `json:"images,omitempty"`
	Pages             []pptPlanPage `json:"pages"`
	AccentColor       string        `json:"accentColor,omitempty"`
	BackgroundColor   string        `json:"backgroundColor,omitempty"`
	UnifiedBackground bool          `json:"unifiedBackground,omitempty"`
	StyleReference    string        `json:"styleReference,omitempty"`
}

// pptPlanDSL is intentionally small: the text model only needs to describe
// editable copy, layout and an optional image brief. Rendering details belong
// to the server-side Presentation DSL.
const pptPlanDSL = `PPT DSL（仅输出 JSON，不要代码块）：
{"pages":[{"title":"页面标题","layout":"split","blocks":[{"type":"text","role":"title","x":0.06,"y":0.08,"width":0.88,"height":0.12,"content":"页面标题"},{"type":"text","role":"body","x":0.08,"y":0.28,"width":0.42,"height":0.48,"content":"要点1\n要点2"},{"type":"image","role":"hero","x":0.56,"y":0.20,"width":0.36,"height":0.60,"imagePrompt":"主体、构图和风格"}]}]}
约束：坐标和尺寸使用 0~1 的页面比例；blocks 描述完整图文排版，type 只能是 text 或 image；text 必须有 content，image 必须有 imagePrompt；role 可用 title、body、caption、hero、icon；layout 可用 title、content、split、hero、timeline。imagePrompt 只描述画面，不包含需要渲染的文字。`

// handlePPTPlan creates a confirmation-ready brief with the selected text model.
func (s *webUIServer) handlePPTPlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var in struct {
		Title, Style, Audience                       string
		TextModel                                    string `json:"textModel"`
		ImageModel                                   string `json:"imageModel"`
		References, Images                           []string
		PageCount                                    int `json:"pageCount"`
		AccentColor, BackgroundColor, StyleReference string
		UnifiedBackground                            bool `json:"unifiedBackground"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&in); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	title := strings.TrimSpace(in.Title)
	style := strings.TrimSpace(in.Style)
	n := in.PageCount
	if n < 3 {
		n = 3
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
	prompt := fmt.Sprintf(`请为“%s”生成一份单页/多页演示文稿的 PPT DSL 草案。内容要求：%s。目标受众：%s。页数：%d 页。视觉风格：%s。
%s
文字与图片提示词分离，先保证文字内容完整。必须恰好返回 %d 页，第一页 layout 必须为 title。`, title, strings.TrimSpace(strings.Join(in.References, "\n")), strings.TrimSpace(in.Audience), n, style, pptPlanDSL, n)
	streamMode := r.URL.Query().Get("stream") == "1" || strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream")
	var resp *responsePayload
	var stream *webUIEventStream
	if streamMode {
		stream = newWebUIEventStream(w)
		stream.send("status", map[string]string{"message": "正在连接大模型，逐字生成大纲…"})
		resp, err = c.createResponseStream(r.Context(), responseRequest{Model: model, Input: prompt}, stream.textWriter())
	} else {
		resp, err = c.createResponse(r.Context(), responseRequest{Model: model, Input: prompt})
	}
	if err != nil {
		if streamMode {
			stream.send("error", map[string]string{"error": err.Error()})
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if resp == nil {
		if streamMode {
			stream.send("error", map[string]string{"error": "text model returned empty response"})
			return
		}
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
		if streamMode {
			stream.send("error", map[string]string{"error": "text model returned invalid PPT outline"})
			return
		}
		writeError(w, http.StatusBadGateway, "text model returned invalid PPT outline")
		return
	}
	if len(generated.Pages) > n {
		generated.Pages = generated.Pages[:n]
	}
	// Models occasionally stop early after emitting a valid first page. Keep
	// the user's requested page count stable and make the missing pages
	// editable placeholders instead of silently returning a one-page deck.
	for len(generated.Pages) < n {
		i := len(generated.Pages) + 1
		generated.Pages = append(generated.Pages, pptPlanPage{
			Title:       fmt.Sprintf("第 %d 页", i),
			Bullets:     []string{"待补充内容"},
			Layout:      "content",
			ImagePrompt: fmt.Sprintf("第 %d 页，%s风格的简洁插画", i, style),
		})
	}
	for i := range generated.Pages {
		// Derive the legacy editor summary fields from the layout blocks so the
		// outline confirmation UI can still display and edit the DSL content.
		for _, b := range generated.Pages[i].Blocks {
			if b.Type != "text" {
				continue
			}
			if b.Role == "title" && strings.TrimSpace(generated.Pages[i].Title) == "" {
				generated.Pages[i].Title = b.Content
			}
			if b.Role != "title" && strings.TrimSpace(b.Content) != "" {
				generated.Pages[i].Bullets = append(generated.Pages[i].Bullets, strings.Split(b.Content, "\n")...)
			}
		}
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
	result := pptPlan{Title: title, Style: style, TextModel: model, ImageModel: strings.TrimSpace(in.ImageModel), Audience: strings.TrimSpace(in.Audience), References: in.References, Images: in.Images, Pages: pages, AccentColor: strings.TrimSpace(in.AccentColor), BackgroundColor: strings.TrimSpace(in.BackgroundColor), UnifiedBackground: in.UnifiedBackground, StyleReference: strings.TrimSpace(in.StyleReference)}
	if streamMode {
		stream.send("done", result)
		return
	}
	writeJSON(w, 200, result)
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
	p.AIModel = plan.TextModel
	p.AIImageModel = plan.ImageModel
	p.Theme.Name = plan.Style
	if strings.Contains(strings.ToLower(plan.Style), "深") {
		p.Theme.Background = "#0F172A"
		p.Theme.Foreground = "#F8FAFC"
		p.Theme.Accent = "#38BDF8"
	}
	// User-selected colors are applied after presets so the style sample is
	// faithfully carried into the generated deck.
	if plan.BackgroundColor != "" {
		p.Theme.Background = plan.BackgroundColor
	}
	if plan.AccentColor != "" {
		p.Theme.Accent = plan.AccentColor
	}
	for i, page := range plan.Pages {
		elems := make([]ppt.Element, 0, len(page.Blocks)+2)
		if len(page.Blocks) > 0 {
			for bi, block := range page.Blocks {
				if block.Type != "text" && block.Type != "image" {
					continue
				}
				x, y, w, h := block.X*p.Width, block.Y*p.Height, block.Width*p.Width, block.Height*p.Height
				if w <= 0 {
					w = 300
				}
				if h <= 0 {
					h = 100
				}
				if block.Type == "text" {
					// Editor font sizes are logical CSS pixels (128 px/in). Use
					// 32/48 px so the exported 18/27 pt text remains readable in
					// PowerPoint after the px-to-point conversion.
					style := map[string]any{"fontSize": 32.0, "lineHeight": 1.5, "color": p.Theme.Foreground}
					if block.Role == "title" {
						style["fontSize"], style["fontWeight"], style["color"] = 48.0, 700, p.Theme.Accent
					}
					elems = append(elems, ppt.Element{ID: fmt.Sprintf("slide-%d-block-%d", i+1, bi+1), Type: "text", X: x, Y: y, Width: w, Height: h, Content: block.Content, Style: style})
				} else {
					elems = append(elems, ppt.Element{ID: fmt.Sprintf("slide-%d-block-%d", i+1, bi+1), Type: "image", X: x, Y: y, Width: w, Height: h, Content: block.ImagePrompt, AssetURL: illustrationDataURL(block.ImagePrompt), Style: map[string]any{"background": "transparent", "objectFit": "contain"}})
				}
			}
		}
		if len(elems) == 0 {
			elems = append(elems, ppt.Element{ID: fmt.Sprintf("slide-%d-title", i+1), Type: "text", X: 72, Y: 56, Width: 1136, Height: 72, Content: page.Title, Style: map[string]any{"fontSize": 48, "fontWeight": 700, "color": p.Theme.Accent}})
			body := strings.Join(page.Bullets, "\n")
			elems = append(elems, ppt.Element{ID: fmt.Sprintf("slide-%d-body", i+1), Type: "text", X: 92, Y: 164, Width: 650, Height: 360, Content: body, Style: map[string]any{"fontSize": 32, "lineHeight": 1.5, "color": p.Theme.Foreground}})
		}
		asset := ""
		if i < len(plan.Images) {
			asset = plan.Images[i]
		}
		hasImage := false
		for j := range elems {
			if elems[j].Type == "image" {
				hasImage = true
				if asset != "" {
					elems[j].AssetURL = asset
				}
				break
			}
		}
		if !hasImage && asset == "" && page.ImagePrompt != "" {
			asset = illustrationDataURL(page.ImagePrompt)
		}
		if asset != "" && !hasImage {
			elems = append(elems, ppt.Element{ID: fmt.Sprintf("slide-%d-image", i+1), Type: "image", X: 790, Y: 170, Width: 400, Height: 300, Content: page.ImagePrompt, AssetURL: asset, Style: map[string]any{"background": "transparent", "objectFit": "contain"}})
		}
		p.Slides = append(p.Slides, ppt.Slide{ID: fmt.Sprintf("slide-%d", i+1), Layout: page.Layout, Elements: elems})
	}
	writeJSON(w, 200, p)
}

// handlePPTImages generates images after the text/layout phase. Each slide is
// independent: an image failure is returned in errors while successful images
// are applied to the presentation.
func (s *webUIServer) handlePPTImages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var in struct {
		Presentation ppt.Presentation `json:"presentation"`
		SlideIndex   *int             `json:"slideIndex,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<20)).Decode(&in); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	imageModel := strings.TrimSpace(in.Presentation.AIImageModel)
	if imageModel == "" && s.clientOpts.imageModel != nil {
		imageModel = strings.TrimSpace(*s.clientOpts.imageModel)
	}
	if imageModel == "" {
		writeJSON(w, 200, map[string]any{"presentation": in.Presentation, "errors": []string{"image model is not configured"}})
		return
	}
	c, err := s.clientOpts.newClient()
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer c.Close()
	indices := make([]int, 0, len(in.Presentation.Slides))
	if in.SlideIndex != nil {
		if *in.SlideIndex < 0 || *in.SlideIndex >= len(in.Presentation.Slides) {
			writeError(w, 400, "slideIndex out of range")
			return
		}
		indices = append(indices, *in.SlideIndex)
	} else {
		for i := range in.Presentation.Slides {
			indices = append(indices, i)
		}
	}
	errs := []string{}
	for _, i := range indices {
		slide := &in.Presentation.Slides[i]
		var target *ppt.Element
		for j := range slide.Elements {
			if slide.Elements[j].Type == "image" {
				target = &slide.Elements[j]
				break
			}
		}
		if target == nil || strings.TrimSpace(target.Content) == "" {
			continue
		}
		resp, genErr := c.createImageGeneration(r.Context(), imageGenerationRequest{Model: imageModel, Prompt: target.Content, N: 1, Size: "1024x1024"})
		if genErr != nil {
			errs = append(errs, fmt.Sprintf("slide %d: %v", i+1, genErr))
			continue
		}
		saved, saveErr := saveImagesFromResponse(r.Context(), c.httpClient, resp, s.saveDirectory(), false, "")
		if saveErr != nil || len(saved) == 0 {
			if saveErr == nil {
				saveErr = fmt.Errorf("empty image response")
			}
			errs = append(errs, fmt.Sprintf("slide %d: %v", i+1, saveErr))
			continue
		}
		if urls := s.publicImageURLs(saved, s.saveDirectory()); len(urls) > 0 {
			target.AssetURL = urls[0]
			adjustImageElementAspect(target, saved[0])
		} else {
			errs = append(errs, fmt.Sprintf("slide %d: saved image is not publicly accessible", i+1))
		}
	}
	writeJSON(w, 200, map[string]any{"presentation": in.Presentation, "errors": errs})
}

func adjustImageElementAspect(e *ppt.Element, path string) {
	if e == nil || path == "" {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return
	}
	if e.Style == nil {
		e.Style = map[string]any{}
	}
	boxW, boxH := e.Width, e.Height
	if v, ok := e.Style["frameWidth"].(float64); ok && v > 0 {
		boxW = v
	}
	if v, ok := e.Style["frameHeight"].(float64); ok && v > 0 {
		boxH = v
	}
	if _, ok := e.Style["frameWidth"]; !ok {
		e.Style["frameWidth"], e.Style["frameHeight"], e.Style["frameX"], e.Style["frameY"] = boxW, boxH, e.X, e.Y
	}
	baseX, baseY := e.X, e.Y
	if v, ok := e.Style["frameX"].(float64); ok {
		baseX = v
	}
	if v, ok := e.Style["frameY"].(float64); ok {
		baseY = v
	}
	ratio := float64(cfg.Width) / float64(cfg.Height)
	if ratio <= 0 {
		return
	}
	if boxW/boxH > ratio {
		e.Width = boxH * ratio
		e.Height = boxH
		e.X = baseX + (boxW-e.Width)/2
		e.Y = baseY
	} else {
		e.Height = boxW / ratio
		e.Width = boxW
		e.X = baseX
		e.Y = baseY + (boxH-e.Height)/2
	}
}

// handlePPTSlideGenerate regenerates one slide using the selected text model.
// The editor sends the current deck so the operation is independently repeatable.
func (s *webUIServer) handlePPTSlideGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var in struct {
		Presentation ppt.Presentation `json:"presentation"`
		SlideIndex   int              `json:"slideIndex"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&in); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if in.SlideIndex < 0 || in.SlideIndex >= len(in.Presentation.Slides) {
		writeError(w, 400, "slideIndex out of range")
		return
	}
	model := strings.TrimSpace(in.Presentation.AIModel)
	if model == "" && s.clientOpts.model != nil {
		model = strings.TrimSpace(*s.clientOpts.model)
	}
	page := in.Presentation.Slides[in.SlideIndex]
	title, body := "", ""
	for _, e := range page.Elements {
		if e.Type == "text" && title == "" {
			title = e.Content
		} else if e.Type == "text" {
			body = e.Content
		}
	}
	streamMode := r.URL.Query().Get("stream") == "1" || strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream")
	var stream *webUIEventStream
	if streamMode {
		stream = newWebUIEventStream(w)
		stream.send("status", map[string]string{"message": "正在生成本页内容…"})
	}
	if model != "" {
		if c, err := s.clientOpts.newClient(); err == nil {
			defer c.Close()
			prompt := fmt.Sprintf(`为 PPT 第 %d 页重新设计具体视觉布局。当前标题：%s。当前内容：%s。主题：%s；背景色：%s；强调色：%s。必须保留当前主题和内容含义，只重新安排版式并补充配图描述。只返回 JSON：{"layout":"content|split|hero|timeline","title":"标题","bullets":["要点"],"imagePrompt":"配图描述"}。`, in.SlideIndex+1, title, body, in.Presentation.Theme.Name, in.Presentation.Theme.Background, in.Presentation.Theme.Accent)
			var resp *responsePayload
			var genErr error
			if streamMode {
				resp, genErr = c.createResponseStream(r.Context(), responseRequest{Model: model, Input: prompt}, stream.textWriter())
			} else {
				resp, genErr = c.createResponse(r.Context(), responseRequest{Model: model, Input: prompt})
			}
			if genErr != nil {
				if streamMode {
					stream.send("error", map[string]string{"error": genErr.Error()})
					return
				}
				writeError(w, http.StatusBadGateway, genErr.Error())
				return
			}
			if resp == nil {
				if streamMode {
					stream.send("error", map[string]string{"error": "text model returned empty response"})
					return
				}
				writeError(w, http.StatusBadGateway, "text model returned empty response")
				return
			}
			{
				var out pptPlanPage
				text := strings.TrimSpace(resp.text())
				validOut := false
				if a, b := strings.Index(text, "{"), strings.LastIndex(text, "}"); a >= 0 && b > a {
					validOut = json.Unmarshal([]byte(text[a:b+1]), &out) == nil
				}
				if !validOut {
					if streamMode {
						stream.send("error", map[string]string{"error": "text model returned invalid slide data"})
						return
					}
					writeError(w, http.StatusBadGateway, "text model returned invalid slide data")
					return
				}
				if out.Title != "" {
					title = out.Title
				}
				body = strings.Join(out.Bullets, "\n")
				if body == "" {
					body = out.ImagePrompt
				}
				page.Layout = out.Layout
				page.Elements = []ppt.Element{{ID: fmt.Sprintf("slide-%d-title", in.SlideIndex+1), Type: "text", X: 72, Y: 56, Width: 1136, Height: 72, Content: title, Style: map[string]any{"fontSize": 48, "fontWeight": 700, "color": in.Presentation.Theme.Foreground}}, {ID: fmt.Sprintf("slide-%d-body", in.SlideIndex+1), Type: "text", X: 92, Y: 164, Width: 650, Height: 360, Content: body, Style: map[string]any{"fontSize": 32, "lineHeight": 1.5, "color": in.Presentation.Theme.Foreground}}}
				if out.ImagePrompt != "" {
					page.Elements = append(page.Elements, ppt.Element{ID: fmt.Sprintf("slide-%d-image", in.SlideIndex+1), Type: "image", X: 790, Y: 170, Width: 400, Height: 300, AssetURL: illustrationDataURL(out.ImagePrompt)})
				}
				imageModel := strings.TrimSpace(in.Presentation.AIImageModel)
				if imageModel == "" && s.clientOpts.imageModel != nil {
					imageModel = strings.TrimSpace(*s.clientOpts.imageModel)
				}
				if imageModel != "" && out.ImagePrompt != "" && len(page.Elements) > 0 {
					if imgResp, imgErr := c.createImageGeneration(r.Context(), imageGenerationRequest{Model: imageModel, Prompt: out.ImagePrompt, N: 1, Size: "1024x1024"}); imgErr != nil {
						if streamMode {
							// Keep the regenerated text/layout even when the optional
							// image phase fails. The client can retry the image alone.
							stream.send("status", map[string]string{"message": "配图生成失败，已保留文字内容"})
						}
					} else if imgResp != nil {
						if saved, saveErr := saveImagesFromResponse(r.Context(), c.httpClient, imgResp, s.saveDirectory(), false, ""); saveErr != nil {
							// Image generation is best-effort; keep the regenerated text slide
							// usable even when the image provider or storage fails.
							if streamMode {
								stream.send("status", map[string]string{"message": "配图生成失败，已保留文字内容"})
							}
						} else if urls := s.publicImageURLs(saved, s.saveDirectory()); len(urls) > 0 {
							page.Elements[len(page.Elements)-1].AssetURL = urls[0]
							adjustImageElementAspect(&page.Elements[len(page.Elements)-1], saved[0])
						}
					}
				}
			}
		} else if streamMode {
			stream.send("error", map[string]string{"error": err.Error()})
			return
		} else {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
	} else if streamMode {
		stream.send("error", map[string]string{"error": "text model is not configured"})
		return
	}
	in.Presentation.Slides[in.SlideIndex] = page
	if streamMode {
		stream.send("done", map[string]any{"slide": page, "slideIndex": in.SlideIndex})
		return
	}
	writeJSON(w, 200, map[string]any{"slide": page, "slideIndex": in.SlideIndex})
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
	case "center", "centre", "ctr", "text-align-center":
		return genppt.AlignCenter
	case "right", "end", "r", "text-align-right":
		return genppt.AlignRight
	case "justify", "distributed", "dist", "just", "text-align-justify":
		return genppt.AlignJustify
	default:
		return genppt.AlignLeft
	}
}

func elementAlign(style map[string]any) string {
	if v := styleString(style, "align"); v != "" {
		return v
	}
	if v := styleString(style, "textAlign"); v != "" {
		return v
	}
	return styleString(style, "text-align")
}

// svgDataURLBytes accepts both base64 data URLs and the URL-escaped SVG URLs
// produced by the editor's generated artwork.
func svgDataURLBytes(value string) []byte {
	if !strings.HasPrefix(strings.ToLower(value), "data:") {
		return nil
	}
	comma := strings.IndexByte(value, ',')
	if comma < 0 {
		return nil
	}
	header, payload := strings.ToLower(value[:comma]), value[comma+1:]
	if strings.Contains(header, ";base64") {
		b, _ := base64.StdEncoding.DecodeString(payload)
		return b
	}
	b, err := url.PathUnescape(payload)
	if err != nil {
		return []byte(payload)
	}
	return []byte(b)
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

func elementVAlign(style map[string]any) string {
	if v := styleString(style, "verticalAlign"); v != "" {
		return v
	}
	if v := styleString(style, "verticalAlignment"); v != "" {
		return v
	}
	if v := styleString(style, "vAlign"); v != "" {
		return v
	}
	if v := styleString(style, "vertical-align"); v != "" {
		return v
	}
	if v := styleString(style, "vertical"); v != "" {
		return v
	}
	return styleString(style, "textVerticalAlign")
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

func customGeometrySVG(e ppt.Element, opts genppt.ShapeOptions) []byte {
	if e.Geometry == nil || strings.TrimSpace(e.Geometry.Path) == "" {
		return nil
	}
	viewBox := e.Geometry.ViewBox
	if viewBox == "" {
		viewBox = "0 0 1000 1000"
	}
	attr := func(v string) string {
		return strings.NewReplacer("&", "&amp;", "\"", "&quot;", "<", "&lt;", ">", "&gt;").Replace(v)
	}
	// Image fills must live inside the SVG path.  Adding the source image as a
	// separate PPTX picture would lose the custom geometry and produce a
	// rectangle when the file is opened in PowerPoint.
	imageHref := ""
	if e.Fill != nil && e.Fill.Type == "image" && e.Fill.Image != nil {
		imageHref = strings.TrimSpace(e.Fill.Image.AssetURL)
	}
	if imageHref == "" && e.Style != nil {
		if bg, ok := e.Style["background"].(string); ok && strings.HasPrefix(strings.ToLower(strings.TrimSpace(bg)), "data:image/") {
			imageHref = strings.TrimSpace(bg)
		}
	}
	fill := opts.Fill
	if imageHref != "" {
		fill = "none"
	}
	if fill == "" {
		fill = "none"
	}
	stroke, sw := opts.LineColor, opts.LineWidth
	if stroke == "" || sw <= 0 {
		stroke, sw = "none", 0
	}
	defs := ""
	image := ""
	if imageHref != "" {
		// Clip a normal SVG image with the custom path. This is better supported
		// by PowerPoint than pattern fills, while still preserving the geometry.
		defs = fmt.Sprintf(`<defs><clipPath id="ppt-image-clip"><path d="%s"/></clipPath></defs>`, attr(e.Geometry.Path))
		// Keep the legacy xlink form first; Office versions that support SVG
		// often ignore the newer href-only form for embedded image data.
		image = fmt.Sprintf(`<image xlink:href="%s" href="%s" x="0" y="0" width="100%%" height="100%%" preserveAspectRatio="none" clip-path="url(#ppt-image-clip)"/>`, attr(imageHref), attr(imageHref))
	}
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="%s" preserveAspectRatio="none">%s%s<path d="%s" fill="%s" stroke="%s" stroke-width="%g"/></svg>`, attr(viewBox), defs, image, attr(e.Geometry.Path), attr(fill), attr(stroke), sw)
	return []byte(svg)
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
			bg := model.Theme.Background
			if strings.HasPrefix(strings.ToLower(bg), "data:image/") {
				// genppt exposes solid slide backgrounds only. Materialize an
				// image fill as a full-slide image so it remains editable and is
				// preserved in the exported PPTX.
				if comma := strings.Index(bg, ","); comma >= 0 {
					if data, decErr := base64.StdEncoding.DecodeString(bg[comma+1:]); decErr == nil && len(data) > 0 {
						sl.AddImage(genppt.ImageOptions{X: 0, Y: 0, Width: model.Width / ppt.LogicalDPI, Height: model.Height / ppt.LogicalDPI, Data: data})
					}
				}
			} else {
				sl.SetBackground(genppt.BackgroundOptions{Color: bg})
			}
		}
		for _, e := range sld.Elements {
			opts := genppt.ShapeOptions{X: e.X / ppt.LogicalDPI, Y: e.Y / ppt.LogicalDPI, Width: e.Width / ppt.LogicalDPI, Height: e.Height / ppt.LogicalDPI}
			// genppt intentionally supplies Office defaults for ordinary shapes.
			// The editor model, however, distinguishes an unset fill/border from
			// a requested one, so make the absence explicit before adding a shape.
			opts.Fill = "transparent"
			opts.LineStyle = genppt.BorderNone
			explicitBorder := false
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
					explicitBorder = true
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
						explicitBorder = true
						if c, ok := bo["color"].(string); ok {
							opts.LineColor = c
						}
						if w, ok := styleFloat(bo, "width"); ok {
							opts.LineWidth = w * 72 / ppt.LogicalDPI
						}
						if d, ok := bo["style"].(string); ok {
							opts.LineStyle = borderStyleValue(d)
						}
						if w, ok := styleFloat(bo, "width"); ok && w <= 0 {
							opts.LineColor = ""
							opts.LineWidth = 0
							opts.LineStyle = genppt.BorderNone
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
			if opts.LineColor == "" && e.Line != nil && !explicitBorder {
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
			// A custom geometry embeds its image fill in the SVG path below. For
			// ordinary elements retain the existing editable picture behavior.
			if (e.Geometry == nil || e.Geometry.Type != "custom") && e.Fill != nil && e.Fill.Type == "image" && e.Fill.Image != nil {
				if data := dataURLBytes(e.Fill.Image.AssetURL); len(data) > 0 {
					sl.AddImage(genppt.ImageOptions{X: opts.X, Y: opts.Y, Width: opts.Width, Height: opts.Height, Data: data, Rotate: e.Rotation})
					opts.Fill = "transparent"
				}
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
				align := elementAlign(e.Style)
				o.Align = alignValue(align)
				vAlign := elementVAlign(e.Style)
				if vAlign == "" && e.Text != nil {
					vAlign = e.Text.VerticalAlign
				}
				// The web canvas lays text out from the top (the element has its
				// own 8px padding). Only an explicit middle/center value should
				// become PowerPoint's ctr anchor; defaulting to ctr makes exported
				// text appear noticeably lower than it does on the canvas.
				if vAlign == "" {
					vAlign = "top"
				}
				o.VAlign = valignValue(vAlign)
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
					if align == "" {
						o.Align = alignValue(e.Text.Align)
					}
					// vAlign was resolved from the element or text style above.
					if o.LineSpacing == 0 {
						o.LineSpacing = e.Text.LineHeight
					}
				}
				if v := styleString(e.Style, "background"); v != "" && v != "transparent" && !strings.HasPrefix(strings.ToLower(v), "data:image/") {
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
				if e.Style == nil {
					e.Style = map[string]any{}
				}
				// Emit custom paths as native DrawingML geometry. This keeps the
				// shape editable and lets PowerPoint apply image fills directly.
				if e.Geometry != nil && e.Geometry.Type == "custom" {
					customOpts := genppt.CustomGeometryOptions{X: opts.X, Y: opts.Y, Width: opts.Width, Height: opts.Height, Path: e.Geometry.Path, ViewBox: e.Geometry.ViewBox, Fill: opts.Fill, LineColor: opts.LineColor, LineWidth: opts.LineWidth, Transparency: opts.Transparency, Rotate: opts.Rotate}
					if e.Fill != nil && e.Fill.Type == "image" && e.Fill.Image != nil {
						customOpts.ImageData = dataURLBytes(e.Fill.Image.AssetURL)
					}
					sl.AddCustomGeometry(customOpts)
					continue
				}
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
				if shape == genppt.ShapeRoundRect {
					radius := 12.0
					if v, ok := styleFloat(e.Style, "radius"); ok {
						radius = v
					}
					if radius < 0 {
						radius = 0
					}
					if radius > 50 {
						radius = 50
					}
					opts.Rounding = radius
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
				var data []byte
				var readErr error
				if i := strings.Index(e.AssetURL, ","); i >= 0 {
					data, readErr = base64.StdEncoding.DecodeString(e.AssetURL[i+1:])
				} else if strings.HasPrefix(e.AssetURL, "/outputs/") {
					rel := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(e.AssetURL, "/outputs/")))
					if rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
						data, readErr = os.ReadFile(filepath.Join(s.saveDirectory(), rel))
					}
				}
				if readErr == nil && len(data) > 0 {
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
	b, err := p.ToBytes()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.presentationml.presentation")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", "presentation.pptx"))
	w.Write(b)
}

func dataURLBytes(value string) []byte {
	return svgDataURLBytes(value)
}
