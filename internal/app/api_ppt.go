package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"kuaima_cli/internal/ppt"
	"kuaima_cli/internal/ppt/pptximport"
	"math"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	genppt "github.com/CoolBanHub/genppt"
)

func cropPPTOCRImage(ctx context.Context, asset string, x, y, w, h, canvasW, canvasH float64) (string, error) {
	var data []byte
	if strings.HasPrefix(asset, "data:image/") {
		comma := strings.IndexByte(asset, ',')
		if comma < 0 {
			return "", fmt.Errorf("invalid data URL")
		}
		var err error
		data, err = base64.StdEncoding.DecodeString(asset[comma+1:])
		if err != nil {
			return "", err
		}
	} else {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset, nil)
		if err != nil {
			return "", err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("image download failed: %s", resp.Status)
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		if err != nil {
			return "", err
		}
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	if canvasW <= 0 || canvasH <= 0 {
		return "", fmt.Errorf("invalid canvas size")
	}
	if w <= 1 || h <= 1 {
		x, y, w, h = autoPPTOCRRegion(src, x, y, canvasW, canvasH)
	}
	left := int(x * float64(src.Bounds().Dx()) / canvasW)
	top := int(y * float64(src.Bounds().Dy()) / canvasH)
	right := int((x + w) * float64(src.Bounds().Dx()) / canvasW)
	bottom := int((y + h) * float64(src.Bounds().Dy()) / canvasH)
	left = maxOCRInt(0, minInt(left, src.Bounds().Dx()-1))
	top = maxOCRInt(0, minInt(top, src.Bounds().Dy()-1))
	right = maxOCRInt(left+1, minInt(right, src.Bounds().Dx()))
	bottom = maxOCRInt(top+1, minInt(bottom, src.Bounds().Dy()))
	out := image.NewRGBA(image.Rect(0, 0, right-left, bottom-top))
	draw.Draw(out, out.Bounds(), src, image.Point{X: left + src.Bounds().Min.X, Y: top + src.Bounds().Min.Y}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return "", err
	}
	return imageDataURL("image/png", buf.Bytes()), nil
}

// autoPPTOCRRegion finds the text line nearest the click using the image's
// edge density. It deliberately has no fixed crop size: the returned box is
// derived from the actual glyph pixels in the slide image.
func autoPPTOCRRegion(src image.Image, x, y, canvasW, canvasH float64) (float64, float64, float64, float64) {
	b := src.Bounds()
	if b.Dx() < 2 || b.Dy() < 2 || canvasW <= 0 || canvasH <= 0 {
		return 0, 0, canvasW, canvasH
	}
	px := int(float64(b.Dx()) * x / canvasW)
	py := int(float64(b.Dy()) * y / canvasH)
	px = maxOCRInt(0, minInt(px, b.Dx()-1))
	py = maxOCRInt(0, minInt(py, b.Dy()-1))
	w, h := b.Dx(), b.Dy()
	gray := make([]uint8, w*h)
	for yy := 0; yy < h; yy++ {
		for xx := 0; xx < w; xx++ {
			r, g, bl, _ := src.At(b.Min.X+xx, b.Min.Y+yy).RGBA()
			gray[yy*w+xx] = color.GrayModel.Convert(color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8), 255}).(color.Gray).Y
		}
	}
	edge := make([]uint8, w*h)
	bestDist, seedX, seedY := int(^uint(0)>>1), px, py
	maxGradient := 0
	gradients := make([]int, w*h)
	for yy := 1; yy < h-1; yy++ {
		for xx := 1; xx < w-1; xx++ {
			a := int(gray[(yy-1)*w+xx-1]) + 2*int(gray[(yy-1)*w+xx]) + int(gray[(yy-1)*w+xx+1])
			c := int(gray[(yy+1)*w+xx-1]) + 2*int(gray[(yy+1)*w+xx]) + int(gray[(yy+1)*w+xx+1])
			l := int(gray[(yy-1)*w+xx-1]) + 2*int(gray[yy*w+xx-1]) + int(gray[(yy+1)*w+xx-1])
			r := int(gray[(yy-1)*w+xx+1]) + 2*int(gray[yy*w+xx+1]) + int(gray[(yy+1)*w+xx+1])
			v := absInt(a-c) + absInt(l-r)
			gradients[yy*w+xx] = v
			if v > maxGradient {
				maxGradient = v
			}
		}
	}
	gradientThreshold := maxOCRInt(24, maxGradient/8)
	for yy := 1; yy < h-1; yy++ {
		for xx := 1; xx < w-1; xx++ {
			v := gradients[yy*w+xx]
			if v >= gradientThreshold {
				edge[yy*w+xx] = 1
				d := (xx-px)*(xx-px) + (yy-py)*(yy-py)
				if d < bestDist {
					bestDist, seedX, seedY = d, xx, yy
				}
			}
		}
	}
	if bestDist == int(^uint(0)>>1) {
		return 0, 0, canvasW, canvasH
	}
	row := make([]int, h)
	col := make([]int, w)
	for yy := 0; yy < h; yy++ {
		for xx := 0; xx < w; xx++ {
			if edge[yy*w+xx] != 0 {
				row[yy]++
				col[xx]++
			}
		}
	}
	peak := row[seedY]
	if peak < 1 {
		peak = 1
	}
	threshold := maxOCRInt(3, peak/3)
	top, bottom := seedY, seedY
	for top > 1 && row[top-1] >= threshold {
		top--
	}
	for bottom < h-2 && row[bottom+1] >= threshold {
		bottom++
	}
	left, right := seedX, seedX
	for xx := 0; xx < w; xx++ {
		n := 0
		for yy := top; yy <= bottom; yy++ {
			n += int(edge[yy*w+xx])
		}
		if n > 0 {
			if xx < left {
				left = xx
			}
			if xx > right {
				right = xx
			}
		}
	}
	padX, padY := maxOCRInt(2, (right-left+1)/8), maxOCRInt(2, (bottom-top+1)/4)
	left = maxOCRInt(0, left-padX)
	right = minInt(w-1, right+padX)
	top = maxOCRInt(0, top-padY)
	bottom = minInt(h-1, bottom+padY)
	return float64(left) * canvasW / float64(w), float64(top) * canvasH / float64(h), float64(right-left+1) * canvasW / float64(w), float64(bottom-top+1) * canvasH / float64(h)
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func maxOCRInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func nextPPTOCRZ(elements []ppt.Element) int {
	max := -1
	for _, e := range elements {
		if e.ZIndex > max {
			max = e.ZIndex
		}
	}
	return max + 1
}

func ocrFontSize(height float64, text string, detected float64) float64 {
	lines := float64(strings.Count(text, "\n") + 1)
	minSize := height / lines * 0.62
	if detected <= 0 || detected < minSize*0.75 {
		detected = minSize
	}
	return detected
}

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
	MasterImage       string        `json:"masterImage,omitempty"`
	UnifiedPrompt     string        `json:"unifiedPrompt,omitempty"`
	OutlinePrompt     string        `json:"outlinePrompt,omitempty"`
	ImageOnly         bool          `json:"imageOnly,omitempty"`
}

// handlePPTMaster generates one visual master image. It is deliberately
// separate from outline generation so the user can approve the visual
// language before any slide images are created.
func (s *webUIServer) handlePPTMaster(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var in struct{ Title, Style, Brief, ImageModel, StyleReference, UnifiedPrompt string }
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&in); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	model := strings.TrimSpace(in.ImageModel)
	if model == "" && s.clientOpts.imageModel != nil {
		model = strings.TrimSpace(*s.clientOpts.imageModel)
	}
	if model == "" {
		writeError(w, 400, "imageModel is required")
		return
	}
	c, err := s.clientOpts.newClient()
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	defer c.Close()
	prompt := fmt.Sprintf("PPT视觉母版，主题：%s。内容方向：%s。统一视觉风格：%s。统一提示词：%s。生成一张16:9的高质感演示文稿母版示例图，包含清晰的版式、配色、字体层级和装饰元素，不要出现可读文字，不要水印。", strings.TrimSpace(in.Title), strings.TrimSpace(in.Brief), strings.TrimSpace(in.Style), strings.TrimSpace(in.UnifiedPrompt))
	resp, err := c.createImageGeneration(r.Context(), imageGenerationRequest{Model: model, Prompt: prompt, N: 1, Size: "1536x864"})
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	saved, err := saveImagesFromResponse(r.Context(), c.httpClient, resp, s.saveDirectory(), false, "")
	if err != nil || len(saved) == 0 {
		if err == nil {
			err = fmt.Errorf("empty image response")
		}
		writeError(w, 502, err.Error())
		return
	}
	urls := s.publicImageURLs(saved, s.saveDirectory())
	if len(urls) == 0 {
		writeError(w, 502, "saved image is not publicly accessible")
		return
	}
	writeJSON(w, 200, map[string]any{"image": urls[0], "prompt": prompt})
}

func (s *webUIServer) handlePPTPrompts(w http.ResponseWriter, r *http.Request) {
	var in struct{ Brief, Style string }
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	brief := strings.TrimSpace(in.Brief)
	style := strings.TrimSpace(in.Style)
	writeJSON(w, 200, map[string]string{
		"unifiedPrompt": fmt.Sprintf("保持%s风格统一，所有页面使用一致的配色、字体层级、留白和装饰语言；画面清晰、信息层级明确、适合演示文稿。主题内容：%s", style, brief),
		"outlinePrompt": "按背景/问题、核心方案、关键数据或案例、行动计划的逻辑组织内容；每页只表达一个重点，标题简洁，要点具体。",
	})
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
		UnifiedPrompt, OutlinePrompt                 string
		UnifiedBackground                            bool `json:"unifiedBackground"`
	}
	w.Header().Set("X-PPT-OCR-Debug", "request-received")
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
	prompt := fmt.Sprintf(`请为“%s”生成一份单页/多页演示文稿的 PPT DSL 草案。内容要求：%s。大纲提示词：%s。目标受众：%s。页数：%d 页。视觉风格：%s。统一提示词：%s。
%s
文字与图片提示词分离，先保证文字内容完整。必须恰好返回 %d 页，第一页 layout 必须为 title。`, title, strings.TrimSpace(strings.Join(in.References, "\n")), strings.TrimSpace(in.OutlinePrompt), strings.TrimSpace(in.Audience), n, style, strings.TrimSpace(in.UnifiedPrompt), pptPlanDSL, n)
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
	result := pptPlan{Title: title, Style: style, TextModel: model, ImageModel: strings.TrimSpace(in.ImageModel), Audience: strings.TrimSpace(in.Audience), References: in.References, Images: in.Images, Pages: pages, AccentColor: strings.TrimSpace(in.AccentColor), BackgroundColor: strings.TrimSpace(in.BackgroundColor), UnifiedBackground: in.UnifiedBackground, StyleReference: strings.TrimSpace(in.StyleReference), UnifiedPrompt: strings.TrimSpace(in.UnifiedPrompt), OutlinePrompt: strings.TrimSpace(in.OutlinePrompt)}
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
	p.UnifiedPrompt = plan.UnifiedPrompt
	p.OutlinePrompt = plan.OutlinePrompt
	if plan.MasterImage != "" {
		p.Master = &ppt.Master{Background: plan.MasterImage, Elements: []ppt.Element{{ID: "master-image", Type: "image", X: 0, Y: 0, Width: p.Width, Height: p.Height, AssetURL: plan.MasterImage, Style: map[string]any{"objectFit": "cover"}}}}
	}
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
		if plan.ImageOnly {
			// The generated raster is the slide itself. Keep outline text in
			// metadata/content for the optional editable conversion step.
			elems = []ppt.Element{{ID: fmt.Sprintf("slide-%d-image", i+1), Type: "image", X: 0, Y: 0, Width: p.Width, Height: p.Height, Content: page.Title + "\n" + strings.Join(page.Bullets, "\n"), Style: map[string]any{"objectFit": "cover"}}}
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
	if err := json.NewDecoder(io.LimitReader(r.Body, 256<<20)).Decode(&in); err != nil {
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
		prompt := target.Content
		if strings.TrimSpace(in.Presentation.UnifiedPrompt) != "" {
			prompt += "\n统一提示词：" + in.Presentation.UnifiedPrompt
		}
		var resp *responsePayload
		var genErr error
		if in.Presentation.Master != nil && strings.TrimSpace(in.Presentation.Master.Background) != "" {
			// Keep the reference image out of the text prompt. Besides wasting context,
			// embedding a data URL there can exceed the model's prompt limit. The image
			// edit endpoint accepts it as a proper visual reference instead.
			prompt += "\n保持与统一母版视觉风格一致，严格使用相同配色、字体层级、留白和装饰语言；生成16:9完整演示文稿页面，不要水印。"
			reference := strings.TrimSpace(in.Presentation.Master.Background)
			if strings.HasPrefix(reference, "/outputs/") {
				rel := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(reference, "/outputs/")))
				if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					genErr = fmt.Errorf("invalid master image path")
				} else if data, readErr := os.ReadFile(filepath.Join(s.saveDirectory(), rel)); readErr != nil {
					genErr = fmt.Errorf("read master image: %w", readErr)
				} else {
					mediaType := mime.TypeByExtension(filepath.Ext(rel))
					if mediaType == "" {
						mediaType = http.DetectContentType(data)
					}
					reference = "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)
				}
			}
			if genErr != nil {
				errs = append(errs, fmt.Sprintf("slide %d: %v", i+1, genErr))
				continue
			}
			resp, genErr = c.createImageEdit(r.Context(), imageEditRequest{
				Model:  imageModel,
				Prompt: prompt,
				Images: []imageRef{{ImageURL: reference}},
				N:      1,
				Size:   "1536x864",
			})
		} else {
			resp, genErr = c.createImageGeneration(r.Context(), imageGenerationRequest{Model: imageModel, Prompt: prompt, N: 1, Size: "1536x864"})
		}
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

// handlePPTImageToEditable wraps one generated page image in the normal editor
// model and restores the outline copy as editable text layers.
func (s *webUIServer) handlePPTImageToEditable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	var in struct {
		Presentation ppt.Presentation `json:"presentation"`
		SlideIndex   int              `json:"slideIndex"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<20)).Decode(&in); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if in.SlideIndex < 0 || in.SlideIndex >= len(in.Presentation.Slides) {
		writeError(w, 400, "slideIndex out of range")
		return
	}
	sl := &in.Presentation.Slides[in.SlideIndex]
	var img *ppt.Element
	for i := range sl.Elements {
		if sl.Elements[i].Type == "image" {
			img = &sl.Elements[i]
			break
		}
	}
	if img == nil || img.AssetURL == "" {
		writeError(w, 400, "slide has no generated image")
		return
	}
	parts := strings.Split(img.Content, "\n")
	title := strings.TrimSpace(parts[0])
	body := strings.TrimSpace(strings.Join(parts[1:], "\n"))
	if title == "" {
		title = fmt.Sprintf("第 %d 页", in.SlideIndex+1)
	}
	sl.Elements = []ppt.Element{{ID: fmt.Sprintf("slide-%d-image", in.SlideIndex+1), Type: "image", X: 0, Y: 0, Width: in.Presentation.Width, Height: in.Presentation.Height, AssetURL: img.AssetURL, Style: map[string]any{"objectFit": "cover"}}, {ID: fmt.Sprintf("slide-%d-title", in.SlideIndex+1), Type: "text", X: 72, Y: 52, Width: 1136, Height: 80, Content: title, Style: map[string]any{"fontSize": 44.0, "fontWeight": 700, "color": "#ffffff", "background": "transparent"}}, {ID: fmt.Sprintf("slide-%d-body", in.SlideIndex+1), Type: "text", X: 88, Y: 150, Width: 620, Height: 280, Content: body, Style: map[string]any{"fontSize": 28.0, "color": "#ffffff", "background": "transparent"}}}
	in.Presentation.Slides[in.SlideIndex] = *sl
	writeJSON(w, 200, map[string]any{"presentation": in.Presentation})
}

// handlePPTOCR recognizes one user-selected region of a slide image.  The
// returned element is deliberately small and editable; the original raster
// remains as the slide background so users can manually repair the patch.
func (s *webUIServer) handlePPTOCR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var in struct {
		Presentation        ppt.Presentation `json:"presentation"`
		SlideIndex          int              `json:"slideIndex"`
		Provider            string           `json:"provider"`
		Asset               string           `json:"asset"`
		ImageData           string           `json:"imageData"`
		ImageOffsetX        float64          `json:"imageOffsetX"`
		ImageOffsetY        float64          `json:"imageOffsetY"`
		CropWidth           float64          `json:"cropWidth"`
		CropHeight          float64          `json:"cropHeight"`
		ScanAll             bool             `json:"scanAll"`
		X, Y, Width, Height float64
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<20)).Decode(&in); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	// New clients upload only the selected crop. Keep the old presentation
	// path for compatibility with older web clients and API callers.
	compactX, compactY := in.X, in.Y
	if in.Asset != "" {
		localX, localY := compactX, compactY
		compactX, compactY = 0, 0
		if in.CropWidth <= 0 {
			in.CropWidth = in.Width
		}
		if in.CropHeight <= 0 {
			in.CropHeight = in.Height
		}
		in.Presentation.Width, in.Presentation.Height = in.CropWidth, in.CropHeight
		in.Presentation.Slides = []ppt.Slide{{Elements: []ppt.Element{{Type: "image", AssetURL: in.Asset, X: 0, Y: 0, Width: in.CropWidth, Height: in.CropHeight}}}}
		// Keep a zero selection so the normal auto-region detector runs against
		// the supplied image; CropWidth/CropHeight provide its canvas size.
		in.SlideIndex, in.X, in.Y = 0, localX, localY
	}
	if in.SlideIndex < 0 || in.SlideIndex >= len(in.Presentation.Slides) {
		writeError(w, 400, "slideIndex out of range")
		return
	}
	w.Header().Set("X-PPT-OCR-Selection", fmt.Sprintf("%.0fx%.0f+%.0f+%.0f", in.Width, in.Height, in.X, in.Y))
	provider := strings.TrimSpace(in.Provider)
	if provider == "" {
		provider = "local:PaddleOCR"
	}
	// The browser sends the full image on the first OCR request. This avoids
	// silently recognizing a tiny placeholder URL and makes the request self
	// contained; keep Asset as a compatibility fallback for older clients.
	if strings.HasPrefix(in.ImageData, "data:image/") {
		in.Asset = in.ImageData
	}
	var asset string
	for _, e := range in.Presentation.Slides[in.SlideIndex].Elements {
		if e.Type == "image" && e.AssetURL != "" {
			asset = e.AssetURL
			break
		}
	}
	if asset == "" {
		writeError(w, 400, "slide has no image")
		return
	}
	// The first double-click scans the complete raster once. Return every text
	// region without inserting it into the presentation: the browser keeps
	// these candidates and materializes only the region the user clicks.
	if in.ScanAll && provider != "remote" {
		modelDir := paddleOCRModelDir(provider, s.rembgPath)
		if modelDir == "" {
			writeError(w, http.StatusNotFound, "PaddleOCR 模型不存在或缺少识别模型")
			return
		}
		full, err := cropPPTOCRImage(r.Context(), asset, 0, 0, in.Presentation.Width, in.Presentation.Height, in.Presentation.Width, in.Presentation.Height)
		if err != nil {
			writeError(w, http.StatusBadRequest, "无法读取 OCR 图片："+err.Error())
			return
		}
		comma := strings.IndexByte(full, ',')
		if comma < 0 {
			writeError(w, http.StatusBadRequest, "OCR 图片格式无效")
			return
		}
		data, err := base64.StdEncoding.DecodeString(full[comma+1:])
		if err != nil {
			writeError(w, http.StatusBadRequest, "OCR 图片编码无效")
			return
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			writeError(w, http.StatusBadRequest, "OCR 图片无法读取")
			return
		}
		polys, err := paddleOCRDetectPolygons(img, modelDir)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		canvasW, canvasH := in.Presentation.Width, in.Presentation.Height
		if canvasW <= 0 || canvasH <= 0 {
			canvasW, canvasH = float64(img.Bounds().Dx()), float64(img.Bounds().Dy())
		}
		z := nextPPTOCRZ(in.Presentation.Slides[in.SlideIndex].Elements)
		elements := make([]ppt.Element, 0, len(polys))
		for i, poly := range polys {
			box := polygonBounds(poly.Points)
			rawCrop := image.NewRGBA(image.Rect(0, 0, box.Dx(), box.Dy()))
			draw.Draw(rawCrop, rawCrop.Bounds(), img, box.Min.Add(img.Bounds().Min), draw.Src)
			rel := poly.Points
			for j := range rel {
				rel[j].X -= box.Min.X
				rel[j].Y -= box.Min.Y
			}
			cropImg := cropPPTOCRPolygon(rawCrop, rel)
			// Keep the detector's original box. PaddleOCR's DB detector already
			// applies its unclip ratio; trimming by raster contrast here makes
			// large Chinese glyph boxes too short and shifts them downward.
			text, recognizeErr := paddleOCRRecognizeOriented(cropImg, modelDir)
			if recognizeErr != nil {
				writeError(w, http.StatusBadGateway, recognizeErr.Error())
				return
			}
			if strings.TrimSpace(text) == "" {
				continue
			}
			x := float64(box.Min.X) * canvasW / float64(img.Bounds().Dx())
			y := float64(box.Min.Y) * canvasH / float64(img.Bounds().Dy())
			width := float64(box.Dx()) * canvasW / float64(img.Bounds().Dx())
			height := float64(box.Dy()) * canvasH / float64(img.Bounds().Dy())
			fontSize := ocrFontSize(height, text, height)
			if n := len([]rune(strings.ReplaceAll(text, "\n", ""))); n > 0 {
				// Reserve one extra pixel of effective width so the browser's
				// fractional glyph metrics do not wrap the final character.
				// Keep a tiny safety margin for browser glyph rounding; the
				// rendered box itself remains tied to the detector polygon.
				fontSize = math.Min(fontSize, math.Max(1, (math.Round(width)-2))/float64(n))
			}
			style := map[string]any{"fontSize": fontSize, "fontFamily": "Arial", "color": paddleOCRTextColor(cropImg), "fontWeight": 400, "bold": false, "italic": false, "align": "left", "verticalAlign": "middle", "lineHeight": 1.0, "whiteSpace": "nowrap", "background": paddleOCRBackground(cropImg)}
			dx1, dy1 := float64(poly.Points[1].X-poly.Points[0].X), float64(poly.Points[1].Y-poly.Points[0].Y)
			dx2, dy2 := float64(poly.Points[2].X-poly.Points[1].X), float64(poly.Points[2].Y-poly.Points[1].Y)
			if dx2*dx2+dy2*dy2 > dx1*dx1+dy1*dy1 {
				dx1, dy1 = dx2, dy2
			}
			rotation := math.Atan2(dy1, dx1) * 180 / math.Pi
			e := ppt.Element{ID: fmt.Sprintf("ocr-%d-%d", time.Now().UnixNano(), i), Type: "text", X: x + in.ImageOffsetX, Y: y + in.ImageOffsetY, Width: width, Height: height, Rotation: rotation, ZIndex: z + i, Content: text, Style: style, TextRuns: []ppt.TextRun{{Content: text, Style: style}}}
			if e.Style == nil {
				e.Style = map[string]any{}
			}
			e.Style["ocrPolygon"] = poly.Points
			elements = append(elements, e)
		}
		writeJSON(w, 200, map[string]any{"elements": elements, "regions": len(elements), "polygons": polys, "provider": provider, "scanned": true})
		return
	}
	if in.Width <= 1 || in.Height <= 1 {
		full, err := cropPPTOCRImage(r.Context(), asset, 0, 0, in.Presentation.Width, in.Presentation.Height, in.Presentation.Width, in.Presentation.Height)
		if err != nil {
			writeError(w, http.StatusBadRequest, "无法读取 OCR 图片："+err.Error())
			return
		}
		comma := strings.IndexByte(full, ',')
		data, err := base64.StdEncoding.DecodeString(full[comma+1:])
		if err != nil {
			writeError(w, http.StatusBadRequest, "OCR 图片编码无效")
			return
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			writeError(w, http.StatusBadRequest, "OCR 图片无法读取")
			return
		}
		in.X, in.Y, in.Width, in.Height = autoPPTOCRRegion(img, in.X, in.Y, in.Presentation.Width, in.Presentation.Height)
		if in.Width <= 1 || in.Height <= 1 {
			writeError(w, 400, "未找到点击位置附近的文字")
			return
		}
	}
	// Crop before OCR so the provider only receives the selected pixels.
	cropped, cropErr := cropPPTOCRImage(r.Context(), asset, in.X, in.Y, in.Width, in.Height, in.Presentation.Width, in.Presentation.Height)
	if cropErr != nil {
		writeError(w, http.StatusBadRequest, "无法裁剪 OCR 区域："+cropErr.Error())
		return
	}
	// Local PaddleOCR recognition operates directly on the selected crop and
	// does not require a text/vision API key.
	if provider != "" && provider != "remote" {
		modelDir := paddleOCRModelDir(provider, s.rembgPath)
		if modelDir == "" {
			writeError(w, http.StatusNotFound, "PaddleOCR 模型不存在或缺少识别模型")
			return
		}
		comma := strings.IndexByte(cropped, ',')
		if comma < 0 {
			writeError(w, http.StatusBadRequest, "OCR 图片格式无效")
			return
		}
		data, err := base64.StdEncoding.DecodeString(cropped[comma+1:])
		if err != nil {
			writeError(w, http.StatusBadRequest, "OCR 图片编码无效")
			return
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			writeError(w, http.StatusBadRequest, "OCR 图片无法读取")
			return
		}
		text, err := paddleOCRRecognizeOriented(img, modelDir)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		// Recognition is intentionally performed exactly once per request. The
		// browser caches the resulting editable box, so later double-clicks do
		// not invoke OCR again.
		if text == "" {
			writeJSON(w, 200, map[string]any{"text": "", "provider": provider, "recognized": false, "cropWidth": img.Bounds().Dx(), "cropHeight": img.Bounds().Dy()})
			return
		}
		fontSize := ocrFontSize(in.Height, text, 24)
		background := paddleOCRBackground(img)
		style := map[string]any{"fontSize": fontSize, "fontFamily": "Arial", "color": "#111827", "fontWeight": 400, "bold": false, "italic": false, "align": "left", "verticalAlign": "middle", "background": background}
		sl := &in.Presentation.Slides[in.SlideIndex]
		z := nextPPTOCRZ(sl.Elements)
		// Cover the raster glyphs before adding the editable OCR text. The
		// border-derived color works well for flat slide backgrounds and keeps
		// the operation fully offline.
		e := ppt.Element{ID: fmt.Sprintf("ocr-%d", time.Now().UnixNano()), Type: "text", X: in.X, Y: in.Y, Width: in.Width, Height: in.Height, ZIndex: z + 1, Content: text, Style: style, TextRuns: []ppt.TextRun{{Content: text, Style: style}}}
		e.X += in.ImageOffsetX
		e.Y += in.ImageOffsetY
		sl.Elements = append(sl.Elements, e)
		if compactX != 0 || compactY != 0 {
			for i := range sl.Elements {
				sl.Elements[i].X += compactX
				sl.Elements[i].Y += compactY
			}
			e.X, e.Y = compactX, compactY
		}
		writeJSON(w, 200, map[string]any{"presentation": in.Presentation, "element": e, "elements": []ppt.Element{e}, "text": text, "style": style, "provider": provider})
		return
	}
	model := strings.TrimSpace(in.Presentation.AIModel)
	if model == "" && s.clientOpts.model != nil {
		model = strings.TrimSpace(*s.clientOpts.model)
	}
	if model == "" {
		writeError(w, 400, "text model is not configured")
		return
	}
	c, err := s.clientOpts.newClient()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	prompt := `You are an OCR engine. The supplied image is exactly the selected region. Return JSON only, no markdown: {"text":"...","color":"#RRGGBB","fontSize":24,"fontFamily":"Arial","bold":false,"italic":false,"align":"left","backgroundColor":"#RRGGBB"}. Preserve line breaks. Estimate the dominant text color and size in logical pixels. If the region has no text, return {"text":""}.`
	input := []inputMessage{{Role: "user", Content: []inputContent{{Type: "input_text", Text: prompt}, {Type: "input_image", ImageURL: cropped}}}}
	resp, err := c.createResponse(r.Context(), responseRequest{Model: model, Input: input})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	text := strings.TrimSpace(resp.text())
	if a, b := strings.Index(text, "{"), strings.LastIndex(text, "}"); a >= 0 && b > a {
		text = text[a : b+1]
	}
	var out struct {
		Text, Color, FontFamily, Align, BackgroundColor string
		FontSize                                        float64
		Bold, Italic                                    bool
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		writeError(w, http.StatusBadGateway, "OCR 返回格式无效")
		return
	}
	out.Text = strings.TrimSpace(out.Text)
	if out.Text == "" {
		writeJSON(w, 200, map[string]any{"text": ""})
		return
	}
	if out.Color == "" {
		out.Color = "#111827"
	}
	if out.FontSize <= 0 {
		out.FontSize = 24
	}
	out.FontSize = ocrFontSize(in.Height, out.Text, out.FontSize)
	if out.FontFamily == "" {
		out.FontFamily = "Arial"
	}
	if out.Align == "" {
		out.Align = "left"
	}
	style := map[string]any{"fontSize": out.FontSize, "fontFamily": out.FontFamily, "color": out.Color, "bold": out.Bold, "fontWeight": func() any {
		if out.Bold {
			return 700
		}
		return 400
	}(), "italic": out.Italic, "align": out.Align, "verticalAlign": "middle", "background": "transparent"}
	// A solid patch hides the old raster text when the OCR model can estimate
	// the local background. Users can remove or recolor this patch manually.
	if out.BackgroundColor == "" || strings.EqualFold(strings.TrimSpace(out.BackgroundColor), strings.TrimSpace(out.Color)) {
		out.BackgroundColor = ""
		if comma := strings.IndexByte(cropped, ','); comma >= 0 {
			if data, err := base64.StdEncoding.DecodeString(cropped[comma+1:]); err == nil {
				if img, _, err := image.Decode(bytes.NewReader(data)); err == nil {
					out.BackgroundColor = paddleOCRBackground(img)
				}
			}
		}
	}
	if out.BackgroundColor == "" {
		out.BackgroundColor = "#ffffff"
	}
	sl := &in.Presentation.Slides[in.SlideIndex]
	style["background"] = out.BackgroundColor
	e := ppt.Element{ID: fmt.Sprintf("ocr-%d", time.Now().UnixNano()), Type: "text", X: in.X, Y: in.Y, Width: in.Width, Height: in.Height, ZIndex: nextPPTOCRZ(sl.Elements), Content: out.Text, Style: style, TextRuns: []ppt.TextRun{{Content: out.Text, Style: style}}}
	e.X += in.ImageOffsetX
	e.Y += in.ImageOffsetY
	sl.Elements = append(sl.Elements, e)
	if compactX != 0 || compactY != 0 {
		for i := range sl.Elements {
			sl.Elements[i].X += compactX
			sl.Elements[i].Y += compactY
		}
		e.X, e.Y = compactX, compactY
	}
	writeJSON(w, 200, map[string]any{"presentation": in.Presentation, "element": e, "elements": []ppt.Element{e}, "text": out.Text, "style": style})
}

func adaptivePPTOCRRegions(x, y, w, h float64) [][4]float64 {
	if w <= 0 || h <= 0 {
		return nil
	}
	var out [][4]float64
	for _, scale := range []float64{0.12, 0.22, 0.38, 0.58} {
		rw, rh := w*scale, h*scale*.42
		left, top := maxFloat(0, minFloat(w-rw, x-rw/2)), maxFloat(0, minFloat(h-rh, y-rh/2))
		out = append(out, [4]float64{left, top, rw, rh})
	}
	// If the click landed in a gap between glyphs, scan the whole slide with
	// overlapping, slide-relative tiles and let recognition identify the tile
	// containing text. The chosen tile is still returned as the editable box.
	for _, scale := range []float64{0.38, 0.58} {
		rw, rh := w*scale, h*scale*.42
		for _, fx := range []float64{0, .5, 1} {
			for _, fy := range []float64{0, .5, 1} {
				out = append(out, [4]float64{(w - rw) * fx, (h - rh) * fy, rw, rh})
			}
		}
	}
	return out
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func (s *webUIServer) handlePPTOCRModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// A PaddleOCR model is a bundle (det + rec, with an optional cls model),
	// not each individual ONNX file.  Listing every file made the picker show
	// several entries that could not be used on their own.
	type modelBundle struct {
		dir   string
		files map[string]bool
		size  int64
	}
	dirs := []string{filepath.Join("models", "paddleocr")}
	if s.rembgPath != "" {
		dirs = append(dirs, filepath.Join(filepath.Dir(s.rembgPath), "models", "paddleocr"))
	}
	bundles := map[string]*modelBundle{}
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, e os.DirEntry, walkErr error) error {
			if walkErr != nil || e.IsDir() {
				return nil
			}
			lowerName := strings.ToLower(e.Name())
			if !strings.HasSuffix(lowerName, ".onnx") && lowerName != "ppocr_keys_v1.txt" && lowerName != "keys.txt" {
				return nil
			}
			info, _ := e.Info()
			rel, _ := filepath.Rel(dir, path)
			parent := filepath.Clean(filepath.Dir(rel))
			if parent == "." {
				parent = ""
			}
			key := parent
			bundle := bundles[key]
			if bundle == nil {
				bundle = &modelBundle{dir: parent, files: map[string]bool{}}
				bundles[key] = bundle
			}
			fileName := lowerName
			if bundle.files[fileName] {
				return nil
			}
			bundle.files[fileName] = true
			if info != nil {
				bundle.size += info.Size()
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			writeError(w, 500, err.Error())
			return
		}
	}
	// A bundle is usable when its recognition model and dictionary are present.
	keys := make([]string, 0, len(bundles))
	for key, bundle := range bundles {
		if (bundle.files["rec.onnx"] || bundle.files["pp-ocrv6_rec_small.onnx"] || bundle.files["pp-ocrv6_rec_infer_small.onnx"] || bundle.files["pp-ocrv6_rec_infer.onnx"] || bundle.files["rec_small.onnx"]) && (bundle.files["ppocr_keys_v1.txt"] || bundle.files["keys.txt"]) {
			keys = append(keys, key)
		}
	}
	sort.SliceStable(keys, func(i, j int) bool {
		// Present the bundled PP-OCRv6 package first.
		vi := strings.EqualFold(keys[i], "ppocrv6-small")
		vj := strings.EqualFold(keys[j], "ppocrv6-small")
		if vi != vj {
			return vi
		}
		return keys[i] < keys[j]
	})
	models := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		bundle := bundles[key]
		name := bundle.dir
		if name == "" {
			name = "PaddleOCR"
		}
		if strings.EqualFold(bundle.dir, "ppocrv6-small") {
			name = "PP-OCRv6 Small（轻量）"
		} else if strings.EqualFold(bundle.dir, "ppocrv6") {
			name = "PP-OCRv6 Medium（标准）"
		} else {
			for _, dir := range dirs {
				if st, err := os.Stat(filepath.Join(dir, "PP-OCRv6_det_small.onnx")); err == nil && !st.IsDir() {
					name = "PP-OCRv6 检测 + " + name + " 识别"
					break
				}
			}
		}
		models = append(models, map[string]any{"id": bundle.dir, "name": name, "size": bundle.size, "available": true, "local": true})
	}
	writeJSON(w, 200, map[string]any{"models": models, "remote": map[string]any{"id": "remote", "name": "远程视觉模型", "paid": true}})
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

// styleLineHeight returns the DrawingML multiplier used by genppt. Numeric
// values are the editor's multiplier (1.4 means 140%). CSS-style strings are
// accepted as absolute lengths and converted using the element font size.
func styleLineHeight(m map[string]any, key string, fontSizePx float64) (float64, bool) {
	if m == nil {
		return 0, false
	}
	if v, ok := styleFloat(m, key); ok {
		return v, true
	}
	raw, ok := m[key].(string)
	if !ok {
		return 0, false
	}
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" || raw == "normal" || fontSizePx <= 0 {
		return 0, false
	}
	unit := ""
	for _, suffix := range []string{"px", "pt"} {
		if strings.HasSuffix(raw, suffix) {
			raw, unit = strings.TrimSpace(strings.TrimSuffix(raw, suffix)), suffix
			break
		}
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	if unit == "pt" {
		v *= ppt.LogicalDPI / 72
	}
	if unit == "px" || unit == "pt" {
		return v / fontSizePx, true
	}
	return 0, false
}

func exportFontFamily(family string, embedded []ppt.FontAsset) string {
	family = strings.TrimSpace(family)
	if family == "" {
		return ""
	}
	// A font asset can be browser-only (WOFF/WOFF2) or remain obfuscated after
	// import. Referencing that family in the PPTX would make WPS report it as a
	// missing font even though the source editor rendered it successfully.
	found, usable := false, false
	for _, asset := range embedded {
		if !strings.EqualFold(strings.TrimSpace(asset.Family), family) {
			continue
		}
		found = true
		if !asset.Obfuscated && isOfficeEmbeddedFont(dataURLBytes(asset.AssetURL)) {
			usable = true
			break
		}
	}
	if found && !usable {
		return "Arial"
	}
	return family
}

// isOfficeEmbeddedFont reports whether the payload is a font format accepted
// by PowerPoint/WPS embedded-font parts. Browsers can load WOFF/WOFF2, but
// those web formats are not valid embedded fonts in a PPTX package and make
// WPS show a misleading "missing font" warning.
func isOfficeEmbeddedFont(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	sig := string(data[:4])
	return sig == "OTTO" || sig == "ttcf" || sig == "true" || sig == "typ1" || sig == "\x00\x01\x00\x00"
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
	vl := strings.ToLower(v)
	if strings.HasPrefix(vl, "rgb(") || strings.HasPrefix(vl, "rgba(") {
		open := strings.IndexByte(v, '(')
		parts := strings.Split(strings.TrimSuffix(v[open+1:], ")"), ",")
		if len(parts) == 3 || len(parts) == 4 {
			var r, g, b int
			var a float64
			if _, err := fmt.Sscanf(strings.TrimSpace(parts[0]), "%d", &r); err == nil {
				_, err = fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &g)
				if err == nil {
					_, err = fmt.Sscanf(strings.TrimSpace(parts[2]), "%d", &b)
				}
				if err == nil {
					if len(parts) == 4 {
						_, err = fmt.Sscanf(strings.TrimSpace(parts[3]), "%f", &a)
					} else {
						a = 1
					}
				}
				if err == nil {
					return fmt.Sprintf("#%02X%02X%02X", r, g, b), (1 - a) * 100
				}
			}
		}
	}
	return v, 0
}

func exportColor(v string) string {
	c, _ := cssColor(v)
	return c
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
		// DrawingML's distributed alignment (dist) is rendered by WPS as
		// the paragraph/distributed mode exposed in the editor. Keep accepting
		// the historical just/justify input, but emit dist on export.
		return genppt.Align("dist")
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
	// Preserve the original family names and carry the embedded font payloads;
	// export must never silently substitute a different typeface.
	usedFontFiles := map[string]bool{}
	for _, font := range model.FontAssets {
		if font.Obfuscated || font.AssetURL == "" {
			continue
		}
		if data := dataURLBytes(font.AssetURL); len(data) > 0 {
			if !isOfficeEmbeddedFont(data) {
				// Keep the family available to the editor, but never emit a web
				// font as an OOXML embedded part (WPS treats it as missing).
				continue
			}
			base := strings.TrimSuffix(filepath.Base(font.FileName), filepath.Ext(font.FileName))
			if base == "" || base == "." {
				base = "font"
			}
			name := base + ".fntdata"
			for n := 2; usedFontFiles[name]; n++ {
				name = fmt.Sprintf("%s-%d.fntdata", base, n)
			}
			usedFontFiles[name] = true
			p.AddEmbeddedFont(font.Family, font.Style, font.Weight, name, data)
		}
	}
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
				// A structured line with type=none is an explicit absence of a
				// border; do not let the custom-geometry writer synthesize one.
				if e.Line.Color == nil || e.Line.Color.Type != "none" {
					opts.LineColor = colorFrom(e.Line.Color)
					opts.LineWidth = e.Line.Width * 72 / ppt.LogicalDPI
					opts.LineStyle = borderStyleValue(e.Line.Dash)
				}
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
					sl.AddImage(genppt.ImageOptions{X: opts.X, Y: opts.Y, Width: opts.Width, Height: opts.Height, Data: data, Rotate: e.Rotation, FlipH: e.FlipH, FlipV: e.FlipV})
					opts.Fill = "transparent"
				}
			}
			if e.Type == "text" {
				o := genppt.TextOptions{X: e.X / ppt.LogicalDPI, Y: e.Y / ppt.LogicalDPI, Width: e.Width / ppt.LogicalDPI, Height: e.Height / ppt.LogicalDPI, FlipH: e.FlipH, FlipV: e.FlipV}
				if v, ok := styleFloat(e.Style, "fontSize"); ok {
					// genppt expects points; the editor model uses 128 logical px/in.
					o.FontSize = v * 72 / ppt.LogicalDPI
				}
				o.FontColor = exportColor(styleString(e.Style, "color"))
				if o.FontColor == "" {
					o.FontColor = exportColor(model.Theme.Foreground)
				}
				if v, ok := styleFloat(e.Style, "fontWeight"); ok {
					o.Bold = v >= 600
				}
				o.Bold = o.Bold || styleBool(e.Style, "bold")
				o.Italic = styleBool(e.Style, "italic")
				o.Underline = styleBool(e.Style, "underline")
				o.FontFace = exportFontFamily(styleString(e.Style, "fontFamily"), model.FontAssets)
				o.Rotate = opts.Rotate
				fontSizePx := 0.0
				if v, ok := styleFloat(e.Style, "fontSize"); ok {
					fontSizePx = v
				} else if e.Text != nil {
					fontSizePx = e.Text.FontSize
				}
				if v, ok := styleLineHeight(e.Style, "lineHeight", fontSizePx); ok {
					o.LineSpacing = v
				}
				// CSS's normal line-height is roughly 1.2 for the fonts used by
				// the editor. Make it explicit so WPS/PowerPoint do not choose a
				// different application default.
				if o.LineSpacing <= 0 {
					o.LineSpacing = 1.2
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
						o.FontFace = exportFontFamily(e.Text.FontFamily, model.FontAssets)
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
					if e.Text.LineSpacingPoints > 0 {
						o.LineSpacingPoints = e.Text.LineSpacingPoints
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
						if run.Paragraph >= 0 && run.Paragraph < len(e.TextParagraphs) {
							r.Options.LineSpacingPoints = e.TextParagraphs[run.Paragraph].LineSpacingPoints
						}
						if v, ok := styleFloat(run.Style, "fontSize"); ok {
							r.Options.FontSize = v * 72 / ppt.LogicalDPI
						}
						runFontSizePx := fontSizePx
						if v, ok := styleFloat(run.Style, "fontSize"); ok {
							runFontSizePx = v
						}
						if v, ok := styleLineHeight(run.Style, "lineHeight", runFontSizePx); ok {
							r.Options.LineSpacing = v
						}
						if v := styleString(run.Style, "fontColor"); v != "" {
							r.Options.FontColor = exportColor(v)
						}
						if v := styleString(run.Style, "color"); v != "" {
							r.Options.FontColor = exportColor(v)
						}
						if v := styleString(run.Style, "fontFamily"); v != "" {
							r.Options.FontFace = exportFontFamily(v, model.FontAssets)
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
					customOpts := genppt.CustomGeometryOptions{X: opts.X, Y: opts.Y, Width: opts.Width, Height: opts.Height, Path: e.Geometry.Path, ViewBox: e.Geometry.ViewBox, Fill: opts.Fill, LineColor: opts.LineColor, LineWidth: opts.LineWidth, Transparency: opts.Transparency, Rotate: opts.Rotate, FlipH: e.FlipH, FlipV: e.FlipV}
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
				opts.FlipH, opts.FlipV = e.FlipH, e.FlipV
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
				to.FontFace = exportFontFamily(styleString(e.Style, "fontFamily"), model.FontAssets)
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
					sl.AddImage(genppt.ImageOptions{X: x, Y: y, Width: w, Height: h, Data: data, Rotate: e.Rotation, FlipH: e.FlipH, FlipV: e.FlipV})
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
