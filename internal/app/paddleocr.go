package app

// The PPT OCR endpoint uses the recognition part of a PaddleOCR bundle for a
// user-selected region.  Detection is intentionally left to the editor (the
// selection is already a text line/box), which keeps this path fast and makes
// it useful for replacing text in generated slide images.
import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	ort "github.com/yalue/onnxruntime_go"
)

func paddleOCRBackground(img image.Image) string {
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return "#ffffff"
	}
	var r, g, bl uint64
	n := uint64(0)
	// Sample a thin border: text is usually centered, while the border carries
	// the local slide background used to hide the raster glyphs.
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if x-b.Min.X > 2 && b.Max.X-1-x > 2 && y-b.Min.Y > 2 && b.Max.Y-1-y > 2 {
				continue
			}
			cr, cg, cb, _ := img.At(x, y).RGBA()
			r += uint64(cr >> 8)
			g += uint64(cg >> 8)
			bl += uint64(cb >> 8)
			n++
		}
	}
	if n == 0 {
		return "#ffffff"
	}
	return fmt.Sprintf("#%02x%02x%02x", r/n, g/n, bl/n)
}

func paddleOCRModelDir(provider, rembgPath string) string {
	provider = strings.TrimPrefix(provider, "local:")
	for _, root := range []string{filepath.Join("models", "paddleocr"), filepath.Join(filepath.Dir(rembgPath), "models", "paddleocr")} {
		if root == "models/paddleocr" && rembgPath == "" { /* keep the normal relative path */
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
	// PP-OCRv4 recognition models use NCHW, height 48 and a dynamic width.
	const h, w = 48, 320
	data := make([]float32, 3*h*w)
	b := img.Bounds()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sx := b.Min.X + x*b.Dx()/w
			sy := b.Min.Y + y*b.Dy()/h
			r, g, bl, _ := img.At(sx, sy).RGBA()
			for c, v := range []uint32{r, g, bl} {
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
	classes := int(shape[len(shape)-1])
	steps := len(raw) / classes
	labels := strings.Split(strings.ReplaceAll(string(keys), "\r\n", "\n"), "\n")
	labels = append([]string{""}, labels...)
	var result strings.Builder
	last := -1
	for t := 0; t < steps; t++ {
		best, score := 0, float32(-1e30)
		for c := 0; c < classes; c++ {
			if raw[t*classes+c] > score {
				best, score = c, raw[t*classes+c]
			}
		}
		if best != 0 && best != last && best < len(labels) {
			result.WriteString(labels[best])
		}
		last = best
	}
	return strings.TrimSpace(result.String()), nil
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
