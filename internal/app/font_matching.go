package app

// Font discovery and server-side OCR font matching.  Keeping this work here
// means the browser only has to display the style returned by the OCR API.

import (
	"image"
	"image/color"
	"image/draw"
	"kuaima_cli/internal/ppt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type installedFont struct {
	Family string
	Style  string
	Weight int
	Path   string
	Data   []byte
	Index  int
}

var fontCache struct {
	sync.Once
	fonts []installedFont
}

// fontSearchDirs is deliberately exported through a small function rather
// than hard-coded in the HTTP handler, which also makes platform tests easy.
func fontSearchDirs() []string {
	var dirs []string
	switch runtime.GOOS {
	case "windows":
		if root := os.Getenv("WINDIR"); root != "" {
			dirs = append(dirs, filepath.Join(root, "Fonts"))
		} else {
			dirs = append(dirs, `C:\Windows\Fonts`)
		}
		if app, err := os.UserConfigDir(); err == nil {
			dirs = append(dirs, filepath.Join(app, "Microsoft", "Windows", "Fonts"))
		}
		if app := os.Getenv("LOCALAPPDATA"); app != "" {
			dirs = append(dirs, filepath.Join(app, "Microsoft", "Windows", "Fonts"))
		}
	case "darwin":
		dirs = append(dirs, "/System/Library/Fonts", "/Library/Fonts")
		if home, err := os.UserHomeDir(); err == nil {
			dirs = append(dirs, filepath.Join(home, "Library", "Fonts"))
		}
	default:
		if home, err := os.UserHomeDir(); err == nil {
			dirs = append(dirs, filepath.Join(home, ".fonts"), filepath.Join(home, ".local", "share", "fonts"))
		}
		dirs = append(dirs, "/usr/share/fonts", "/usr/local/share/fonts")
	}
	return dirs
}

func fontFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ttf", ".otf", ".ttc", ".otc":
		return true
	}
	return false
}

func weightFromStyle(s string) int {
	s = strings.ToLower(s)
	if strings.Contains(s, "black") || strings.Contains(s, "heavy") {
		return 900
	}
	if strings.Contains(s, "bold") || strings.Contains(s, "demi") {
		return 700
	}
	if strings.Contains(s, "semibold") {
		return 600
	}
	if strings.Contains(s, "medium") {
		return 500
	}
	if strings.Contains(s, "light") || strings.Contains(s, "thin") {
		return 300
	}
	return 400
}

func parseFont(path string, data []byte) []installedFont {
	c, err := opentype.ParseCollection(data)
	if err != nil {
		return nil
	}
	out := make([]installedFont, 0, c.NumFonts())
	for i := 0; i < c.NumFonts(); i++ {
		f, err := c.Font(i)
		if err != nil {
			continue
		}
		family, _ := f.Name(nil, 1)
		style, _ := f.Name(nil, 2)
		if family == "" {
			family, _ = f.Name(nil, 16)
		}
		if family == "" {
			continue
		}
		out = append(out, installedFont{Family: family, Style: style, Weight: weightFromStyle(style), Path: path, Data: data, Index: i})
	}
	return out
}

func scanInstalledFonts() []installedFont {
	fontCache.Do(func() {
		seen := map[string]bool{}
		for _, dir := range fontSearchDirs() {
			_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() || !fontFile(path) {
					return nil
				}
				if seen[path] {
					return nil
				}
				seen[path] = true
				if data, err := os.ReadFile(path); err == nil {
					fontCache.fonts = append(fontCache.fonts, parseFont(path, data)...)
				}
				return nil
			})
		}
		sort.Slice(fontCache.fonts, func(i, j int) bool {
			return strings.ToLower(fontCache.fonts[i].Family) < strings.ToLower(fontCache.fonts[j].Family)
		})
	})
	return append([]installedFont(nil), fontCache.fonts...)
}

func sourceMask(im image.Image) []bool {
	b := im.Bounds()
	w, h := b.Dx(), b.Dy()
	out := make([]bool, w*h)
	// OCR boxes frequently sit on top of a photograph or a gradient.  A
	// border colour threshold works for a flat background, but mistakes every
	// background variation for ink in those images.  Estimate a local
	// background with an integral-image box blur and keep pixels that are
	// substantially darker than it (the usual case for slide text).
	gray := make([]float64, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, _ := im.At(b.Min.X+x, b.Min.Y+y).RGBA()
			gray[y*w+x] = (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(bl)) / 257
		}
	}
	const radius = 8
	stride := w + 1
	prefix := make([]float64, (h+1)*stride)
	for y := 0; y < h; y++ {
		row := 0.0
		for x := 0; x < w; x++ {
			row += gray[y*w+x]
			prefix[(y+1)*stride+x+1] = prefix[y*stride+x+1] + row
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			x0, x1 := x-radius, x+radius
			y0, y1 := y-radius, y+radius
			if x0 < 0 {
				x0 = 0
			}
			if y0 < 0 {
				y0 = 0
			}
			if x1 >= w {
				x1 = w - 1
			}
			if y1 >= h {
				y1 = h - 1
			}
			sum := prefix[(y1+1)*stride+x1+1] - prefix[y0*stride+x1+1] - prefix[(y1+1)*stride+x0] + prefix[y0*stride+x0]
			area := float64((x1 - x0 + 1) * (y1 - y0 + 1))
			// Slide text in the matching crops is dark against its local
			// background.  Do not use absolute contrast here: gradients and
			// illustration edges would become foreground and favor decorative
			// fonts such as ST Hupo.
			out[y*w+x] = gray[y*w+x] < sum/area-12
		}
	}
	return out
}

func renderFontMask(f installedFont, text string, width, height int, size float64) ([]bool, bool) {
	collection, err := opentype.ParseCollection(f.Data)
	if err != nil {
		return nil, false
	}
	if f.Index < 0 || f.Index >= collection.NumFonts() {
		return nil, false
	}
	fontObj, err := collection.Font(f.Index)
	if err != nil {
		return nil, false
	}
	face, err := opentype.NewFace(fontObj, &opentype.FaceOptions{Size: size, DPI: 72})
	if err != nil {
		return nil, false
	}
	defer face.Close()
	gray := image.NewGray(image.Rect(0, 0, width, height))
	draw.Draw(gray, gray.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	d := &font.Drawer{Dst: gray, Src: image.NewUniform(color.Black), Face: face, Dot: fixed.P(1, int(size))}
	d.DrawString(text)
	mask := make([]bool, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			mask[y*width+x] = gray.GrayAt(x, y).Y < 200
		}
	}
	return mask, true
}

func maskBounds(mask []bool, width, height int) (minX, minY, maxX, maxY int, ok bool) {
	minX, minY, maxX, maxY = width, height, -1, -1
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if !mask[y*width+x] {
				continue
			}
			if x < minX {
				minX = x
			}
			if x > maxX {
				maxX = x
			}
			if y < minY {
				minY = y
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	return minX, minY, maxX, maxY, maxX >= minX && maxY >= minY
}

func normalizeMask(mask []bool, width, height int) []bool {
	minX, minY, maxX, maxY, ok := maskBounds(mask, width, height)
	if !ok {
		return nil
	}
	const outW, outH = 128, 64
	out := make([]bool, outW*outH)
	boxW, boxH := maxX-minX+1, maxY-minY+1
	for y := 0; y < outH; y++ {
		for x := 0; x < outW; x++ {
			sx := minX + x*boxW/outW
			sy := minY + y*boxH/outH
			out[y*outW+x] = mask[sy*width+sx]
		}
	}
	return out
}

func maskSimilarity(a, b []bool, width, height int) float64 {
	aMinX, aMinY, aMaxX, aMaxY, aOK := maskBounds(a, width, height)
	bMinX, bMinY, bMaxX, bMaxY, bOK := maskBounds(b, width, height)
	if !aOK || !bOK {
		return 0
	}
	// Keep a small signal from the original raster area. Normalizing both
	// masks to their bounds is useful for baseline/size differences, but by
	// itself it lets a sparse decorative face imitate a dense body face.
	aInk, bInk := 0, 0
	for i := range a {
		if a[i] {
			aInk++
		}
		if b[i] {
			bInk++
		}
	}
	density := 0.0
	if aInk > 0 && bInk > 0 {
		density = math.Min(float64(aInk), float64(bInk)) / math.Max(float64(aInk), float64(bInk))
	}
	a = normalizeMask(a, width, height)
	b = normalizeMask(b, width, height)
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	// OCR crops and font rasterizers can disagree by a pixel or two at the
	// baseline.  Search a tiny translation window before scoring; otherwise a
	// correct face loses to a different face whose strokes happen to line up.
	bestF1 := 0.0
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			overlap, inkA, inkB := 0, 0, 0
			for y := 0; y < 64; y++ {
				for x := 0; x < 128; x++ {
					i := y*128 + x
					if a[i] {
						inkA++
					}
					sx, sy := x-dx, y-dy
					if sx >= 0 && sx < 128 && sy >= 0 && sy < 64 && b[sy*128+sx] {
						inkB++
						if a[i] {
							overlap++
						}
					}
				}
			}
			if inkA > 0 && inkB > 0 {
				f1 := 2 * float64(overlap) / float64(inkA+inkB)
				if f1 > bestF1 {
					bestF1 = f1
				}
			}
		}
	}
	if bestF1 == 0 {
		return 0
	}
	// Compare ink only. Counting matching background pixels makes every sparse
	// glyph mask look similar and hides real differences between font faces.
	// F1 balances overlap, precision and recall so a tiny subset of the
	// original glyphs cannot win merely by having a favorable IoU.
	aRatio := float64(aMaxX-aMinX+1) / float64(aMaxY-aMinY+1)
	bRatio := float64(bMaxX-bMinX+1) / float64(bMaxY-bMinY+1)
	// Keep the original glyph aspect ratio as a second signal. Normalizing
	// every mask to the same rectangle alone can make very wide display faces
	// look deceptively similar to ordinary body fonts.
	ratio := math.Min(aRatio, bRatio) / math.Max(aRatio, bRatio)
	return bestF1*0.68 + ratio*0.20 + density*0.12
}

func hasChinese(text string) bool {
	for _, r := range text {
		if unicode.In(r, unicode.Han) {
			return true
		}
	}
	return false
}

// fontSupportsText rejects Latin-only fonts for Chinese OCR regions.  Testing
// the cmap table is more reliable than family-name heuristics (Arial Unicode,
// Noto CJK and localized Windows names all differ).
func fontSupportsText(f installedFont, text string) bool {
	if !hasChinese(text) {
		return true
	}
	c, err := opentype.ParseCollection(f.Data)
	if err != nil {
		return false
	}
	for i := 0; i < c.NumFonts(); i++ {
		face, err := c.Font(i)
		if err != nil {
			continue
		}
		ok := true
		for _, r := range text {
			if unicode.In(r, unicode.Han) {
				glyph, err := face.GlyphIndex(nil, r)
				if err != nil || glyph == 0 {
					ok = false
					break
				}
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// matchOCRFont compares server-rendered glyph masks with the OCR crop. Assets
// from the imported PPT are considered first, followed by installed fonts.
func matchOCRFont(crop image.Image, text string, size float64, assets []ppt.FontAsset) (family string, bold bool, weight int) {
	b := crop.Bounds()
	src := sourceMask(crop)
	candidates := make([]installedFont, 0, len(assets)+64)
	for _, a := range assets {
		if isDecorativeOCRFont(a.Family) {
			continue
		}
		if a.Obfuscated || a.AssetURL == "" {
			continue
		}
		if d := dataURLBytes(a.AssetURL); len(d) > 0 {
			candidates = append(candidates, installedFont{Family: a.Family, Style: a.Style, Weight: a.Weight, Data: d, Index: 0})
		}
	}
	candidates = append(candidates, scanInstalledFonts()...)
	if hasChinese(text) {
		filtered := candidates[:0]
		for _, c := range candidates {
			if fontSupportsText(c, text) {
				filtered = append(filtered, c)
			}
		}
		candidates = filtered
	}
	best := -1.0
	bestF := installedFont{Family: "Arial", Weight: 400}
	if hasChinese(text) {
		bestF.Family = "Microsoft YaHei"
	}
	for _, c := range candidates {
		if isDecorativeOCRFont(c.Family) {
			continue
		}
		m, ok := renderFontMask(c, text, b.Dx(), b.Dy(), size)
		if !ok {
			continue
		}
		score := maskSimilarity(src, m, b.Dx(), b.Dy())
		if score > best {
			best, bestF = score, c
		}
	}
	if bestF.Family == "" {
		bestF.Family = "Arial"
	}
	if bestF.Weight == 0 {
		bestF.Weight = 400
	}
	return bestF.Family, bestF.Weight >= 600, bestF.Weight
}

// Decorative display faces such as 华文琥珀 are poor OCR reconstruction
// candidates: their dense outlines and unusual glyph metrics frequently win
// mask matching by accident (often serialized by Windows as STHUPO).
func isDecorativeOCRFont(family string) bool {
	s := strings.ToLower(strings.TrimSpace(family))
	return strings.Contains(s, "sthupo") || strings.Contains(s, "hupo") || strings.Contains(s, "琥珀") || strings.Contains(s, "outlook") || strings.Contains(s, "webding") || strings.Contains(s, "jokerman")
}
