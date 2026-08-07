package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

const maxRembgInputSize = 50 << 20
const maxCutoutModelSize = int64(5 << 30)

type cutoutResult struct {
	data    []byte
	created time.Time
}

func (s *webUIServer) storeCutoutResult(data []byte) string {
	var idBytes [16]byte
	_, _ = rand.Read(idBytes[:])
	id := hex.EncodeToString(idBytes[:])
	now := time.Now()
	s.cutoutResultsMu.Lock()
	defer s.cutoutResultsMu.Unlock()
	if s.cutoutResults == nil {
		s.cutoutResults = make(map[string]cutoutResult)
	}
	for key, result := range s.cutoutResults {
		if now.Sub(result.created) > 10*time.Minute {
			delete(s.cutoutResults, key)
		}
	}
	s.cutoutResults[id] = cutoutResult{data: data, created: now}
	return id
}

func (s *webUIServer) handleCutoutResult(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/image/remove-background/result/")
	if len(id) != 32 {
		http.NotFound(w, r)
		return
	}
	s.cutoutResultsMu.Lock()
	result, ok := s.cutoutResults[id]
	if ok && r.Method == http.MethodDelete {
		delete(s.cutoutResults, id)
	}
	s.cutoutResultsMu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(result.data)))
	written, err := w.Write(result.data)
	if err != nil || written != len(result.data) {
		s.logCutout("result interrupted id=%s output=%d written=%d error=%v", id, len(result.data), written, err)
	} else {
		s.logCutout("result delivered id=%s output=%d", id, len(result.data))
	}
}

func (s *webUIServer) writeCutoutOutput(w http.ResponseWriter, r *http.Request, output []byte, warning string) (int, error) {
	if strings.Contains(strings.ToLower(r.Header.Get("Accept")), "application/json") {
		id := s.storeCutoutResult(output)
		writeJSON(w, http.StatusOK, map[string]string{
			"url":     "/api/image/remove-background/result/" + id,
			"warning": warning,
		})
		return len(output), nil
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(output)))
	w.WriteHeader(http.StatusOK)
	return w.Write(output)
}

func (s *webUIServer) logCutout(format string, args ...any) {
	if s.diagnosticLog == nil {
		return
	}
	s.diagnosticMu.Lock()
	defer s.diagnosticMu.Unlock()
	_, _ = fmt.Fprintf(s.diagnosticLog, "%s [remove-background] %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
}

type cutoutWorker struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr bytes.Buffer
	model  string
	edge   string
}

func (w *cutoutWorker) stop() {
	_ = w.stdin.Close()
	if w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
	}
	_ = w.cmd.Wait()
}

func (s *webUIServer) stopCutoutWorker() {
	s.rembgWorkerMu.Lock()
	defer s.rembgWorkerMu.Unlock()
	if s.rembgWorker != nil {
		s.rembgWorker.stop()
		s.rembgWorker = nil
	}
}

func (s *webUIServer) persistentCutout(model, edge string, input []byte) ([]byte, error) {
	s.rembgWorkerMu.Lock()
	defer s.rembgWorkerMu.Unlock()
	worker := s.rembgWorker
	if worker != nil && (worker.model != model || worker.edge != edge) {
		worker.stop()
		s.rembgWorker = nil
		worker = nil
	}
	if worker == nil {
		cmd := exec.Command(s.rembgPath, "--worker", "--model", model, "--edge", edge)
		hideSubprocessWindow(cmd)
		cmd.Env = append(os.Environ(), "MODEL_CHECKSUM_DISABLED=1")
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		worker = &cutoutWorker{cmd: cmd, stdin: stdin, stdout: stdout, model: model, edge: edge}
		cmd.Stderr = &worker.stderr
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		s.rembgWorker = worker
	}
	fail := func(err error) ([]byte, error) {
		message := decodeEngineError(worker.stderr.Bytes())
		worker.stop()
		s.rembgWorker = nil
		if message != "" {
			return nil, fmt.Errorf("%w: %s", err, message)
		}
		return nil, err
	}
	if err := binary.Write(worker.stdin, binary.LittleEndian, uint64(len(input))); err != nil {
		return fail(err)
	}
	if _, err := worker.stdin.Write(input); err != nil {
		return fail(err)
	}
	var header uint64
	if err := binary.Read(worker.stdout, binary.LittleEndian, &header); err != nil {
		return fail(err)
	}
	failed := header&(uint64(1)<<63) != 0
	size := header &^ (uint64(1) << 63)
	if size > 100<<20 {
		return fail(fmt.Errorf("invalid worker response size: %d", size))
	}
	payload := make([]byte, int(size))
	if _, err := io.ReadFull(worker.stdout, payload); err != nil {
		return fail(err)
	}
	if failed {
		return nil, fmt.Errorf("%s", strings.TrimSpace(string(payload)))
	}
	return payload, nil
}

type cutoutProfile struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Default     bool   `json:"default,omitempty"`
}

func (s *webUIServer) cutoutProfiles() []cutoutProfile {
	if strings.TrimSpace(s.rembgPath) == "" || !strings.HasPrefix(strings.ToLower(filepath.Base(s.rembgPath)), "rembg-engine") {
		return nil
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(s.rembgPath), "models", "*.onnx"))
	profiles := make([]cutoutProfile, 0, len(files))
	for _, file := range files {
		id := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		profiles = append(profiles, cutoutProfile{ID: id, Label: id, Description: "本地模型 · " + filepath.Base(file)})
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].ID < profiles[j].ID })
	preferred := []string{"birefnet-general-lite", "isnet-general-use", "silueta"}
	for _, want := range preferred {
		for i := range profiles {
			if strings.EqualFold(profiles[i].ID, want) {
				profiles[i].Default = true
				return profiles
			}
		}
	}
	if len(profiles) > 0 {
		profiles[0].Default = true
	}
	return profiles
}

func normalizeCutoutEdge(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "product", true
	}
	return value, value == "none" || value == "product" || value == "fine"
}

func (s *webUIServer) handleCutoutProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"profiles": s.cutoutProfiles()})
}

func (s *webUIServer) cutoutModelsDir() (string, bool) {
	if strings.TrimSpace(s.rembgPath) == "" || !strings.HasPrefix(strings.ToLower(filepath.Base(s.rembgPath)), "rembg-engine") {
		return "", false
	}
	return filepath.Join(filepath.Dir(s.rembgPath), "models"), true
}

func cutoutModelName(rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return "", fmt.Errorf("请输入有效的 HTTP(S) 模型下载链接")
	}
	name, err := url.PathUnescape(filepath.Base(parsed.Path))
	if err == nil && filepath.Base(name) == name && strings.HasSuffix(strings.ToLower(name), ".onnx") {
		return name, nil
	}
	for _, key := range []string{"filename", "file", "name"} {
		candidate := filepath.Base(strings.TrimSpace(parsed.Query().Get(key)))
		if candidate != "." && strings.HasSuffix(strings.ToLower(candidate), ".onnx") {
			return candidate, nil
		}
	}
	return "", nil
}

func downloadedCutoutModelName(suggested, contentDisposition string, now time.Time) string {
	if suggested != "" {
		return suggested
	}
	if _, params, err := mime.ParseMediaType(contentDisposition); err == nil {
		candidate := filepath.Base(strings.TrimSpace(params["filename"]))
		if candidate != "." && strings.HasSuffix(strings.ToLower(candidate), ".onnx") {
			return candidate
		}
	}
	return "model-" + now.Format("20060102-150405") + ".onnx"
}

func cutoutModelLogURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "invalid-url"
	}
	// Download queries commonly contain temporary credentials or signatures.
	return parsed.Scheme + "://" + parsed.Host + parsed.EscapedPath()
}

func (s *webUIServer) validateCutoutModel(ctx context.Context, modelName string) error {
	if s.cutoutValidator != nil {
		return s.cutoutValidator(ctx, modelName)
	}
	// --self-test is supported by both old and new bundled engines. Besides
	// loading the ONNX graph, it runs one generated image through the model,
	// which catches unsupported operators and incompatible output layouts too.
	validation := exec.CommandContext(ctx, s.rembgPath, "--self-test", "--model", modelName)
	hideSubprocessWindow(validation)
	validation.Env = append(os.Environ(), "MODEL_CHECKSUM_DISABLED=1")
	output, err := validation.CombinedOutput()
	if err == nil {
		return nil
	}
	message := decodeEngineError(output)
	if message == "" {
		message = err.Error()
	}
	return fmt.Errorf("%s", message)
}

func (s *webUIServer) handleCutoutModelDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var input struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "模型下载参数无效")
		return
	}
	name, err := cutoutModelName(input.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.logCutout("model download requested source=%s suggested_name=%q remote=%s", cutoutModelLogURL(input.URL), name, r.RemoteAddr)
	modelsDir, ok := s.cutoutModelsDir()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "离线抠图引擎未包含在安装包中")
		return
	}
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "无法创建 models 目录: "+err.Error())
		return
	}
	client := &http.Client{
		Timeout: 30 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 || (req.URL.Scheme != "http" && req.URL.Scheme != "https") || req.URL.User != nil {
				return fmt.Errorf("模型下载重定向无效")
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, strings.TrimSpace(input.URL), nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "模型下载链接无效")
		return
	}
	response, err := client.Do(req)
	if err != nil {
		s.logCutout("model download connection failed source=%s error=%q", cutoutModelLogURL(input.URL), err.Error())
		writeError(w, http.StatusBadGateway, "模型下载失败: "+err.Error())
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		s.logCutout("model download rejected source=%s status=%d content_type=%q", cutoutModelLogURL(input.URL), response.StatusCode, response.Header.Get("Content-Type"))
		writeError(w, http.StatusBadGateway, fmt.Sprintf("模型下载失败: 远端返回 HTTP %d", response.StatusCode))
		return
	}
	if response.ContentLength > maxCutoutModelSize {
		writeError(w, http.StatusRequestEntityTooLarge, "模型文件超过 5 GB")
		return
	}
	name = downloadedCutoutModelName(name, response.Header.Get("Content-Disposition"), time.Now())
	s.logCutout("model download started source=%s final_source=%s name=%q content_length=%d content_type=%q disposition=%q", cutoutModelLogURL(input.URL), cutoutModelLogURL(response.Request.URL.String()), name, response.ContentLength, response.Header.Get("Content-Type"), response.Header.Get("Content-Disposition"))
	temporary, err := os.CreateTemp(modelsDir, ".download-*.onnx")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法写入 models 目录: "+err.Error())
		return
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	encoder := json.NewEncoder(w)
	flusher, _ := w.(http.Flusher)
	emit := func(value map[string]any) {
		_ = encoder.Encode(value)
		if flusher != nil {
			flusher.Flush()
		}
	}
	total := response.ContentLength
	if total < 0 {
		total = 0
	}
	emit(map[string]any{"downloaded": int64(0), "total": total})
	buffer := make([]byte, 256<<10)
	limited := io.LimitReader(response.Body, maxCutoutModelSize+1)
	var written int64
	var copyErr error
	for {
		count, readErr := limited.Read(buffer)
		if count > 0 {
			if _, err := temporary.Write(buffer[:count]); err != nil {
				copyErr = err
				break
			}
			written += int64(count)
			emit(map[string]any{"downloaded": written, "total": total})
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			copyErr = readErr
			break
		}
	}
	closeErr := temporary.Close()
	if copyErr != nil || closeErr != nil {
		s.logCutout("model download interrupted name=%q downloaded=%d copy_error=%v close_error=%v", name, written, copyErr, closeErr)
		emit(map[string]any{"error": "模型下载中断"})
		return
	}
	if written > maxCutoutModelSize {
		s.logCutout("model download too large name=%q downloaded=%d", name, written)
		emit(map[string]any{"error": "模型文件超过 5 GB"})
		return
	}
	emit(map[string]any{"downloaded": written, "total": written, "validating": true})
	if validationErr := s.validateCutoutModel(r.Context(), filepath.Base(temporaryName)); validationErr != nil {
		message := validationErr.Error()
		runes := []rune(message)
		if len(runes) > 300 {
			message = string(runes[:300])
		}
		s.logCutout("model validation failed name=%q size=%d engine=%q error=%q", name, written, s.rembgPath, validationErr.Error())
		emit(map[string]any{"error": "模型无法加载，可能下载链接不是 ONNX 文件，或模型与当前抠图引擎不兼容：" + message})
		return
	}
	destination := filepath.Join(modelsDir, name)
	if _, err := os.Stat(destination); err == nil {
		s.logCutout("model save conflict name=%q size=%d", name, written)
		emit(map[string]any{"error": "models 目录中已存在同名模型"})
		return
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		s.logCutout("model save failed name=%q size=%d error=%q", name, written, err.Error())
		emit(map[string]any{"error": "保存模型失败: " + err.Error()})
		return
	}
	s.logCutout("model download completed name=%q size=%d destination=%q", name, written, destination)
	emit(map[string]any{"name": name, "size": written, "done": true})
}

func (s *webUIServer) handleOpenCutoutModelsDir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	dir, ok := s.cutoutModelsDir()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "离线抠图引擎未包含在安装包中")
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "无法创建 models 目录: "+err.Error())
		return
	}
	if err := openFile(dir); err != nil {
		writeError(w, http.StatusInternalServerError, "无法打开 models 目录: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"models_dir": dir})
}

func decodeEngineError(data []byte) string {
	if len(data) >= 4 {
		zeroes := 0
		for i := 1; i < len(data); i += 2 {
			if data[i] == 0 {
				zeroes++
			}
		}
		if zeroes*3 > len(data)/2 {
			units := make([]uint16, 0, len(data)/2)
			for i := 0; i+1 < len(data); i += 2 {
				units = append(units, uint16(data[i])|uint16(data[i+1])<<8)
			}
			return strings.TrimSpace(string(utf16.Decode(units)))
		}
	}
	return strings.TrimSpace(strings.ToValidUTF8(string(data), "�"))
}

func findRembgEngine() string {
	executable, _ := os.Executable()
	return findRembgEngineFrom(executable, runtime.GOOS, exec.LookPath)
}

func findRembgEngineFrom(executable, goos string, lookPath func(string) (string, error)) string {
	dir := filepath.Dir(executable)
	name := "rembg-engine"
	if goos == "windows" {
		name += ".exe"
	}
	candidates := []string{filepath.Join(dir, "rembg-engine", name)}
	if goos == "darwin" {
		candidates = append(candidates, filepath.Clean(filepath.Join(dir, "..", "Resources", "rembg-engine", name)))
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	if path, err := lookPath("rembg"); err == nil {
		return path
	}
	return ""
}

// handleRemoveBackground delegates matting to the frozen offline engine shipped
// beside the app. A PATH-installed rembg remains supported for development.
func (s *webUIServer) handleRemoveBackground(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRembgInputSize)
	file, _, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "读取图片失败: "+err.Error())
		return
	}
	defer file.Close()
	input, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "读取图片失败: "+err.Error())
		return
	}
	if len(input) == 0 {
		writeError(w, http.StatusBadRequest, "图片不能为空")
		return
	}
	if config, _, configErr := image.DecodeConfig(bytes.NewReader(input)); configErr == nil {
		rgbaMiB := float64(config.Width*config.Height*4) / (1024 * 1024)
		s.logCutout("request input=%d dimensions=%dx%d rgba=%.1fMiB remote=%s", len(input), config.Width, config.Height, rgbaMiB, r.RemoteAddr)
	} else {
		s.logCutout("request input=%d dimensions=unknown remote=%s", len(input), r.RemoteAddr)
	}
	// Read the upload before reporting a missing sidecar. Returning while the
	// browser is still writing a multipart body is surfaced as CONNECTION_RESET
	// on Windows instead of the useful JSON error response.
	if strings.TrimSpace(s.rembgPath) == "" {
		writeError(w, http.StatusServiceUnavailable, "离线抠图引擎未包含在安装包中")
		return
	}
	profiles := s.cutoutProfiles()
	model := strings.TrimSpace(r.FormValue("profile"))
	if model == "" {
		for _, item := range profiles {
			if item.Default {
				model = item.ID
				break
			}
		}
	}
	validModel := false
	for _, item := range profiles {
		if model == item.ID {
			validModel = true
			break
		}
	}
	if !validModel {
		s.logCutout("rejected model=%q elapsed=%s", model, time.Since(started).Round(time.Millisecond))
		writeError(w, http.StatusBadRequest, "本地目录中没有所选抠图模型")
		return
	}
	edge, ok := normalizeCutoutEdge(r.FormValue("edge"))
	if !ok {
		writeError(w, http.StatusBadRequest, "不支持的边缘处理方式")
		return
	}
	s.logCutout("inference start model=%s edge=%s input=%d", model, edge, len(input))
	if strings.HasPrefix(strings.ToLower(filepath.Base(s.rembgPath)), "rembg-engine") {
		output, err := s.persistentCutout(model, edge, input)
		if err != nil {
			message := err.Error()
			runes := []rune(message)
			if len(runes) > 500 {
				message = string(runes[:500])
			}
			s.logCutout("inference failed model=%s edge=%s elapsed=%s error=%q", model, edge, time.Since(started).Round(time.Millisecond), message)
			writeError(w, http.StatusInternalServerError, "rembg 处理失败: "+message)
			return
		}
		written, err := s.writeCutoutOutput(w, r, output, "")
		if err != nil || written != len(output) {
			s.logCutout("response interrupted model=%s output=%d written=%d elapsed=%s error=%v", model, len(output), written, time.Since(started).Round(time.Millisecond), err)
			return
		}
		s.logCutout("completed model=%s edge=%s output=%d elapsed=%s", model, edge, len(output), time.Since(started).Round(time.Millisecond))
		return
	}
	timeout := 3 * time.Minute
	if strings.Contains(strings.ToLower(model), "rmbg") || strings.Contains(strings.ToLower(model), "birefnet") {
		timeout = 8 * time.Minute
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	args := []string(nil)
	if strings.HasPrefix(strings.ToLower(filepath.Base(s.rembgPath)), "rembg-engine") {
		args = []string{"--model", model, "--edge", edge}
	} else {
		args = []string{"i", "-", "-"}
	}
	cmd := exec.CommandContext(ctx, s.rembgPath, args...)
	hideSubprocessWindow(cmd)
	if strings.HasPrefix(strings.ToLower(filepath.Base(s.rembgPath)), "rembg-engine") {
		cmd.Env = append(os.Environ(), "U2NET_HOME="+filepath.Join(filepath.Dir(s.rembgPath), "models"))
		cmd.Env = append(cmd.Env, "MODEL_CHECKSUM_DISABLED=1")
	}
	cmd.Stdin = bytes.NewReader(input)
	var output, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			writeError(w, http.StatusGatewayTimeout, "rembg 处理超时")
			return
		}
		message := decodeEngineError(stderr.Bytes())
		runes := []rune(message)
		if len(runes) > 500 {
			message = string(runes[:500])
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("rembg 处理失败: %s", message))
		return
	}
	if output.Len() == 0 {
		writeError(w, http.StatusInternalServerError, "rembg 未返回图片")
		return
	}
	if strings.Contains(stderr.String(), "KUAIMA_WARNING=fallback_low_memory") {
		w.Header().Set("X-Kuaima-Cutout-Warning", "fallback_low_memory")
	}
	warning := ""
	if strings.Contains(stderr.String(), "KUAIMA_WARNING=fallback_low_memory") {
		warning = "fallback_low_memory"
	}
	written, writeErr := s.writeCutoutOutput(w, r, output.Bytes(), warning)
	if writeErr != nil || written != output.Len() {
		s.logCutout("response interrupted model=%s output=%d written=%d elapsed=%s error=%v", model, output.Len(), written, time.Since(started).Round(time.Millisecond), writeErr)
		return
	}
	s.logCutout("completed model=%s edge=%s output=%d elapsed=%s", model, edge, output.Len(), time.Since(started).Round(time.Millisecond))
}
