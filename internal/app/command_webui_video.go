package app

import (
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
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
	if err != nil || duration < 4 || duration > 30 {
		writeError(w, http.StatusBadRequest, "时长必须在 4 到 30 秒之间")
		return
	}
	c, err := s.clientOpts.newClient()
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer c.Close()
	group := accountGroupFromClient(r.Context(), c)
	if group != "vip" && group != "svip" && !strings.EqualFold(strings.TrimSpace(r.FormValue("size")), "480p") {
		writeError(w, http.StatusForbidden, "普通用户只能生成 480p 视频")
		return
	}
	refs, err := uploadedVideoMedia(r, c, s.fileFormat, "references", 16)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	legacyRefs, err := uploadedVideoMedia(r, c, s.fileFormat, "images", 16-len(refs))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	refs = append(refs, legacyRefs...)
	mode := strings.TrimSpace(r.FormValue("mode"))
	req := videoGenerationRequest{
		Model: strings.TrimSpace(r.FormValue("model")), Mode: mode, Prompt: prompt, Duration: duration,
		Size:     strings.TrimSpace(r.FormValue("size")),
		Ratio:    strings.TrimSpace(r.FormValue("ratio")),
		Metadata: map[string]any{"audio": formBool(r.FormValue("audio"), true), "watermark": formBool(r.FormValue("watermark"), false)},
	}
	if mode == "frames" {
		first, firstErr := uploadedVideoMedia(r, c, s.fileFormat, "first_frame", 1)
		last, lastErr := uploadedVideoMedia(r, c, s.fileFormat, "last_frame", 1)
		if firstErr != nil || lastErr != nil {
			writeError(w, http.StatusBadRequest, firstError(firstErr, lastErr).Error())
			return
		}
		// Keep accepting the old positional images field for API compatibility.
		if len(first) == 0 && len(last) == 0 && len(refs) > 0 {
			first = refs[:1]
			if len(refs) > 1 {
				last = refs[1:2]
			}
		}
		if len(first) == 0 && len(last) == 0 {
			writeError(w, http.StatusBadRequest, "首帧或尾帧至少上传一张")
			return
		}
		if len(first) > 0 {
			req.Image = first[0]
		}
		if len(last) > 0 {
			req.LastFrame = last[0]
		}
	} else if mode == "edit" {
		videos, videoErr := uploadedVideoMedia(r, c, s.fileFormat, "video", 1)
		if videoErr != nil {
			writeError(w, http.StatusBadRequest, videoErr.Error())
			return
		}
		if len(videos) == 0 {
			writeError(w, http.StatusBadRequest, "编辑模式必须上传视频")
			return
		}
		req.Video = videos[0]
		req.Images = refs
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

func (s *webUIServer) handleVideoDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	taskID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/video/download/"))
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
	if err != nil || strings.TrimSpace(task.URL) == "" {
		if err == nil {
			err = fmt.Errorf("video is not ready")
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	videoURL, err := url.Parse(task.URL)
	if err != nil || (videoURL.Scheme != "http" && videoURL.Scheme != "https") {
		writeError(w, http.StatusBadGateway, "video API returned an invalid URL")
		return
	}
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, videoURL.String(), nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		writeError(w, http.StatusBadGateway, "download video failed: "+response.Status)
		return
	}
	ext := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(task.Format)), ".")
	if ext != "mp4" && ext != "webm" && ext != "mov" {
		ext = "mp4"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": "video-" + taskID + "." + ext}))
	if contentType := response.Header.Get("Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if response.ContentLength >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(response.ContentLength, 10))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, response.Body)
}

func uploadedVideoMedia(r *http.Request, c *client, fileFormat, field string, limit int) ([]string, error) {
	files := r.MultipartForm.File[field]
	if len(files) > limit {
		return nil, fmt.Errorf("too many %s inputs: got %d, expected at most %d", field, len(files), limit)
	}
	result := make([]string, 0, len(files))
	for _, header := range files {
		file, err := header.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(file)
		closeErr := file.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		mediaType := strings.TrimSpace(strings.Split(header.Header.Get("Content-Type"), ";")[0])
		if mediaType == "" {
			mediaType = mime.TypeByExtension(filepath.Ext(header.Filename))
		}
		if mediaType == "" {
			mediaType = http.DetectContentType(data)
		}
		value := "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)
		if strings.EqualFold(strings.TrimSpace(fileFormat), fileFormatURL) {
			if uploaded, uploadErr := c.uploadLocalData(r.Context(), data, header.Filename); uploadErr == nil && strings.TrimSpace(uploaded) != "" {
				value = uploaded
			}
		}
		result = append(result, value)
	}
	return result, nil
}

func formBool(value string, fallback bool) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func firstError(errors ...error) error {
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
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
