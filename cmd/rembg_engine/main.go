package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"

	ort "github.com/yalue/onnxruntime_go"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "rembg-engine:", err)
		os.Exit(1)
	}
}

func run() error {
	model := flag.String("model", "", "model name or ONNX path")
	edge := flag.String("edge", "product", "none, product, or fine")
	selfTest := flag.Bool("self-test", false, "run with a generated image")
	validateModel := flag.Bool("validate-model", false, "load and inspect the model, then exit")
	worker := flag.Bool("worker", false, "serve length-prefixed images on stdin/stdout")
	runtimePath := flag.String("runtime", "", "path to ONNX Runtime shared library")
	flag.Parse()
	if *edge != "none" && *edge != "product" && *edge != "fine" {
		return fmt.Errorf("unsupported edge mode %q", *edge)
	}
	exe, _ := os.Executable()
	base := filepath.Dir(exe)
	if *runtimePath == "" {
		name := "onnxruntime.dll"
		if runtime.GOOS == "linux" {
			name = "libonnxruntime.so"
		}
		if runtime.GOOS == "darwin" {
			name = "libonnxruntime.dylib"
		}
		*runtimePath = filepath.Join(base, name)
	}
	modelPath, err := resolveModel(base, *model)
	if err != nil {
		return err
	}
	ort.SetSharedLibraryPath(*runtimePath)
	if err := ort.InitializeEnvironment(); err != nil {
		return fmt.Errorf("initialize ONNX Runtime: %w", err)
	}
	defer ort.DestroyEnvironment()
	runner, err := newModelRunner(modelPath)
	if err != nil {
		return err
	}
	defer runner.close()
	if *validateModel {
		return nil
	}
	if *worker {
		return runWorker(runner, *edge)
	}
	var source []byte
	if *selfTest {
		pic := image.NewNRGBA(image.Rect(0, 0, 32, 32))
		for y := 0; y < 32; y++ {
			for x := 0; x < 32; x++ {
				pic.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 8), G: uint8(y * 8), B: 128, A: 255})
			}
		}
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, pic); err != nil {
			return err
		}
		source = encoded.Bytes()
	} else {
		source, err = io.ReadAll(io.LimitReader(os.Stdin, 40<<20))
		if err != nil {
			return fmt.Errorf("read input image: %w", err)
		}
	}
	result, err := runner.infer(source, *edge)
	if err != nil {
		return err
	}
	if *selfTest {
		return nil
	}
	return png.Encode(os.Stdout, result)
}

func resolveModel(engineDir, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("--model is required")
	}
	if filepath.Base(value) != value {
		return "", fmt.Errorf("model must be a file name from the models directory")
	}
	if !strings.HasSuffix(strings.ToLower(value), ".onnx") {
		value += ".onnx"
	}
	path := filepath.Join(engineDir, "models", value)
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return "", fmt.Errorf("model not found: %s", value)
	}
	return path, nil
}

type modelRunner struct {
	session     *ort.DynamicAdvancedSession
	shape       ort.Shape
	nchw        bool
	input       ort.InputOutputInfo
	outputCount int
	options     *ort.SessionOptions
}

func newModelRunner(modelPath string) (*modelRunner, error) {
	options, err := ort.NewSessionOptions()
	if err != nil {
		return nil, err
	}
	_ = options.SetExecutionMode(ort.ExecutionModeSequential)
	_ = options.SetGraphOptimizationLevel(ort.GraphOptimizationLevelEnableAll)
	_ = options.SetInterOpNumThreads(1)
	_ = options.SetIntraOpNumThreads(threadCount(modelPath))
	_ = options.SetCpuMemArena(false)
	_ = options.SetMemPattern(false)
	session, inputs, outputs, err := ort.NewDynamicAdvancedSessionAuto(modelPath, options)
	if err != nil {
		options.Destroy()
		return nil, fmt.Errorf("load and inspect ONNX model: %w", err)
	}
	if len(inputs) != 1 || len(outputs) == 0 {
		session.Destroy()
		options.Destroy()
		return nil, fmt.Errorf("model needs one image input and at least one output (got %d/%d)", len(inputs), len(outputs))
	}
	in := inputs[0]
	if in.DataType != ort.TensorElementDataTypeFloat || len(in.Dimensions) != 4 {
		session.Destroy()
		options.Destroy()
		return nil, fmt.Errorf("unsupported input: %s", in.String())
	}
	shape, nchw, err := concreteImageShape(in.Dimensions)
	if err != nil {
		session.Destroy()
		options.Destroy()
		return nil, err
	}
	return &modelRunner{session: session, shape: shape, nchw: nchw, input: in, outputCount: len(outputs), options: options}, nil
}

func (r *modelRunner) close() { _ = r.session.Destroy(); _ = r.options.Destroy() }

func (r *modelRunner) infer(sourceBytes []byte, edge string) (*image.NRGBA, error) {
	source, _, err := image.Decode(bytes.NewReader(sourceBytes))
	if err != nil {
		return nil, fmt.Errorf("decode input image: %w", err)
	}
	var data []float32
	if r.nchw {
		data = imageTensor(source, int(r.shape[2]), int(r.shape[3]), true, r.input.Name)
	} else {
		data = imageTensor(source, int(r.shape[1]), int(r.shape[2]), false, r.input.Name)
	}
	tensor, err := ort.NewTensor(r.shape, data)
	if err != nil {
		return nil, err
	}
	defer tensor.Destroy()
	// The full-resolution decoded image can be much larger than the model
	// tensor. Release it before BiRefNet allocates its large decoder buffers;
	// decode it again only after inference for full-resolution composition.
	source = nil
	data = nil
	debug.FreeOSMemory()
	values := make([]ort.Value, r.outputCount)
	if err := r.session.Run([]ort.Value{tensor}, values); err != nil {
		return nil, fmt.Errorf("run ONNX model: %w", err)
	}
	defer func() {
		for _, value := range values {
			if value != nil {
				_ = value.Destroy()
			}
		}
	}()
	mask, mw, mh, err := firstMask(values)
	if err != nil {
		return nil, err
	}
	source, _, err = image.Decode(bytes.NewReader(sourceBytes))
	if err != nil {
		return nil, fmt.Errorf("decode source for output: %w", err)
	}
	return compose(source, mask, mw, mh, edge), nil
}

func runWorker(runner *modelRunner, edge string) error {
	for {
		var size uint64
		if err := binary.Read(os.Stdin, binary.LittleEndian, &size); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if size == 0 || size > 50<<20 {
			return fmt.Errorf("invalid worker image size: %d", size)
		}
		source := make([]byte, int(size))
		if _, err := io.ReadFull(os.Stdin, source); err != nil {
			return err
		}
		result, err := runner.infer(source, edge)
		var payload []byte
		failed := err != nil
		if failed {
			payload = []byte(err.Error())
		} else {
			var encoded bytes.Buffer
			if err := png.Encode(&encoded, result); err != nil {
				failed = true
				payload = []byte(err.Error())
			} else {
				payload = encoded.Bytes()
			}
		}
		header := uint64(len(payload))
		if failed {
			header |= uint64(1) << 63
		}
		if err := binary.Write(os.Stdout, binary.LittleEndian, header); err != nil {
			return err
		}
		if _, err := os.Stdout.Write(payload); err != nil {
			return err
		}
	}
}

func threadCount(modelPath string) int {
	// BiRefNet's deformable ASPP decoder allocates a large temporary buffer
	// per worker. One worker is slower but avoids multi-gigabyte peak memory.
	if strings.Contains(strings.ToLower(filepath.Base(modelPath)), "birefnet") {
		return 1
	}
	if n, err := strconv.Atoi(os.Getenv("OMP_NUM_THREADS")); err == nil && n > 0 {
		return n
	}
	return min(8, runtime.NumCPU())
}

func concreteImageShape(value ort.Shape) (ort.Shape, bool, error) {
	s := value.Clone()
	for i := range s {
		if s[i] <= 0 {
			s[i] = 1
		}
	}
	if s[0] != 1 {
		return nil, false, fmt.Errorf("only batch size 1 is supported: %s", value)
	}
	if s[1] == 3 || (s[1] <= 1 && s[3] != 3) {
		s[1] = 3
		if s[2] <= 1 {
			s[2] = 1024
		}
		if s[3] <= 1 {
			s[3] = 1024
		}
		return s, true, nil
	}
	if s[3] == 3 || s[3] <= 1 {
		s[3] = 3
		if s[1] <= 1 {
			s[1] = 1024
		}
		if s[2] <= 1 {
			s[2] = 1024
		}
		return s, false, nil
	}
	return nil, false, fmt.Errorf("cannot infer RGB tensor layout from %s", value)
}

func imageTensor(src image.Image, h, w int, nchw bool, inputName string) []float32 {
	data := make([]float32, 3*w*h)
	bria := strings.Contains(strings.ToLower(inputName), "pixel")
	b := src.Bounds()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fx := b.Min.X + (2*x+1)*b.Dx()/(2*w)
			fy := b.Min.Y + (2*y+1)*b.Dy()/(2*h)
			r, g, bl, _ := src.At(fx, fy).RGBA()
			v := [3]float32{float32(r) / 65535, float32(g) / 65535, float32(bl) / 65535}
			if bria {
				for c := range v {
					v[c] -= .5
				}
			} else {
				mean := [3]float32{.485, .456, .406}
				std := [3]float32{.229, .224, .225}
				for c := range v {
					v[c] = (v[c] - mean[c]) / std[c]
				}
			}
			for c := 0; c < 3; c++ {
				if nchw {
					data[c*w*h+y*w+x] = v[c]
				} else {
					data[(y*w+x)*3+c] = v[c]
				}
			}
		}
	}
	return data
}

func firstMask(values []ort.Value) ([]float32, int, int, error) {
	for _, value := range values {
		t, ok := value.(*ort.Tensor[float32])
		if !ok {
			continue
		}
		s := t.GetShape()
		if len(s) < 2 {
			continue
		}
		w, h := int(s[len(s)-1]), int(s[len(s)-2])
		if w <= 1 || h <= 1 {
			continue
		}
		data := t.GetData()
		if len(data) < w*h {
			continue
		}
		return data[:w*h], w, h, nil
	}
	return nil, 0, 0, fmt.Errorf("model returned no float mask image")
}

func compose(src image.Image, mask []float32, mw, mh int, edge string) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	alpha := make([]float32, w*h)
	lo, hi := float32(math.Inf(1)), float32(math.Inf(-1))
	for _, v := range mask {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	logits := lo < 0 || hi > 1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sx := min(mw-1, x*mw/w)
			sy := min(mh-1, y*mh/h)
			v := mask[sy*mw+sx]
			if logits {
				v = 1 / (1 + float32(math.Exp(float64(-max(-30, min(30, v))))))
			}
			alpha[y*w+x] = max(0, min(1, v))
		}
	}
	if edge != "none" {
		alpha = smoothAlpha(alpha, w, h, edge == "product")
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			pixel := color.NRGBAModel.Convert(src.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			ma := uint8(alpha[y*w+x]*255 + .5)
			ma = uint8(uint16(ma) * uint16(pixel.A) / 255)
			out.SetNRGBA(x, y, color.NRGBA{pixel.R, pixel.G, pixel.B, ma})
		}
	}
	return out
}

func smoothAlpha(src []float32, w, h int, product bool) []float32 {
	out := append([]float32(nil), src...)
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			i := y*w + x
			minV, maxV, sum := float32(1), float32(0), float32(0)
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					v := src[i+dy*w+dx]
					minV = min(minV, v)
					maxV = max(maxV, v)
					sum += v
				}
			}
			if maxV-minV > .01 {
				out[i] = sum / 9
			}
			if product {
				if out[i] < .016 {
					out[i] = 0
				}
				if out[i] > .984 {
					out[i] = 1
				}
			}
		}
	}
	return out
}
