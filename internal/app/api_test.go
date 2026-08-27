package app

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestCreateResponseWritesLogFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			t.Fatalf("unexpected auth header: %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"id":"resp_1","output_text":"ok"}`))
	}))
	defer server.Close()

	logPath := filepath.Join(t.TempDir(), "kuaima.log")
	c, err := newClient(server.URL, "", "secret-token", "", "", false, logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	resp, err := c.createResponse(t.Context(), responseRequest{
		Model: "test-model",
		Input: []inputMessage{{Role: "user", Content: []inputContent{{Type: "input_text", Text: "hello"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.text() != "ok" {
		t.Fatalf("unexpected response text: %q", resp.text())
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(data)
	for _, want := range []string{"--- request ---", "POST " + server.URL + "/v1/responses", "Authorization: Bearer <redacted>", "--- response ---", "200 OK", `"output_text": "ok"`} {
		if !strings.Contains(log, want) {
			t.Fatalf("log missing %q:\n%s", want, log)
		}
	}
	prefixPattern := regexp.MustCompile(`(?m)^\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3}\] \[app\.\(\*client\)\.createResponse\] --- (request|response|timing) ---$`)
	if matches := prefixPattern.FindAllString(log, -1); len(matches) != 3 {
		t.Fatalf("expected request/response/timing prefixes, got %d:\n%s", len(matches), log)
	}
	if strings.Contains(log, "secret-token") {
		t.Fatalf("log should redact token:\n%s", log)
	}
}

func TestCreateResponseAddsRechargeHintForInsufficientQuota(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":{"message":"You exceeded your current quota","type":"insufficient_quota","code":"insufficient_quota"}}`))
	}))
	defer server.Close()

	c, err := newClient(server.URL, "", "secret-token", "", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	_, err = c.createResponse(t.Context(), responseRequest{Model: "test-model", Input: "hello"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), rechargeHint) {
		t.Fatalf("expected recharge hint, got: %v", err)
	}
}

func TestCreateResponseHTTPErrorAddsRechargeHintForBalanceMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":{"message":"余额不足"}}`))
	}))
	defer server.Close()

	c, err := newClient(server.URL, "", "secret-token", "", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	_, err = c.createResponse(t.Context(), responseRequest{Model: "test-model", Input: "hello"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), rechargeHint) {
		t.Fatalf("expected recharge hint, got: %v", err)
	}
}

func TestCreateResponseStreamAggregatesOutputItemEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.image_generation_call.partial_image\",\"output_index\":0,\"partial_image_b64\":\"partial-image\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"ig_1\",\"type\":\"image_generation_call\",\"status\":\"in_progress\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"ig_1\",\"type\":\"image_generation_call\",\"status\":\"completed\",\"result\":\"final-image\"}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	c, err := newClient(server.URL, "", "secret-token", "", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var out bytes.Buffer
	resp, err := c.createResponseStream(t.Context(), responseRequest{Model: "test-model", Input: "draw"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "" {
		t.Fatalf("unexpected stdout: %q", out.String())
	}
	if len(resp.Output) != 1 {
		t.Fatalf("expected 1 output item, got %d", len(resp.Output))
	}
	item := resp.Output[0]
	if item.ID != "ig_1" || item.Type != "image_generation_call" || item.Status != "completed" || item.Result != "final-image" {
		t.Fatalf("unexpected output item: %+v", item)
	}
	if !strings.Contains(string(resp.Raw), "response.image_generation_call.partial_image") {
		t.Fatalf("raw stream missing partial image event: %s", string(resp.Raw))
	}
}

func TestCreateResponseStreamAggregatesFunctionCallArgumentDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"name\":\"generate_video\",\"call_id\":\"call_1\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"delta\":\"{\\\"prompt\\\":\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"delta\":\"\\\"a cat\\\"}\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"output\":[{\"type\":\"function_call\",\"name\":\"generate_video\",\"call_id\":\"call_1\"}]}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	c, err := newClient(server.URL, "", "secret-token", "", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	resp, err := c.createResponseStream(t.Context(), responseRequest{Model: "test-model", Input: "video"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Output) != 1 || resp.Output[0].Name != "generate_video" || resp.Output[0].CallID != "call_1" || resp.Output[0].Arguments != `{"prompt":"a cat"}` {
		t.Fatalf("unexpected function call output: %#v", resp.Output)
	}
}

type responseStreamObserverBuffer struct {
	bytes.Buffer
	events []string
}

func (b *responseStreamObserverBuffer) ResponseStreamEvent(event string) {
	b.events = append(b.events, event)
}

func TestCreateResponseStreamNotifiesFunctionCallImmediately(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"delta\":\"{\"}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	c, err := newClient(server.URL, "", "secret-token", "", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	out := &responseStreamObserverBuffer{}
	if _, err := c.createResponseStream(t.Context(), responseRequest{Model: "test-model", Input: "video"}, out); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != 1 || out.events[0] != "function_call" {
		t.Fatalf("expected immediate function call notification, got %#v", out.events)
	}
}

func TestCreateResponseStreamAddsRechargeHintForErrorEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.failed\",\"error\":{\"message\":\"insufficient balance\"}}\n\n"))
	}))
	defer server.Close()

	c, err := newClient(server.URL, "", "secret-token", "", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	_, err = c.createResponseStream(t.Context(), responseRequest{Model: "test-model", Input: "hello"}, io.Discard)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), rechargeHint) {
		t.Fatalf("expected recharge hint, got: %v", err)
	}
}

func TestCreateResponseStreamEmitsImagesBeforeCompleted(t *testing.T) {
	imageSeen := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.image_generation_call.partial_image\",\"output_index\":0,\"partial_image_b64\":\"partial-image\"}\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		select {
		case <-imageSeen:
		case <-time.After(time.Second):
			t.Fatal("image callback did not run before completed event")
		}
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"output\":[]}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	c, err := newClient(server.URL, "", "secret-token", "", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var images []string
	_, err = c.createResponseStreamWithImages(t.Context(), responseRequest{Model: "test-model", Input: "draw"}, io.Discard, func(candidate imageCandidate) error {
		if len(images) == 0 {
			close(imageSeen)
		}
		images = append(images, candidate.Value)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 || images[0] != "partial-image" {
		t.Fatalf("unexpected streamed images: %#v", images)
	}
}

func TestCreateResponseStreamKeepsOutputItemsWhenCompletedResponseIsEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"ig_1\",\"type\":\"image_generation_call\",\"status\":\"in_progress\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.image_generation_call.partial_image\",\"output_index\":0,\"partial_image_b64\":\"partial-image\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"ig_1\",\"type\":\"image_generation_call\",\"status\":\"generating\",\"result\":\"final-image\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"output\":[]}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	c, err := newClient(server.URL, "", "secret-token", "", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	resp, err := c.createResponseStream(t.Context(), responseRequest{Model: "test-model", Input: "draw"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Output) != 1 {
		t.Fatalf("expected completed response to keep streamed output item, got %d", len(resp.Output))
	}
	if resp.Output[0].Result != "final-image" {
		t.Fatalf("unexpected image result: %+v", resp.Output[0])
	}
}

func TestCreateResponseStreamKeepsPartialImageWithoutDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"ig_1\",\"type\":\"image_generation_call\",\"status\":\"in_progress\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.image_generation_call.partial_image\",\"output_index\":0,\"partial_image_b64\":\"partial-image\"}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	c, err := newClient(server.URL, "", "secret-token", "", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	resp, err := c.createResponseStream(t.Context(), responseRequest{Model: "test-model", Input: "draw"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Output) != 1 {
		t.Fatalf("expected 1 output item, got %d", len(resp.Output))
	}
	if resp.Output[0].Result != "partial-image" {
		t.Fatalf("unexpected partial image result: %+v", resp.Output[0])
	}
}

func TestCreateImageGenerationStreamSendsStreamAndEmitsImages(t *testing.T) {
	imageSeen := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Fatalf("unexpected accept header: %q", r.Header.Get("Accept"))
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"stream":true`) {
			t.Fatalf("request body missing stream=true: %s", string(data))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"image_generation.partial_image\",\"partial_image_b64\":\"partial-image\"}\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		select {
		case <-imageSeen:
		case <-time.After(time.Second):
			t.Fatal("image callback did not run before completed event")
		}
		_, _ = w.Write([]byte("data: {\"type\":\"image_generation.completed\",\"b64_json\":\"final-image\"}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	c, err := newClient(server.URL, "", "secret-token", "", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var images []string
	resp, err := c.createImageGenerationStream(t.Context(), imageGenerationRequest{Model: "test-model", Prompt: "draw"}, func(candidate imageCandidate) error {
		if len(images) == 0 {
			close(imageSeen)
		}
		images = append(images, candidate.Value)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 2 || images[0] != "partial-image" || images[1] != "final-image" {
		t.Fatalf("unexpected streamed images: %#v", images)
	}
	if len(resp.Output) != 2 || resp.Output[1].Result != "final-image" {
		t.Fatalf("unexpected stream response: %+v", resp.Output)
	}
}

func TestCreateImageGenerationAddsRechargeHintForHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":{"message":"额度不足"}}`))
	}))
	defer server.Close()

	c, err := newClient(server.URL, "", "secret-token", "", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	_, err = c.createImageGeneration(t.Context(), imageGenerationRequest{Model: "test-model", Prompt: "draw"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), rechargeHint) {
		t.Fatalf("expected recharge hint, got: %v", err)
	}
}

func TestCreateImageEditStreamUsesEditEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/edits" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
			t.Fatalf("unexpected content type: %q", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Fatalf("unexpected accept header: %q", r.Header.Get("Accept"))
		}
		mr, err := r.MultipartReader()
		if err != nil {
			t.Fatal(err)
		}
		fields := map[string]string{}
		var imageBytes []byte
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(part)
			if err != nil {
				t.Fatal(err)
			}
			switch part.FormName() {
			case "model", "prompt", "stream":
				fields[part.FormName()] = string(data)
			case "image":
				imageBytes = data
			}
		}
		if fields["model"] != "test-model" {
			t.Fatalf("unexpected model field: %q", fields["model"])
		}
		if fields["prompt"] != "edit" {
			t.Fatalf("unexpected prompt field: %q", fields["prompt"])
		}
		if fields["stream"] != "true" {
			t.Fatalf("unexpected stream field: %q", fields["stream"])
		}
		if string(imageBytes) != "fake" {
			t.Fatalf("unexpected image bytes: %q", string(imageBytes))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"image_edit.completed\",\"data\":[{\"b64_json\":\"edited-image\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	c, err := newClient(server.URL, "", "secret-token", "", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	resp, err := c.createImageEditStream(t.Context(), imageEditRequest{
		Model:      "test-model",
		Prompt:     "edit",
		FileFormat: fileFormatBase64,
		Images:     []imageRef{{ImageURL: "data:image/png;base64,ZmFrZQ=="}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Output) != 1 || resp.Output[0].Result != "edited-image" {
		t.Fatalf("unexpected edit stream response: %+v", resp.Output)
	}
}
