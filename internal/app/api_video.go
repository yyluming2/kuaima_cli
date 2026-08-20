package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (c *client) createVideoGeneration(ctx context.Context, req videoGenerationRequest) (*videoGenerationTask, error) {
	return c.videoRequest(ctx, http.MethodPost, "/v1/video/generations", req, "video generation")
}

func (c *client) getVideoGeneration(ctx context.Context, taskID string) (*videoGenerationTask, error) {
	return c.videoRequest(ctx, http.MethodGet, "/v1/videos/generations/"+url.PathEscape(strings.TrimSpace(taskID)), nil, "video generation status")
}

func (c *client) videoRequest(ctx context.Context, method, path string, payload any, operation string) (*videoGenerationTask, error) {
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Accept", "application/json")
	if payload != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	c.logRequest(httpReq, body)
	started := time.Now()
	var firstByte time.Time
	httpResp, err := c.httpClient.Do(withFirstByteTrace(httpReq, &firstByte))
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()
	data, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}
	c.logResponse(httpResp, data)
	c.logTiming(started, firstByte)
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, apiRequestError(operation, httpResp.Status, data)
	}
	var task videoGenerationTask
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, err
	}
	if task.Error != nil && strings.EqualFold(strings.TrimSpace(task.Status), "failed") {
		return &task, nil
	}
	return &task, nil
}
