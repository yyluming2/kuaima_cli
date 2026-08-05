package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
)

const maxRembgInputSize = 50 << 20

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
		writeError(w, http.StatusBadRequest, "本地目录中没有所选抠图模型")
		return
	}
	edge, ok := normalizeCutoutEdge(r.FormValue("edge"))
	if !ok {
		writeError(w, http.StatusBadRequest, "不支持的边缘处理方式")
		return
	}
	if strings.HasPrefix(strings.ToLower(filepath.Base(s.rembgPath)), "rembg-engine") {
		output, err := s.persistentCutout(model, edge, input)
		if err != nil {
			message := err.Error()
			runes := []rune(message)
			if len(runes) > 500 {
				message = string(runes[:500])
			}
			writeError(w, http.StatusInternalServerError, "rembg 处理失败: "+message)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(output)
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
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(output.Bytes())
}
