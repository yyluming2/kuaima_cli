package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVideoGenerationAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/video/generations":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s", r.Method)
			}
			var req videoGenerationRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req.Model != "video-model" || req.Prompt != "pan right" || req.Image != "data:image/png;base64,AAA" || req.LastFrame != "data:image/png;base64,BBB" || req.Duration != 5 || req.Size != "1280x720" || req.Ratio != "16:9" {
				t.Fatalf("unexpected request: %+v", req)
			}
			_ = json.NewEncoder(w).Encode(videoGenerationTask{TaskID: "task-1", Status: "queued"})
		case "/v1/video/generations/task-1":
			if r.Method != http.MethodGet {
				t.Fatalf("method = %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(videoGenerationTask{TaskID: "task-1", Status: "completed", URL: "https://example.com/video.mp4", Format: "mp4"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c := &client{baseURL: server.URL, apiKey: "test", httpClient: server.Client()}
	task, err := c.createVideoGeneration(context.Background(), videoGenerationRequest{
		Model: "video-model", Prompt: "pan right", Image: "data:image/png;base64,AAA", LastFrame: "data:image/png;base64,BBB", Duration: 5, Size: "1280x720", Ratio: "16:9",
	})
	if err != nil || task.TaskID != "task-1" || task.Status != "queued" {
		t.Fatalf("create task = %+v, err = %v", task, err)
	}
	task, err = c.getVideoGeneration(context.Background(), task.TaskID)
	if err != nil || task.Status != "completed" || task.URL == "" {
		t.Fatalf("get task = %+v, err = %v", task, err)
	}
}

func TestVideoReferenceImagesJSON(t *testing.T) {
	data, err := json.Marshal(videoGenerationRequest{
		Model: "m", Prompt: "p", Images: []string{"one", "two"},
		Metadata: map[string]any{"audio": true, "watermark": false},
	})
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	if _, ok := value["images"]; !ok {
		t.Fatalf("images missing: %s", data)
	}
	if _, ok := value["image"]; ok {
		t.Fatalf("image should be omitted in reference mode: %s", data)
	}
	if _, ok := value["last_frame"]; ok {
		t.Fatalf("last_frame should be omitted in reference mode: %s", data)
	}
	metadata, ok := value["metadata"].(map[string]any)
	if !ok || metadata["audio"] != true || metadata["watermark"] != false {
		t.Fatalf("unexpected metadata: %s", data)
	}
}

func TestVideoEditJSON(t *testing.T) {
	data, err := json.Marshal(videoGenerationRequest{Model: "m", Mode: "edit", Prompt: "p", Video: "https://example.com/input.mp4", Images: []string{"reference"}})
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	if value["video"] != "https://example.com/input.mp4" {
		t.Fatalf("video missing: %s", data)
	}
	if images, ok := value["images"].([]any); !ok || len(images) != 1 || images[0] != "reference" {
		t.Fatalf("edit references missing: %s", data)
	}
}
