package app

import (
	"fmt"
	"net/http"
	"strings"
)

func (s *webUIServer) handleVideoGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := r.ParseMultipartForm(80 << 20); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	prompt := strings.TrimSpace(r.FormValue("prompt"))
	if prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}
	duration, err := formInt(r, "duration", 0)
	if err != nil || duration < 0 {
		writeError(w, http.StatusBadRequest, "invalid duration")
		return
	}
	c, err := s.clientOpts.newClient()
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer c.Close()
	refs, err := uploadedVideoImages(r, c)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	mode := strings.TrimSpace(r.FormValue("mode"))
	req := videoGenerationRequest{Model: strings.TrimSpace(r.FormValue("model")), Mode: mode, Prompt: prompt, Duration: duration, Size: strings.TrimSpace(r.FormValue("size"))}
	if mode == "frames" {
		if len(refs) == 0 {
			writeError(w, http.StatusBadRequest, "首帧图片不能为空")
			return
		}
		req.Image = refs[0]
		if len(refs) > 1 {
			req.LastFrame = refs[1]
		}
	} else {
		req.Images = refs
	}

	task, err := c.createVideoGeneration(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if strings.TrimSpace(task.TaskID) == "" {
		writeError(w, http.StatusBadGateway, "video API returned an empty task_id")
		return
	}
	writeJSON(w, http.StatusAccepted, task)
}

func uploadedVideoImages(r *http.Request, c *client) ([]string, error) {
	files := r.MultipartForm.File["images"]
	if len(files) > 16 {
		return nil, fmt.Errorf("too many video image inputs: got %d, expected at most 16", len(files))
	}
	result := make([]string, 0, len(files))
	for _, file := range files {
		ref, err := uploadedImageRef(r, c, fileFormatBase64, file)
		if err != nil {
			return nil, err
		}
		result = append(result, ref.ImageURL)
	}
	return result, nil
}

func (s *webUIServer) handleVideoStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	taskID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/video/status/"))
	if taskID == "" || strings.Contains(taskID, "/") {
		writeError(w, http.StatusBadRequest, "invalid task_id")
		return
	}
	c, err := s.clientOpts.newClient()
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer c.Close()
	task, err := c.getVideoGeneration(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, task)
}
