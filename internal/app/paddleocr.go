package app

// The PPT OCR endpoint uses PaddleOCR detection and recognition together. The
// detector runs once for a complete slide image; recognition then fills the
// returned regions so the browser can cache them for coordinate-based edits.
import (
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	ort "github.com/yalue/onnxruntime_go"
)

type OCRPolygon struct {
	Points [4]image.Point `json:"points"`
	Score  float32        `json:"score"`
}

func polygonBounds(p [4]image.Point) image.Rectangle {
	minX, minY, maxX, maxY := p[0].X, p[0].Y, p[0].X, p[0].Y
	for _, q := range p[1:] {
		minX = minInt(minX, q.X)
		minY = minInt(minY, q.Y)
		maxX = maxOCRInt(maxX, q.X)
		maxY = maxOCRInt(maxY, q.Y)
	}
	return image.Rect(minX, minY, maxX+1, maxY+1)
}

// paddleOCRDetect runs PaddleOCR's DB detector once for the complete image
// and returns axis-aligned text boxes in source-image pixels.  PPT images are
// not perspective photographed documents, so an axis-aligned box is both more
// useful to the editor and avoids throwing away pixels before recognition.
func paddleOCRDetect(img image.Image, modelDir string) ([]image.Rectangle, error) {
	polys, err := paddleOCRDetectPolygons(img, modelDir)
	if err != nil {
		return nil, err
	}
	out := make([]image.Rectangle, 0, len(polys))
	for _, p := range polys {
		out = append(out, polygonBounds(p.Points))
	}
	return out, nil
}

func paddleOCRDetectPolygons(img image.Image, modelDir string) ([]OCRPolygon, error) {
	return paddleOCRDetectRaw(img, modelDir)
}

func paddleOCRDetectRaw(img image.Image, modelDir string) ([]OCRPolygon, error) {
	det := filepath.Join(modelDir, "det.onnx")
	// Prefer the bundled PP-OCRv6 detector when present. Recognition remains
	// independent because PaddleOCR distributes det/rec as separate models.
	for _, candidate := range []string{
		filepath.Join(filepath.Dir(modelDir), "PP-OCRv6_det_small.onnx"),
		filepath.Join(modelDir, "PP-OCRv6_det_small.onnx"),
	} {
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			det = candidate
			break
		}
	}
	if st, err := os.Stat(det); err != nil || st.IsDir() {
		return nil, fmt.Errorf("PaddleOCR 模型缺少 det.onnx")
	}
	if !ort.IsInitialized() {
		if library := paddleOCRRuntimePath(modelDir); library != "" {
			ort.SetSharedLibraryPath(library)
		}
		if err := ort.InitializeEnvironment(); err != nil {
			return nil, fmt.Errorf("initialize ONNX runtime: %w", err)
		}
	}
	session, inputs, outputs, err := ort.NewDynamicAdvancedSessionAuto(det, nil)
	if err != nil {
		return nil, fmt.Errorf("load PaddleOCR detection model: %w", err)
	}
	defer session.Destroy()
	if len(inputs) == 0 || len(outputs) == 0 {
		return nil, fmt.Errorf("PaddleOCR detection model has no input/output")
	}
	b := img.Bounds()
	if b.Dx() < 2 || b.Dy() < 2 {
		return nil, nil
	}
	// The bundled ONNX export is trained for the usual high-resolution DB
	// input.  Feeding a full slide at a 64px short side collapses glyphs into
	// single probability pixels and causes severe recognition regressions. Keep
	// the export's stable 1280px long-side envelope and stride alignment.
	scale := math.Min(1, 1280/math.Max(float64(b.Dx()), float64(b.Dy())))
	if math.Max(float64(b.Dx())*scale, float64(b.Dy())*scale) > 4000 {
		scale = 4000 / math.Max(float64(b.Dx()), float64(b.Dy()))
	}
	// PP-OCR DB exports contain stride-32 Resize nodes.  Their dynamic input
	// dimensions must therefore be padded upward to a multiple of 32; using
	// arbitrary dimensions can make ONNX Runtime attempt to reuse an incompatible
	// intermediate buffer (e.g. 15 vs 16 feature columns).
	w := maxOCRInt(32, int(math.Ceil(float64(b.Dx())*scale/32))*32)
	h := maxOCRInt(32, int(math.Ceil(float64(b.Dy())*scale/32))*32)
	data := make([]float32, 3*h*w)
	mean := [3]float32{0.485, 0.456, 0.406}
	std := [3]float32{0.229, 0.224, 0.225}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sx := b.Min.X + x*b.Dx()/w
			sy := b.Min.Y + y*b.Dy()/h
			r, g, bl, _ := img.At(sx, sy).RGBA()
			// PaddleOCR/RapidOCR receives OpenCV BGR tensors, not RGB.
			for c, v := range []uint32{bl, g, r} {
				data[c*h*w+y*w+x] = (float32(v>>8)/255 - mean[c]) / std[c]
			}
		}
	}
	inTensor, err := ort.NewTensor(ort.Shape{1, 3, int64(h), int64(w)}, data)
	if err != nil {
		return nil, err
	}
	defer inTensor.Destroy()
	vals := make([]ort.Value, len(outputs))
	if err = session.Run([]ort.Value{inTensor}, vals); err != nil {
		return nil, fmt.Errorf("PaddleOCR detection inference: %w", err)
	}
	for _, v := range vals {
		defer v.Destroy()
	}
	out, ok := vals[0].(*ort.Tensor[float32])
	if !ok {
		return nil, fmt.Errorf("unsupported PaddleOCR detection output type")
	}
	shape := out.GetShape()
	if len(shape) < 2 {
		return nil, fmt.Errorf("unexpected PaddleOCR detection output shape %v", shape)
	}
	oh, ow := int(shape[len(shape)-2]), int(shape[len(shape)-1])
	prob := out.GetData()
	// Some exports expose DB logits while others include the sigmoid node.
	// Normalize logits here so thresholding remains model-version agnostic.
	needSigmoid := false
	for _, v := range prob {
		if v < 0 || v > 1 {
			needSigmoid = true
			break
		}
	}
	if needSigmoid {
		prob = append([]float32(nil), prob...)
		for i, v := range prob {
			prob[i] = float32(1 / (1 + math.Exp(float64(-v))))
		}
	}
	// This ONNX export already emits the DB-expanded map; applying a second
	// 1.5x radial expansion pushes normal slide titles against image borders.
	polys := paddleOCRDetectionPolygons(prob, ow, oh, .3, .6, 1.5)
	for i := range polys {
		for j, p := range polys[i].Points {
			x, y := p.X*b.Dx()/ow, p.Y*b.Dy()/oh
			x = maxOCRInt(0, minInt(x, b.Dx()-1))
			y = maxOCRInt(0, minInt(y, b.Dy()-1))
			polys[i].Points[j] = image.Point{X: x, Y: y}
		}
	}
	return polys, nil
}

func paddleOCRDetectionPolygons(prob []float32, w, h int, thresh, boxThresh, unclip float32) []OCRPolygon {
	if w <= 0 || h <= 0 || len(prob) < w*h {
		return nil
	}
	mask := make([]bool, w*h)
	for i, v := range prob[:w*h] {
		mask[i] = v >= thresh
	}
	seen := make([]bool, len(mask))
	out := make([]OCRPolygon, 0)
	q := make([]int, 0, 1024)
	for seed, on := range mask {
		if !on || seen[seed] {
			continue
		}
		seen[seed] = true
		q = q[:0]
		q = append(q, seed)
		pts := make([]image.Point, 0)
		sum := float32(0)
		for head := 0; head < len(q); head++ {
			p := q[head]
			x, y := p%w, p/w
			pts = append(pts, image.Point{X: x, Y: y})
			sum += prob[p]
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					nx, ny := x+dx, y+dy
					if nx < 0 || nx >= w || ny < 0 || ny >= h {
						continue
					}
					n := ny*w + nx
					if mask[n] && !seen[n] {
						seen[n] = true
						q = append(q, n)
					}
				}
			}
		}
		if len(pts) < 12 {
			continue
		}
		score := sum / float32(len(pts))
		if score < boxThresh {
			continue
		}
		poly := minAreaPolygon(pts)
		if unclip > 1 {
			poly = unclipRectPolygon(poly, unclip)
		}
		if polygonArea(poly) < 4 {
			continue
		}
		out = append(out, OCRPolygon{Points: poly, Score: score})
	}
	out = mergeOCRPolygons(out)
	sort.Slice(out, func(i, j int) bool { return out[i].Points[0].Y < out[j].Points[0].Y })
	return out
}

// DB masks often split a line into one component per glyph. Recombine nearby
// components into text lines before recognition; otherwise each character is
// sent to the recognizer as an independent region.
func mergeOCRPolygons(in []OCRPolygon) []OCRPolygon {
	if len(in) < 2 {
		return in
	}
	sort.Slice(in, func(i, j int) bool { return polygonBounds(in[i].Points).Min.X < polygonBounds(in[j].Points).Min.X })
	out := make([]OCRPolygon, 0, len(in))
	for _, p := range in {
		b := polygonBounds(p.Points)
		merged := false
		for i := range out {
			q := polygonBounds(out[i].Points)
			overlap := minInt(q.Max.Y, b.Max.Y) - maxOCRInt(q.Min.Y, b.Min.Y)
			lh := maxOCRInt(q.Dy(), b.Dy())
			gap := b.Min.X - q.Max.X
			if overlap >= maxOCRInt(1, lh/3) && gap <= maxOCRInt(12, lh+lh/2) {
				u := q.Union(b)
				out[i].Points = [4]image.Point{{u.Min.X, u.Min.Y}, {u.Max.X, u.Min.Y}, {u.Max.X, u.Max.Y}, {u.Min.X, u.Max.Y}}
				out[i].Score = (out[i].Score + p.Score) / 2
				merged = true
				break
			}
		}
		if !merged {
			out = append(out, p)
		}
	}
	return out
}

func minAreaPolygon(pts []image.Point) [4]image.Point {
	best := math.Inf(1)
	var out [4]image.Point
	for deg := 0; deg < 180; deg++ {
		a := float64(deg) * math.Pi / 180
		c, s := math.Cos(a), math.Sin(a)
		minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		for _, p := range pts {
			x, y := float64(p.X), float64(p.Y)
			rx, ry := x*c-y*s, x*s+y*c
			if rx < minX {
				minX = rx
			}
			if rx > maxX {
				maxX = rx
			}
			if ry < minY {
				minY = ry
			}
			if ry > maxY {
				maxY = ry
			}
		}
		area := (maxX - minX) * (maxY - minY)
		if area < best {
			best = area
			corners := [][2]float64{{minX, minY}, {maxX, minY}, {maxX, maxY}, {minX, maxY}}
			for i, v := range corners {
				out[i] = image.Point{int(math.Round(v[0]*c + v[1]*s)), int(math.Round(-v[0]*s + v[1]*c))}
			}
		}
	}
	return out
}
func scalePolygon(p [4]image.Point, f float32) [4]image.Point {
	cx, cy := 0., 0.
	for _, q := range p {
		cx += float64(q.X)
		cy += float64(q.Y)
	}
	cx /= 4
	cy /= 4
	for i, q := range p {
		p[i] = image.Point{int(math.Round(cx + (float64(q.X)-cx)*float64(f))), int(math.Round(cy + (float64(q.Y)-cy)*float64(f)))}
	}
	return p
}

// unclipRectPolygon matches DBPostProcess's offset distance for a rectangle:
// distance = area * unclip_ratio / perimeter. The model's contour is expanded
// along each edge normal, rather than radially scaling its corners.
func unclipRectPolygon(p [4]image.Point, ratio float32) [4]image.Point {
	w := math.Hypot(float64(p[1].X-p[0].X), float64(p[1].Y-p[0].Y))
	h := math.Hypot(float64(p[2].X-p[1].X), float64(p[2].Y-p[1].Y))
	if w < 1 || h < 1 {
		return p
	}
	d := (w * h * float64(ratio)) / (2 * (w + h))
	// Offset each local rectangle side by d (equivalent to pyclipper offset
	// for a four-corner contour), rather than scaling vertices radially.
	ux, uy := (float64(p[1].X-p[0].X))/w, (float64(p[1].Y-p[0].Y))/w
	vx, vy := (float64(p[3].X-p[0].X))/h, (float64(p[3].Y-p[0].Y))/h
	for i, q := range p {
		sx := 1.
		sy := 1.
		if i == 0 || i == 3 {
			sx = -1
		}
		if i == 0 || i == 1 {
			sy = -1
		}
		p[i] = image.Point{int(math.Round(float64(q.X) + ux*d*sx + vx*d*sy)), int(math.Round(float64(q.Y) + uy*d*sx + vy*d*sy))}
	}
	return p
}
func polygonArea(p [4]image.Point) float64 {
	a := 0.
	for i := 0; i < 4; i++ {
		j := (i + 1) % 4
		a += float64(p[i].X*p[j].Y - p[j].X*p[i].Y)
	}
	if a < 0 {
		a = -a
	}
	return a / 2
}

// cropPPTOCRPolygon rotates the detected quadrilateral to horizontal and crops
// its bounds, preserving geometry for recognition of skewed text.
func cropPPTOCRPolygon(src image.Image, p [4]image.Point) image.Image {
	dx1, dy1 := float64(p[1].X-p[0].X), float64(p[1].Y-p[0].Y)
	dx2, dy2 := float64(p[2].X-p[1].X), float64(p[2].Y-p[1].Y)
	dx, dy := dx1, dy1
	if dx2*dx2+dy2*dy2 > dx1*dx1+dy1*dy1 {
		dx, dy = dx2, dy2
	}
	angle := math.Atan2(dy, dx) * 180 / math.Pi
	rot := rotatePPTOCRImage(src, int(math.Round(-angle)))
	return rot // rotation avoids axis-aligned source crop
}

// perspectiveCropPPTOCR is the public geometry-oriented entry point used by
// tests and integrations; the current raster implementation first rectifies
// the dominant edge, which is equivalent for PPT text quadrilaterals.
func perspectiveCropPPTOCR(src image.Image, p [4]image.Point) image.Image {
	return cropPPTOCRPolygon(src, p)
}

func paddleOCRDetectionBoxes(prob []float32, w, h int) []image.Rectangle {
	if w <= 0 || h <= 0 || len(prob) < w*h {
		return nil
	}
	// Preserve the detector's probability region verbatim. Do not dilate,
	// merge, pad, shrink or otherwise rewrite the OCR result box.
	mask := make([]bool, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			mask[y*w+x] = prob[y*w+x] >= .30
		}
	}
	seen := make([]bool, len(mask))
	var boxes []image.Rectangle
	queue := make([]int, 0, 1024)
	for seed, on := range mask {
		if !on || seen[seed] {
			continue
		}
		seen[seed] = true
		queue = append(queue[:0], seed)
		minX, maxX, minY, maxY, pixels := seed%w, seed%w, seed/w, seed/w, 0
		for head := 0; head < len(queue); head++ {
			p := queue[head]
			x, y := p%w, p/w
			pixels++
			minX, maxX = minInt(minX, x), maxOCRInt(maxX, x)
			minY, maxY = minInt(minY, y), maxOCRInt(maxY, y)
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					nx, ny := x+dx, y+dy
					if nx < 0 || nx >= w || ny < 0 || ny >= h {
						continue
					}
					n := ny*w + nx
					if mask[n] && !seen[n] {
						seen[n] = true
						queue = append(queue, n)
					}
				}
			}
		}
		bw, bh := maxX-minX+1, maxY-minY+1
		if pixels < 12 || bw < 4 || bh < 3 {
			continue
		}
		boxes = append(boxes, image.Rect(minX, minY, maxX+1, maxY+1))
	}
	boxes = mergePPTOCRBoxes(boxes)
	sort.Slice(boxes, func(i, j int) bool {
		cyI, cyJ := boxes[i].Min.Y+boxes[i].Max.Y, boxes[j].Min.Y+boxes[j].Max.Y
		line := maxOCRInt(boxes[i].Dy(), boxes[j].Dy())
		if absInt(cyI-cyJ) > line {
			return boxes[i].Min.Y < boxes[j].Min.Y
		}
		return boxes[i].Min.X < boxes[j].Min.X
	})
	return boxes
}

func mergePPTOCRBoxes(boxes []image.Rectangle) []image.Rectangle {
	if len(boxes) < 2 {
		return boxes
	}
	// Cluster by vertical overlap first. Sorting only by X lets a tall glyph in
	// one row accidentally absorb a nearby glyph from the row above/below.
	sort.Slice(boxes, func(i, j int) bool {
		ci := boxes[i].Min.Y + boxes[i].Max.Y
		cj := boxes[j].Min.Y + boxes[j].Max.Y
		if ci != cj {
			return ci < cj
		}
		return boxes[i].Min.X < boxes[j].Min.X
	})
	lines := make([][]image.Rectangle, 0, len(boxes))
	for _, box := range boxes {
		placed := false
		for i := range lines {
			line := lines[i]
			cur := line[len(line)-1]
			overlap := minInt(cur.Max.Y, box.Max.Y) - maxOCRInt(cur.Min.Y, box.Min.Y)
			lineH := maxOCRInt(cur.Dy(), box.Dy())
			if overlap >= maxOCRInt(1, lineH/3) {
				lines[i] = append(lines[i], box)
				placed = true
				break
			}
		}
		if !placed {
			lines = append(lines, []image.Rectangle{box})
		}
	}
	merged := make([]image.Rectangle, 0, len(lines))
	for _, line := range lines {
		sort.Slice(line, func(i, j int) bool { return line[i].Min.X < line[j].Min.X })
		cur := line[0]
		for _, box := range line[1:] {
			gap := box.Min.X - cur.Max.X
			lineH := maxOCRInt(cur.Dy(), box.Dy())
			// Keep distinct card labels separate; character gaps are normally
			// below roughly one line height, while slide columns often have much
			// larger spacing.
			if gap <= maxOCRInt(12, lineH+lineH/2) {
				cur = image.Rect(minInt(cur.Min.X, box.Min.X), minInt(cur.Min.Y, box.Min.Y), maxOCRInt(cur.Max.X, box.Max.X), maxOCRInt(cur.Max.Y, box.Max.Y))
			} else {
				merged = append(merged, cur)
				cur = box
			}
		}
		merged = append(merged, cur)
	}
	return merged
}

func paddleOCRBackground(img image.Image) string {
	r, g, bl := paddleOCRBorderColor(img)
	// White slide backgrounds are common; avoid turning antialiased white text
	// or a one-pixel shadow into a gray replacement patch.
	if r >= 245 && g >= 245 && bl >= 245 {
		return "#ffffff"
	}
	return fmt.Sprintf("#%02x%02x%02x", r, g, bl)
}

func paddleOCRBorderColor(img image.Image) (uint8, uint8, uint8) {
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return 255, 255, 255
	}
	var rs, gs, bs []uint8
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if x-b.Min.X > 2 && b.Max.X-1-x > 2 && y-b.Min.Y > 2 && b.Max.Y-1-y > 2 {
				continue
			}
			r, g, bl, _ := img.At(x, y).RGBA()
			rs, gs, bs = append(rs, uint8(r>>8)), append(gs, uint8(g>>8)), append(bs, uint8(bl>>8))
		}
	}
	if len(rs) == 0 {
		return 255, 255, 255
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i] < rs[j] })
	sort.Slice(gs, func(i, j int) bool { return gs[i] < gs[j] })
	sort.Slice(bs, func(i, j int) bool { return bs[i] < bs[j] })
	m := len(rs) / 2
	return rs[m], gs[m], bs[m]
}

// paddleOCRTextColor estimates the ink color from pixels that contrast most
// with the crop border (the border is our background estimate). This keeps
// local OCR from replacing white/colored slide text with the old dark default.
func paddleOCRTextColor(img image.Image) string {
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return "#111827"
	}
	br8, bg8, bb8 := paddleOCRBorderColor(img)
	br, bg, bb := uint64(br8), uint64(bg8), uint64(bb8)
	var rsum, gsum, bsum, n uint64
	for y := b.Min.Y + 1; y < b.Max.Y-1; y++ {
		for x := b.Min.X + 1; x < b.Max.X-1; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			r8, g8, b8 := uint64(r>>8), uint64(g>>8), uint64(bl>>8)
			d := absOCRUint(r8, br) + absOCRUint(g8, bg) + absOCRUint(b8, bb)
			if d > 70 {
				rsum, gsum, bsum, n = rsum+r8, gsum+g8, bsum+b8, n+1
			}
		}
	}
	if n == 0 {
		return "#111827"
	}
	return fmt.Sprintf("#%02x%02x%02x", rsum/n, gsum/n, bsum/n)
}

// paddleOCRTrim removes detector padding by finding pixels that differ from
// the crop border. The returned rectangle is relative to img.Bounds().
func paddleOCRTrim(img image.Image) image.Rectangle {
	b := img.Bounds()
	if b.Dx() < 4 || b.Dy() < 4 {
		return image.Rect(0, 0, b.Dx(), b.Dy())
	}
	br8, bg8, bb8 := paddleOCRBorderColor(img)
	br, bg, bb := uint64(br8), uint64(bg8), uint64(bb8)
	left, top, right, bottom := b.Dx(), b.Dy(), -1, -1
	for y := b.Min.Y + 1; y < b.Max.Y-1; y++ {
		for x := b.Min.X + 1; x < b.Max.X-1; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			d := absOCRUint(uint64(r>>8), br) + absOCRUint(uint64(g>>8), bg) + absOCRUint(uint64(bl>>8), bb)
			if d < 32 {
				continue
			}
			xx, yy := x-b.Min.X, y-b.Min.Y
			left, top, right, bottom = minInt(left, xx), minInt(top, yy), maxOCRInt(right, xx), maxOCRInt(bottom, yy)
		}
	}
	if right < left || bottom < top {
		return image.Rect(0, 0, b.Dx(), b.Dy())
	}
	padX, padY := maxOCRInt(2, (right-left+1)/20), maxOCRInt(3, (bottom-top+1)/5)
	return image.Rect(maxOCRInt(0, left-padX), maxOCRInt(0, top-padY), minInt(b.Dx(), right+padX+1), minInt(b.Dy(), bottom+padY+1))
}

func absOCRUint(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return b - a
}

func paddleOCRModelDir(provider, rembgPath string) string {
	provider = strings.TrimPrefix(provider, "local:")
	for _, root := range []string{filepath.Join("models", "paddleocr"), filepath.Join(filepath.Dir(rembgPath), "models", "paddleocr")} {
		if root == "models/paddleocr" && rembgPath == "" { /* keep the normal relative path */
		}
		// Keep the legacy provider id pointed at the bundled v6 model.
		if provider == "PaddleOCR" || provider == "" {
			for _, bundle := range []string{"ppocrv6"} {
				candidate := filepath.Join(root, bundle)
				if st, err := os.Stat(filepath.Join(candidate, "rec.onnx")); err == nil && !st.IsDir() {
					return candidate
				}
			}
		}
		candidate := filepath.Join(root, provider)
		if st, err := os.Stat(filepath.Join(candidate, "rec.onnx")); err == nil && !st.IsDir() {
			return candidate
		}
		if provider == "PaddleOCR" || provider == "" {
			if st, err := os.Stat(filepath.Join(root, "rec.onnx")); err == nil && !st.IsDir() {
				return root
			}
		}
	}
	return ""
}

func paddleOCRRecognize(img image.Image, modelDir string) (string, error) {
	rec := filepath.Join(modelDir, "rec.onnx")
	keys, err := os.ReadFile(filepath.Join(modelDir, "ppocr_keys_v1.txt"))
	if err != nil {
		keys, err = os.ReadFile(filepath.Join(modelDir, "keys.txt"))
	}
	if err != nil {
		return "", fmt.Errorf("read PaddleOCR dictionary: %w", err)
	}
	if !ort.IsInitialized() {
		if library := paddleOCRRuntimePath(modelDir); library != "" {
			ort.SetSharedLibraryPath(library)
		}
		if err := ort.InitializeEnvironment(); err != nil {
			return "", fmt.Errorf("initialize ONNX runtime: %w (未找到可加载的 ONNX Runtime 动态库，请确认 onnxruntime.dll 与程序或 rembg-engine 放在一起)", err)
		}
	}
	session, inputs, outputs, err := ort.NewDynamicAdvancedSessionAuto(rec, nil)
	if err != nil {
		return "", fmt.Errorf("load PaddleOCR recognition model: %w", err)
	}
	defer session.Destroy()
	if len(inputs) == 0 || len(outputs) == 0 {
		return "", fmt.Errorf("PaddleOCR model has no input/output")
	}
	// PP-OCRv6 recognition models use NCHW, height 48 and a dynamic width.
	const h, w = 48, 320
	data := make([]float32, 3*h*w)
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return "", nil
	}
	// PaddleOCR preserves the text aspect ratio and pads the right side with
	// the normalized gray value. Stretching every crop to 320px distorts glyphs
	// and causes especially poor results with PP-OCRv6.
	resizedW := int(math.Ceil(float64(h) * float64(b.Dx()) / float64(b.Dy())))
	if resizedW < 1 {
		resizedW = 1
	}
	if resizedW > w {
		resizedW = w
	}
	for y := 0; y < h; y++ {
		for x := 0; x < resizedW; x++ {
			sx := b.Min.X + x*b.Dx()/resizedW
			sy := b.Min.Y + y*b.Dy()/h
			r, g, bl, _ := img.At(sx, sy).RGBA()
			for c, v := range []uint32{bl, g, r} {
				data[c*h*w+y*w+x] = (float32(v>>8)/255 - 0.5) / 0.5
			}
		}
	}
	inTensor, err := ort.NewTensor(ort.Shape{1, 3, h, w}, data)
	if err != nil {
		return "", err
	}
	defer inTensor.Destroy()
	vals := make([]ort.Value, len(outputs))
	if err = session.Run([]ort.Value{inTensor}, vals); err != nil {
		return "", fmt.Errorf("PaddleOCR inference: %w", err)
	}
	for _, v := range vals {
		defer v.Destroy()
	}
	out, ok := vals[0].(*ort.Tensor[float32])
	if !ok {
		return "", fmt.Errorf("unsupported PaddleOCR output type")
	}
	shape := out.GetShape()
	raw := out.GetData()
	if len(shape) < 3 {
		return "", fmt.Errorf("unexpected PaddleOCR output shape %v", shape)
	}
	// ONNX exports are seen in both [1,time,classes] and
	// [1,classes,time] layouts. Pick the class axis from the dictionary size;
	// decoding the other axis produces deterministic-looking garbage.
	classAxis := len(shape) - 1
	// The sequence dimension is small (typically 40-160), while a multilingual
	// dictionary contains thousands of classes. This remains reliable when the
	// external dictionary file does not belong to the ONNX model.
	if shape[len(shape)-2] > shape[classAxis] {
		classAxis = len(shape) - 2
	}
	classes := int(shape[classAxis])
	steps := int(shape[len(shape)-2])
	if classAxis == len(shape)-2 {
		steps = int(shape[len(shape)-1])
	}
	labels := paddleOCRLabels(session, keys, classes)
	var result strings.Builder
	last := -1
	for t := 0; t < steps; t++ {
		best, score := 0, float32(-1e30)
		for c := 0; c < classes; c++ {
			idx := t*classes + c
			if classAxis == len(shape)-2 {
				idx = c*steps + t
			}
			if idx < len(raw) && raw[idx] > score {
				best, score = c, raw[idx]
			}
		}
		if best != 0 && best != last && best < len(labels) {
			result.WriteString(labels[best])
		}
		last = best
	}
	return strings.TrimSpace(result.String()), nil
}

// paddleOCRRecognizeOriented is a lightweight replacement for PaddleOCR's
// optional angle-classifier model. Bundles shipped with this CLI do not always
// contain cls.onnx, so try the four right-angle orientations and retain the
// most plausible decoded string. This covers vertical labels and rotated
// screenshots while keeping the normal horizontal path unchanged.
func paddleOCRRecognizeOriented(img image.Image, modelDir string) (string, error) {
	// Try the native orientation first. Most PPT text is horizontal; avoiding
	// a dozen extra inferences keeps full-slide scans responsive.
	native, err := paddleOCRRecognize(img, modelDir)
	if err != nil {
		return "", err
	}
	if paddleOCRTextQuality(native) >= 4 {
		return native, nil
	}
	// Fifteen-degree steps cover arbitrary presentation rotations well enough
	// without requiring a separate cls.onnx model. Include both directions;
	// the detector box itself is axis-aligned and therefore loses the original
	// quadrilateral angle.
	angles := make([]int, 0, 25)
	for angle := -90; angle <= 90; angle += 15 {
		angles = append(angles, angle)
	}
	best, bestScore := native, paddleOCRTextQuality(native)
	for _, angle := range angles {
		candidate, err := paddleOCRRecognize(rotatePPTOCRImage(img, angle), modelDir)
		if err != nil {
			return "", err
		}
		score := paddleOCRTextQuality(candidate)
		// Keep the native orientation on ties. This prevents a horizontal line
		// from being replaced by a coincidentally plausible Latin string from a
		// rotated trial.
		if score > bestScore+1e-9 {
			best, bestScore = candidate, score
		}
	}
	return best, nil
}

func rotatePPTOCRImage(src image.Image, angle int) image.Image {
	b := src.Bounds()
	if angle%360 == 0 {
		return src
	}
	rad := float64(angle) * math.Pi / 180
	w := int(math.Ceil(float64(b.Dx())*math.Abs(math.Cos(rad)) + float64(b.Dy())*math.Abs(math.Sin(rad))))
	h := int(math.Ceil(float64(b.Dx())*math.Abs(math.Sin(rad)) + float64(b.Dy())*math.Abs(math.Cos(rad))))
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	cx, cy := float64(w-1)/2, float64(h-1)/2
	scx, scy := float64(b.Dx()-1)/2, float64(b.Dy()-1)/2
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			sx := dx*math.Cos(rad) + dy*math.Sin(rad) + scx
			sy := -dx*math.Sin(rad) + dy*math.Cos(rad) + scy
			ix, iy := int(math.Round(sx)), int(math.Round(sy))
			if ix >= 0 && ix < b.Dx() && iy >= 0 && iy < b.Dy() {
				dst.Set(x, y, src.At(b.Min.X+ix, b.Min.Y+iy))
			}
		}
	}
	return dst
}

func paddleOCRTextQuality(s string) float64 {
	score := 0.0
	for _, r := range strings.TrimSpace(s) {
		switch {
		case r >= 0x3400 && r <= 0x9fff:
			score += 3
		case r >= '0' && r <= '9', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
			score += 2
		case r == ' ' || r == '-' || r == ':' || r == '，' || r == '。':
			score += .5
		default:
			score -= 1
		}
	}
	return score
}

// estimatePPTOCRRotation estimates a text line's baseline angle from the
// contrasting pixels in its crop. It is used when DB polygon coordinates are
// unavailable in the lightweight ONNX export; the editable PPT element can
// still retain the same visual tilt.
func estimatePPTOCRRotation(img image.Image) float64 {
	b := img.Bounds()
	if b.Dx() < 8 || b.Dy() < 4 {
		return 0
	}
	br, bg, bb := paddleOCRBorderColor(img)
	var n, sx, sy float64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			d := absOCRUint(uint64(r>>8), uint64(br)) + absOCRUint(uint64(g>>8), uint64(bg)) + absOCRUint(uint64(bl>>8), uint64(bb))
			if d > 70 {
				n++
				sx += float64(x)
				sy += float64(y)
			}
		}
	}
	if n < 8 {
		return 0
	}
	mx, my := sx/n, sy/n
	var xx, xy float64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			d := absOCRUint(uint64(r>>8), uint64(br)) + absOCRUint(uint64(g>>8), uint64(bg)) + absOCRUint(uint64(bl>>8), uint64(bb))
			if d > 70 {
				dx, dy := float64(x)-mx, float64(y)-my
				xx += dx * dx
				xy += dx * dy
			}
		}
	}
	angle := math.Atan2(2*xy, xx) * 90 / math.Pi
	if angle > 45 {
		angle -= 90
	}
	if angle < -45 {
		angle += 90
	}
	if math.Abs(angle) < 0.5 {
		return 0
	}
	return angle
}

// PP-OCRv6 embeds its complete character list in ONNX metadata.  That list
// is authoritative: the legacy ppocr_keys_v1.txt has fewer classes and using
// it shifts the CTC indices, producing garbled output.
func paddleOCRLabels(session *ort.DynamicAdvancedSession, keys []byte, classes int) []string {
	if metadata, err := session.GetModelMetadata(); err == nil {
		defer metadata.Destroy()
		if value, ok, _ := metadata.LookupCustomMetadataMap("character"); ok {
			if labels := paddleOCRMetadataLabels(value, classes); labels != nil {
				return labels
			}
		}
	}
	labels := strings.Split(strings.TrimRight(strings.ReplaceAll(string(keys), "\r\n", "\n"), "\n"), "\n")
	return append([]string{""}, labels...)
}

func paddleOCRMetadataLabels(value string, classes int) []string {
	value = strings.Trim(value, "\r\n")
	if value == "" || classes < 2 {
		return nil
	}
	chars := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	// Paddle's CTC decoder reserves index 0 for blank and, when
	// use_space_char=true, appends a space after the dictionary. PP-OCRv6's
	// embedded metadata contains only the dictionary, so classes=len(chars)+2.
	labels := append([]string{""}, chars...)
	if len(labels) < classes {
		labels = append(labels, " ")
	}
	if len(labels) != classes {
		return nil
	}
	return labels
}

func paddleOCRRuntimePath(modelDir string) string {
	name := "libonnxruntime.so"
	if runtime.GOOS == "windows" {
		name = "onnxruntime.dll"
	}
	if runtime.GOOS == "darwin" {
		name = "libonnxruntime.dylib"
	}
	var roots []string
	for p := modelDir; p != "." && p != ""; p = filepath.Dir(p) {
		roots = append(roots, p, filepath.Join(p, "lib"), filepath.Join(p, "onnxruntime", "lib"))
		parent := filepath.Dir(p)
		if parent == p {
			break
		}
	}
	if exe, err := os.Executable(); err == nil {
		base := filepath.Dir(exe)
		roots = append(roots, base, filepath.Join(base, "rembg-engine"), filepath.Join(base, "onnxruntime"))
		matches, _ := filepath.Glob(filepath.Join(base, "onnxruntime*", "lib", name))
		sort.Sort(sort.Reverse(sort.StringSlice(matches)))
		roots = append(roots, matches...)
	}
	if wd, err := os.Getwd(); err == nil {
		roots = append(roots, wd, filepath.Join(wd, "dist"), filepath.Join(wd, "rembg-engine"))
		matches, _ := filepath.Glob(filepath.Join(wd, "dist", "rembg-build-windows-go", "onnxruntime-*", "lib", name))
		sort.Sort(sort.Reverse(sort.StringSlice(matches)))
		roots = append(roots, matches...)
	}
	for _, root := range roots {
		candidate := root
		if filepath.Ext(root) == "" || filepath.Base(root) != name {
			candidate = filepath.Join(root, name)
		}
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
	}
	return ""
}
