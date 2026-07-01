package app

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strings"
	"testing"
)

func webUITestUsageHandler(group string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/usage/token/2" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":true,"data":{"group":"` + group + `","subscriptions":[],"total_available":0,"total_used":0,"total_granted":0,"unlimited_quota":false},"message":"ok"}`))
			return
		}
		next(w, r)
	}
}

func TestWebUIHandleRechargeExchange(t *testing.T) {
	var got exchangeTokenRequest
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/usage/token/exchange" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			t.Fatalf("unexpected auth header: %q", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"message":""}`))
	}))
	defer apiServer.Close()

	ui := &webUIServer{
		clientOpts: clientOptions{
			model:      stringPtr("gpt-5.4-mini"),
			imageModel: stringPtr("test-model"),
			baseURL:    stringPtr(apiServer.URL),
			ossURL:     stringPtr(""),
			apiKey:     stringPtr("secret-token"),
			username:   stringPtr(""),
			password:   stringPtr(""),
			verbose:    boolPtr(false),
			logFile:    stringPtr(""),
		},
		saveDir: t.TempDir(),
	}
	req := httptest.NewRequest(http.MethodPost, "/api/recharge/exchange", strings.NewReader(`{"key":" code-2 "}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	ui.handleRechargeExchange(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	if got.Key != "code-2" {
		t.Fatalf("unexpected exchange key: %#v", got)
	}
}

func TestWebUIHandleGenerateRepeatsImageRequestsForCount(t *testing.T) {
	var callCount int
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/usage/token/2" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":true,"data":{"group":"vip","subscriptions":[],"total_available":0,"total_used":0,"total_granted":0,"unlimited_quota":false},"message":"ok"}`))
			return
		}
		callCount++
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var got imageGenerationRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.N != 1 {
			t.Fatalf("expected n=1, got %d", got.N)
		}
		w.Header().Set("Content-Type", "application/json")
		switch callCount {
		case 1:
			_, _ = w.Write([]byte(`{"id":"resp_1","output":[{"type":"image_generation_call","status":"completed","result":"ZmFrZTE="}],"output_text":""}`))
		case 2:
			_, _ = w.Write([]byte(`{"id":"resp_2","output":[{"type":"image_generation_call","status":"completed","result":"ZmFrZTI="}],"output_text":""}`))
		default:
			t.Fatalf("unexpected call count: %d", callCount)
		}
	}))
	defer apiServer.Close()

	saveDir := t.TempDir()
	ui := &webUIServer{
		clientOpts: clientOptions{
			model:      stringPtr("gpt-5.4-mini"),
			imageModel: stringPtr("test-model"),
			baseURL:    stringPtr(apiServer.URL),
			ossURL:     stringPtr(""),
			apiKey:     stringPtr("secret-token"),
			username:   stringPtr(""),
			password:   stringPtr(""),
			verbose:    boolPtr(false),
			logFile:    stringPtr(""),
		},
		fileFormat: fileFormatBase64,
		defaults: webUIDefaults{
			ImageModel:   "test-model",
			ImageSize:    "1024x1024",
			ImageQuality: "auto",
			ImageCount:   2,
		},
		saveDir: saveDir,
	}

	body := &strings.Builder{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("prompt", "generate me")
	_ = writer.WriteField("image_model", "test-model")
	_ = writer.WriteField("image_count", "2")
	_ = writer.WriteField("image_endpoint", "image")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/generate", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()

	ui.handleGenerate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	if callCount != 2 {
		t.Fatalf("expected 2 requests, got %d", callCount)
	}
	if entries, err := os.ReadDir(saveDir); err != nil || len(entries) != 2 {
		t.Fatalf("expected 2 saved images in %s, entries=%v err=%v", saveDir, entries, err)
	}
}

func TestWebUIHandleGenerateRejectsCountForDefaultGroup(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/usage/token/2":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":true,"data":{"group":"default","subscriptions":[],"total_available":0,"total_used":0,"total_granted":0,"unlimited_quota":false},"message":"ok"}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	ui := &webUIServer{
		clientOpts: clientOptions{
			model:      stringPtr("gpt-5.4-mini"),
			imageModel: stringPtr("test-model"),
			baseURL:    stringPtr(apiServer.URL),
			ossURL:     stringPtr(""),
			apiKey:     stringPtr("secret-token"),
			username:   stringPtr(""),
			password:   stringPtr(""),
			verbose:    boolPtr(false),
			logFile:    stringPtr(""),
		},
		fileFormat: fileFormatBase64,
		defaults: webUIDefaults{
			ImageModel:   "test-model",
			ImageSize:    "1024x1024",
			ImageQuality: "auto",
			ImageCount:   1,
		},
		saveDir: t.TempDir(),
	}

	body := &strings.Builder{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("prompt", "generate me")
	_ = writer.WriteField("image_model", "test-model")
	_ = writer.WriteField("image_count", "2")
	_ = writer.WriteField("image_endpoint", "image")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/generate", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()

	ui.handleGenerate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "普通用户一次只能生成 1 张") {
		t.Fatalf("unexpected body: %s", rr.Body.String())
	}
}

func TestWebUIHandleGenerateAllowsVipCount(t *testing.T) {
	var callCount int
	apiServer := httptest.NewServer(webUITestUsageHandler("vip", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/images/generations":
			callCount++
			var got imageGenerationRequest
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.N != 1 {
				t.Fatalf("expected n=1, got %d", got.N)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"resp_1","output":[{"type":"image_generation_call","status":"completed","result":"ZmFrZQ=="}],"output_text":""}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	})))
	defer apiServer.Close()

	saveDir := t.TempDir()
	ui := &webUIServer{
		clientOpts: clientOptions{
			model:      stringPtr("gpt-5.4-mini"),
			imageModel: stringPtr("test-model"),
			baseURL:    stringPtr(apiServer.URL),
			ossURL:     stringPtr(""),
			apiKey:     stringPtr("secret-token"),
			username:   stringPtr(""),
			password:   stringPtr(""),
			verbose:    boolPtr(false),
			logFile:    stringPtr(""),
		},
		fileFormat: fileFormatBase64,
		defaults: webUIDefaults{
			ImageModel:   "test-model",
			ImageSize:    "1024x1024",
			ImageQuality: "auto",
			ImageCount:   3,
		},
		saveDir: saveDir,
	}

	body := &strings.Builder{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("prompt", "generate me")
	_ = writer.WriteField("image_model", "test-model")
	_ = writer.WriteField("image_count", "3")
	_ = writer.WriteField("image_endpoint", "image")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/generate", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()

	ui.handleGenerate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	if callCount != 3 {
		t.Fatalf("expected 3 requests, got %d", callCount)
	}
}

func TestWebUIHandleGenerateStreamRepeatsImageRequestsForCount(t *testing.T) {
	var callCount int
	apiServer := httptest.NewServer(webUITestUsageHandler("vip", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var got imageGenerationRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.N != 1 {
			t.Fatalf("expected n=1, got %d", got.N)
		}
		w.Header().Set("Content-Type", "application/json")
		switch callCount {
		case 1:
			_, _ = w.Write([]byte(`{"id":"resp_1","output":[{"type":"image_generation_call","status":"completed","result":"ZmFrZTE="}],"output_text":""}`))
		case 2:
			_, _ = w.Write([]byte(`{"id":"resp_2","output":[{"type":"image_generation_call","status":"completed","result":"ZmFrZTI="}],"output_text":""}`))
		default:
			t.Fatalf("unexpected call count: %d", callCount)
		}
	})))
	defer apiServer.Close()

	saveDir := t.TempDir()
	ui := &webUIServer{
		clientOpts: clientOptions{
			model:      stringPtr("gpt-5.4-mini"),
			imageModel: stringPtr("test-model"),
			baseURL:    stringPtr(apiServer.URL),
			ossURL:     stringPtr(""),
			apiKey:     stringPtr("secret-token"),
			username:   stringPtr(""),
			password:   stringPtr(""),
			verbose:    boolPtr(false),
			logFile:    stringPtr(""),
		},
		fileFormat: fileFormatBase64,
		defaults: webUIDefaults{
			ImageModel:   "test-model",
			ImageSize:    "1024x1024",
			ImageQuality: "auto",
			ImageCount:   2,
		},
		saveDir: saveDir,
	}

	body := &strings.Builder{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("prompt", "generate me")
	_ = writer.WriteField("image_model", "test-model")
	_ = writer.WriteField("image_count", "2")
	_ = writer.WriteField("image_endpoint", "image")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/generate/stream", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()

	ui.handleGenerateStream(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	if callCount != 2 {
		t.Fatalf("expected 2 requests, got %d", callCount)
	}
	if entries, err := os.ReadDir(saveDir); err != nil || len(entries) != 2 {
		t.Fatalf("expected 2 saved images in %s, entries=%v err=%v", saveDir, entries, err)
	}
}

func TestWebUIHandleGenerateWithResponsesConcurrentUsesStreamWhenStreaming(t *testing.T) {
	var callCount int
	apiServer := httptest.NewServer(webUITestUsageHandler("vip", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/responses":
			callCount++
			if r.Header.Get("Accept") != "text/event-stream" {
				t.Fatalf("expected streamed accept header, got %q", r.Header.Get("Accept"))
			}
			var got responseRequest
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if !got.Stream {
				t.Fatal("expected stream=true in request")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(
				"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
					"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"output_text\":\"ok\",\"output\":[]}}\n\n",
			))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	})))
	defer apiServer.Close()

	ui := &webUIServer{
		clientOpts: clientOptions{
			model:      stringPtr("gpt-5.4-mini"),
			imageModel: stringPtr("test-model"),
			baseURL:    stringPtr(apiServer.URL),
			ossURL:     stringPtr(""),
			apiKey:     stringPtr("secret-token"),
			username:   stringPtr(""),
			password:   stringPtr(""),
			verbose:    boolPtr(false),
			logFile:    stringPtr(""),
		},
		fileFormat: fileFormatBase64,
	}

	c, err := newClient(apiServer.URL, "", "secret-token", "", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	resp, err := ui.handleGenerateWithResponsesConcurrent(
		t.Context(),
		c,
		responseRequest{Model: "test-model", Input: "draw", Stream: true},
		2,
		&webUIEventStream{w: httptest.NewRecorder()},
		nil,
		2,
	)
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil || resp.OutputText != "ok\n\nok" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 streamed requests, got %d", callCount)
	}
}

func TestWebUIHandleGenerateUsesMultipartForImageEdits(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/usage/token/2" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":true,"data":{"group":"vip","subscriptions":[],"total_available":0,"total_used":0,"total_granted":0,"unlimited_quota":false},"message":"ok"}`))
			return
		}
		if r.URL.Path != "/v1/images/edits" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
			t.Fatalf("unexpected content type: %q", r.Header.Get("Content-Type"))
		}
		mr, err := r.MultipartReader()
		if err != nil {
			t.Fatal(err)
		}
		fields := map[string]string{}
		var imageData []byte
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
				imageData = data
			}
		}
		if fields["model"] != "test-model" {
			t.Fatalf("unexpected model: %q", fields["model"])
		}
		if fields["prompt"] != "edit me" {
			t.Fatalf("unexpected prompt: %q", fields["prompt"])
		}
		if fields["stream"] != "" {
			t.Fatalf("unexpected stream field: %q", fields["stream"])
		}
		if string(imageData) != "fake" {
			t.Fatalf("unexpected image data: %q", string(imageData))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","output":[{"type":"image_generation_call","status":"completed","result":"ZmFrZQ=="}],"output_text":""}`))
	}))
	defer apiServer.Close()

	saveDir := t.TempDir()
	ui := &webUIServer{
		clientOpts: clientOptions{
			model:      stringPtr("gpt-5.4-mini"),
			imageModel: stringPtr("test-model"),
			baseURL:    stringPtr(apiServer.URL),
			ossURL:     stringPtr(""),
			apiKey:     stringPtr("secret-token"),
			username:   stringPtr(""),
			password:   stringPtr(""),
			verbose:    boolPtr(false),
			logFile:    stringPtr(""),
		},
		fileFormat: fileFormatBase64,
		defaults: webUIDefaults{
			ImageModel:   "test-model",
			ImageSize:    "1024x1024",
			ImageQuality: "auto",
			ImageCount:   1,
		},
		saveDir: saveDir,
	}

	body := &strings.Builder{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("prompt", "edit me")
	_ = writer.WriteField("image_model", "test-model")
	_ = writer.WriteField("image_endpoint", "image")
	part, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="images"; filename="ref.png"`},
		"Content-Type":        {"image/png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("fake"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/generate", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()

	ui.handleGenerate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "saved") {
		t.Fatalf("unexpected response body: %s", rr.Body.String())
	}
	if entries, err := os.ReadDir(saveDir); err != nil || len(entries) == 0 {
		t.Fatalf("expected saved image in %s, entries=%v err=%v", saveDir, entries, err)
	}
}

func TestWebUIHandleGeneratePassesMaskForResponses(t *testing.T) {
	var gotRequest struct {
		Tools []map[string]any `json:"tools"`
		Input []inputMessage   `json:"input"`
	}
	apiServer := httptest.NewServer(webUITestUsageHandler("vip", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","output_text":"ok","output":[]}`))
	})))
	defer apiServer.Close()

	saveDir := t.TempDir()
	ui := &webUIServer{
		clientOpts: clientOptions{
			model:      stringPtr("gpt-5.4-mini"),
			imageModel: stringPtr("test-model"),
			baseURL:    stringPtr(apiServer.URL),
			ossURL:     stringPtr(""),
			apiKey:     stringPtr("secret-token"),
			username:   stringPtr(""),
			password:   stringPtr(""),
			verbose:    boolPtr(false),
			logFile:    stringPtr(""),
		},
		fileFormat: fileFormatBase64,
		defaults: webUIDefaults{
			ImageModel:   "test-model",
			ImageSize:    "1024x1024",
			ImageQuality: "auto",
			ImageCount:   1,
		},
		saveDir: saveDir,
	}

	body := &strings.Builder{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("prompt", "edit me")
	_ = writer.WriteField("image_model", "test-model")
	_ = writer.WriteField("image_endpoint", "response")
	imagePart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="images"; filename="ref.png"`},
		"Content-Type":        {"image/png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = imagePart.Write([]byte("fake"))
	maskPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="mask"; filename="mask.png"`},
		"Content-Type":        {"image/png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = maskPart.Write([]byte("mask"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/generate", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()

	ui.handleGenerate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	if len(gotRequest.Tools) != 1 {
		t.Fatalf("unexpected tools: %#v", gotRequest.Tools)
	}
	maskValue, ok := gotRequest.Tools[0]["input_image_mask"].(map[string]any)
	if !ok {
		t.Fatalf("missing input_image_mask: %#v", gotRequest.Tools[0])
	}
	if got := maskValue["image_url"].(string); !strings.Contains(got, "bWFzaw==") {
		t.Fatalf("unexpected mask image URL: %q", got)
	}
	if len(gotRequest.Input) == 0 || len(gotRequest.Input[len(gotRequest.Input)-1].Content) < 2 {
		t.Fatalf("expected prompt and reference image input, got %#v", gotRequest.Input)
	}
}

func TestWebUIHandleGenerateRepeatsImageEditRequestsForCount(t *testing.T) {
	var callCount int
	apiServer := httptest.NewServer(webUITestUsageHandler("vip", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.URL.Path != "/v1/images/edits" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
			t.Fatalf("unexpected content type: %q", r.Header.Get("Content-Type"))
		}
		mr, err := r.MultipartReader()
		if err != nil {
			t.Fatal(err)
		}
		fields := map[string]string{}
		var imageData []byte
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
			case "model", "prompt", "stream", "n":
				fields[part.FormName()] = string(data)
			case "image":
				imageData = data
			}
		}
		if fields["n"] != "1" {
			t.Fatalf("expected n=1, got %q", fields["n"])
		}
		if string(imageData) != "fake" {
			t.Fatalf("unexpected image data: %q", string(imageData))
		}
		w.Header().Set("Content-Type", "application/json")
		switch callCount {
		case 1:
			_, _ = w.Write([]byte(`{"id":"resp_1","output":[{"type":"image_generation_call","status":"completed","result":"ZmFrZTE="}],"output_text":""}`))
		case 2:
			_, _ = w.Write([]byte(`{"id":"resp_2","output":[{"type":"image_generation_call","status":"completed","result":"ZmFrZTI="}],"output_text":""}`))
		default:
			t.Fatalf("unexpected call count: %d", callCount)
		}
	})))
	defer apiServer.Close()

	saveDir := t.TempDir()
	ui := &webUIServer{
		clientOpts: clientOptions{
			model:      stringPtr("gpt-5.4-mini"),
			imageModel: stringPtr("test-model"),
			baseURL:    stringPtr(apiServer.URL),
			ossURL:     stringPtr(""),
			apiKey:     stringPtr("secret-token"),
			username:   stringPtr(""),
			password:   stringPtr(""),
			verbose:    boolPtr(false),
			logFile:    stringPtr(""),
		},
		fileFormat: fileFormatBase64,
		defaults: webUIDefaults{
			ImageModel:   "test-model",
			ImageSize:    "1024x1024",
			ImageQuality: "auto",
			ImageCount:   2,
		},
		saveDir: saveDir,
	}

	body := &strings.Builder{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("prompt", "edit me")
	_ = writer.WriteField("image_model", "test-model")
	_ = writer.WriteField("image_count", "2")
	_ = writer.WriteField("image_endpoint", "image")
	part, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="images"; filename="ref.png"`},
		"Content-Type":        {"image/png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("fake"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/generate", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()

	ui.handleGenerate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	if callCount != 2 {
		t.Fatalf("expected 2 requests, got %d", callCount)
	}
	if entries, err := os.ReadDir(saveDir); err != nil || len(entries) != 2 {
		t.Fatalf("expected 2 saved images in %s, entries=%v err=%v", saveDir, entries, err)
	}
}

func TestWebUIHandleGenerateUsesOSSURLForUploadedImagesWhenFileFormatURL(t *testing.T) {
	var ossServer *httptest.Server
	ossServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/go/oss/prepare_upload_local/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"errno":0,"file":{"file_url":"` + ossServer.URL + `/uploaded.png"}}`))
		case "/uploaded.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(tinyPNG())
		default:
			t.Fatalf("unexpected OSS path: %s", r.URL.Path)
		}
	}))
	defer ossServer.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/usage/token/2" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":true,"data":{"group":"vip","subscriptions":[],"total_available":0,"total_used":0,"total_granted":0,"unlimited_quota":false},"message":"ok"}`))
			return
		}
		if r.URL.Path != "/v1/images/edits" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
			t.Fatalf("unexpected content type: %q", r.Header.Get("Content-Type"))
		}
		mr, err := r.MultipartReader()
		if err != nil {
			t.Fatal(err)
		}
		fields := map[string]string{}
		var imageData []byte
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
			case "model", "prompt", "size", "quality", "background", "moderation":
				fields[part.FormName()] = string(data)
			case "image":
				imageData = data
			}
		}
		if fields["model"] != "test-model" {
			t.Fatalf("unexpected model: %q", fields["model"])
		}
		if fields["prompt"] != "edit me" {
			t.Fatalf("unexpected prompt: %q", fields["prompt"])
		}
		if len(imageData) == 0 {
			t.Fatal("expected uploaded image data")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","output":[{"type":"image_generation_call","status":"completed","result":"ZmFrZQ=="}],"output_text":""}`))
	}))
	defer apiServer.Close()

	saveDir := t.TempDir()
	ui := &webUIServer{
		clientOpts: clientOptions{
			model:      stringPtr("gpt-5.4-mini"),
			imageModel: stringPtr("test-model"),
			baseURL:    stringPtr(apiServer.URL),
			ossURL:     stringPtr(ossServer.URL),
			apiKey:     stringPtr("secret-token"),
			username:   stringPtr(""),
			password:   stringPtr(""),
			verbose:    boolPtr(false),
			logFile:    stringPtr(""),
		},
		fileFormat: fileFormatURL,
		defaults: webUIDefaults{
			ImageModel:   "test-model",
			ImageSize:    "1024x1024",
			ImageQuality: "auto",
			ImageCount:   1,
		},
		saveDir: saveDir,
	}

	body := &strings.Builder{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("prompt", "edit me")
	_ = writer.WriteField("image_model", "test-model")
	_ = writer.WriteField("image_endpoint", "image")
	part, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="images"; filename="ref.png"`},
		"Content-Type":        {"image/png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("fake"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/generate", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()

	ui.handleGenerate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	if entries, err := os.ReadDir(saveDir); err != nil || len(entries) != 1 {
		t.Fatalf("expected 1 saved image in %s, entries=%v err=%v", saveDir, entries, err)
	}
}

func TestWebUIHandleGenerateFallsBackToBase64WhenUploadFails(t *testing.T) {
	ossServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/go/oss/prepare_upload_local/" {
			t.Fatalf("unexpected OSS path: %s", r.URL.Path)
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ossServer.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/usage/token/2" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":true,"data":{"group":"vip","subscriptions":[],"total_available":0,"total_used":0,"total_granted":0,"unlimited_quota":false},"message":"ok"}`))
			return
		}
		if r.URL.Path != "/v1/images/edits" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
			t.Fatalf("unexpected content type: %q", r.Header.Get("Content-Type"))
		}
		mr, err := r.MultipartReader()
		if err != nil {
			t.Fatal(err)
		}
		fields := map[string]string{}
		var imageData []byte
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
			case "model", "prompt":
				fields[part.FormName()] = string(data)
			case "image":
				imageData = data
			}
		}
		if fields["model"] != "test-model" {
			t.Fatalf("unexpected model: %q", fields["model"])
		}
		if fields["prompt"] != "edit me" {
			t.Fatalf("unexpected prompt: %q", fields["prompt"])
		}
		if string(imageData) != "fake" {
			t.Fatalf("unexpected image data: %q", string(imageData))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","output":[{"type":"image_generation_call","status":"completed","result":"ZmFrZQ=="}],"output_text":""}`))
	}))
	defer apiServer.Close()

	saveDir := t.TempDir()
	ui := &webUIServer{
		clientOpts: clientOptions{
			model:      stringPtr("gpt-5.4-mini"),
			imageModel: stringPtr("test-model"),
			baseURL:    stringPtr(apiServer.URL),
			ossURL:     stringPtr(ossServer.URL),
			apiKey:     stringPtr("secret-token"),
			username:   stringPtr(""),
			password:   stringPtr(""),
			verbose:    boolPtr(false),
			logFile:    stringPtr(""),
		},
		fileFormat: fileFormatURL,
		defaults: webUIDefaults{
			ImageModel:   "test-model",
			ImageSize:    "1024x1024",
			ImageQuality: "auto",
			ImageCount:   1,
		},
		saveDir: saveDir,
	}

	body := &strings.Builder{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("prompt", "edit me")
	_ = writer.WriteField("image_model", "test-model")
	_ = writer.WriteField("image_endpoint", "image")
	part, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="images"; filename="ref.png"`},
		"Content-Type":        {"image/png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("fake"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/generate", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()

	ui.handleGenerate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	if entries, err := os.ReadDir(saveDir); err != nil || len(entries) != 1 {
		t.Fatalf("expected 1 saved image in %s, entries=%v err=%v", saveDir, entries, err)
	}
}

func TestWebUIHandleAgentStreamNormalizesAssistantContent(t *testing.T) {
	var gotRequest struct {
		Model  string           `json:"model"`
		Input  []inputMessage   `json:"input"`
		Tools  []map[string]any `json:"tools"`
		Stream bool             `json:"stream"`
	}
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"output_text\":\"ok\",\"output\":[]}}\n\n",
		))
	}))
	defer apiServer.Close()

	ui := &webUIServer{
		clientOpts: clientOptions{
			model:      stringPtr("gpt-5.4-mini"),
			imageModel: stringPtr("test-model"),
			baseURL:    stringPtr(apiServer.URL),
			ossURL:     stringPtr(""),
			apiKey:     stringPtr("secret-token"),
			username:   stringPtr(""),
			password:   stringPtr(""),
			verbose:    boolPtr(false),
			logFile:    stringPtr(""),
		},
		fileFormat: fileFormatBase64,
	}

	body := `{"messages":[{"role":"user","content":[{"type":"input_text","text":"hello"}]},{"role":"assistant","content":[{"type":"input_text","text":"old reply"}]},{"role":"user","content":[{"type":"input_text","text":"again"}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/agent/stream", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	ui.handleAgentStream(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	if !gotRequest.Stream {
		t.Fatal("expected stream=true")
	}
	if len(gotRequest.Input) != 4 {
		t.Fatalf("unexpected input: %#v", gotRequest.Input)
	}
	if gotRequest.Input[0].Role != "system" || !strings.Contains(gotRequest.Input[0].Content[0].Text, "image_generation") {
		t.Fatalf("expected agent system prompt, got %#v", gotRequest.Input[0])
	}
	if got := gotRequest.Input[2].Content[0].Type; got != "output_text" {
		t.Fatalf("expected assistant output_text, got %q", got)
	}
	if len(gotRequest.Tools) != 1 || gotRequest.Tools[0]["type"] != "image_generation" {
		t.Fatalf("expected image generation tool, got %#v", gotRequest.Tools)
	}
}

func TestWebUIHandleAgentStreamSavesGeneratedImages(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(tinyPNG())
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var gotRequest struct {
			Tools []map[string]any `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatal(err)
		}
		if len(gotRequest.Tools) != 1 || gotRequest.Tools[0]["type"] != "image_generation" {
			t.Fatalf("expected image generation tool, got %#v", gotRequest.Tools)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"ig_1\",\"type\":\"image_generation_call\",\"status\":\"completed\",\"result\":\"" + encoded + "\"}}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"output_text\":\"done\",\"output\":[{\"id\":\"ig_1\",\"type\":\"image_generation_call\",\"status\":\"completed\",\"result\":\"" + encoded + "\"}]}}\n\n",
		))
	}))
	defer apiServer.Close()

	saveDir := t.TempDir()
	ui := &webUIServer{
		clientOpts: clientOptions{
			model:      stringPtr("gpt-5.4-mini"),
			imageModel: stringPtr("test-model"),
			baseURL:    stringPtr(apiServer.URL),
			ossURL:     stringPtr(""),
			apiKey:     stringPtr("secret-token"),
			username:   stringPtr(""),
			password:   stringPtr(""),
			verbose:    boolPtr(false),
			logFile:    stringPtr(""),
		},
		fileFormat: fileFormatBase64,
		defaults: webUIDefaults{
			ImageSize:    "1024x1024",
			ImageQuality: "auto",
			ImageUpscale: false,
		},
		saveDir: saveDir,
	}

	body := `{"messages":[{"role":"user","content":[{"type":"input_text","text":"画一张图"}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/agent/stream", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	ui.handleAgentStream(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Body.String(); !strings.Contains(got, "event: image") || !strings.Contains(got, "/outputs/response-image-01.png") {
		t.Fatalf("expected image event, got %s", got)
	}
	if entries, err := os.ReadDir(saveDir); err != nil || len(entries) != 1 {
		t.Fatalf("expected 1 saved image in %s, entries=%v err=%v", saveDir, entries, err)
	}
}

func TestWebUIHandleAgentUploadUsesOSSURLWhenFileFormatURL(t *testing.T) {
	var ossServer *httptest.Server
	ossServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/go/oss/prepare_upload_local/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"errno":0,"file":{"file_url":"` + ossServer.URL + `/agent-uploaded.png"}}`))
		default:
			t.Fatalf("unexpected OSS path: %s", r.URL.Path)
		}
	}))
	defer ossServer.Close()

	ui := &webUIServer{
		clientOpts: clientOptions{
			model:      stringPtr("gpt-5.4-mini"),
			imageModel: stringPtr("test-model"),
			baseURL:    stringPtr("http://example.test"),
			ossURL:     stringPtr(ossServer.URL),
			apiKey:     stringPtr("secret-token"),
			username:   stringPtr(""),
			password:   stringPtr(""),
			verbose:    boolPtr(false),
			logFile:    stringPtr(""),
		},
		fileFormat: fileFormatURL,
		saveDir:    t.TempDir(),
	}

	body := &strings.Builder{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="images"; filename="ref.png"`},
		"Content-Type":        {"image/png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(tinyPNG())
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/agent/upload", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()

	ui.handleAgentUpload(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Body.String(); !strings.Contains(got, ossServer.URL+"/agent-uploaded.png") || strings.Contains(got, "data:image/") {
		t.Fatalf("unexpected response body: %s", got)
	}
}

func TestWebUIHandleGenerateResponseUsesCLISystem(t *testing.T) {
	var gotRequest struct {
		Model  string           `json:"model"`
		Input  []inputMessage   `json:"input"`
		Tools  []map[string]any `json:"tools"`
		Stream bool             `json:"stream"`
	}
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/usage/token/2" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":true,"data":{"group":"vip","subscriptions":[],"total_available":0,"total_used":0,"total_granted":0,"unlimited_quota":false},"message":"ok"}`))
			return
		}
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatal(err)
		}
		if gotRequest.Model != "gpt-5.4-mini" {
			t.Fatalf("unexpected model: %q", gotRequest.Model)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","output_text":"ok","output":[]}`))
	}))
	defer apiServer.Close()

	saveDir := t.TempDir()
	ui := &webUIServer{
		clientOpts: clientOptions{
			model:      stringPtr("gpt-5.4-mini"),
			imageModel: stringPtr("test-model"),
			baseURL:    stringPtr(apiServer.URL),
			ossURL:     stringPtr(""),
			apiKey:     stringPtr("secret-token"),
			username:   stringPtr(""),
			password:   stringPtr(""),
			verbose:    boolPtr(false),
			logFile:    stringPtr(""),
		},
		responseSystem: "be concise",
		fileFormat:     fileFormatBase64,
		defaults: webUIDefaults{
			ImageModel:   "test-model",
			ImageSize:    "1024x1024",
			ImageQuality: "auto",
			ImageCount:   1,
		},
		saveDir: saveDir,
	}

	body := &strings.Builder{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("prompt", "hello")
	_ = writer.WriteField("image_model", "test-model")
	_ = writer.WriteField("image_endpoint", "response")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/generate", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()

	ui.handleGenerate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	if len(gotRequest.Input) < 2 {
		t.Fatalf("unexpected input messages: %#v", gotRequest.Input)
	}
	if gotRequest.Input[0].Role != "system" {
		t.Fatalf("expected system role, got: %q", gotRequest.Input[0].Role)
	}
	if len(gotRequest.Input[0].Content) != 1 || gotRequest.Input[0].Content[0].Text != "be concise" {
		t.Fatalf("unexpected system content: %#v", gotRequest.Input[0].Content)
	}
}

func TestWebUIHandleAutoPromptUsesResponses(t *testing.T) {
	var gotRequest struct {
		Model  string         `json:"model"`
		Input  []inputMessage `json:"input"`
		Stream bool           `json:"stream"`
	}
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatal(err)
		}
		if gotRequest.Model != "gpt-5.4-mini" {
			t.Fatalf("unexpected model: %q", gotRequest.Model)
		}
		if gotRequest.Stream {
			t.Fatalf("unexpected stream request")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","output_text":"  未来感产品海报，金属质感，柔和棚拍光，干净背景  ","output":[]}`))
	}))
	defer apiServer.Close()

	ui := &webUIServer{
		clientOpts: clientOptions{
			model:      stringPtr("gpt-5.4-mini"),
			imageModel: stringPtr("test-model"),
			baseURL:    stringPtr(apiServer.URL),
			ossURL:     stringPtr(""),
			apiKey:     stringPtr("secret-token"),
			username:   stringPtr(""),
			password:   stringPtr(""),
			verbose:    boolPtr(false),
			logFile:    stringPtr(""),
		},
		fileFormat: fileFormatBase64,
	}

	body := &strings.Builder{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("prompt", "未来感产品海报")
	imagePart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="images"; filename="ref.png"`},
		"Content-Type":        {"image/png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = imagePart.Write([]byte("fake"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/prompt/auto", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()

	ui.handleAutoPrompt(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	if got := strings.TrimSpace(rr.Body.String()); !strings.Contains(got, `"prompt":"未来感产品海报，金属质感，柔和棚拍光，干净背景"`) {
		t.Fatalf("unexpected response body: %s", rr.Body.String())
	}
	if len(gotRequest.Input) != 2 {
		t.Fatalf("unexpected input messages: %#v", gotRequest.Input)
	}
	if gotRequest.Input[0].Role != "system" {
		t.Fatalf("expected system role, got: %q", gotRequest.Input[0].Role)
	}
	if gotRequest.Input[1].Role != "user" {
		t.Fatalf("expected user role, got: %q", gotRequest.Input[1].Role)
	}
	if len(gotRequest.Input[1].Content) != 2 || gotRequest.Input[1].Content[0].Text != "未来感产品海报" {
		t.Fatalf("unexpected user content: %#v", gotRequest.Input[1].Content)
	}
	if gotRequest.Input[1].Content[1].Type != "input_image" || !strings.HasPrefix(gotRequest.Input[1].Content[1].ImageURL, "data:image/") {
		t.Fatalf("unexpected reference image content: %#v", gotRequest.Input[1].Content[1])
	}
}

func TestWebUIHandleAutoPromptStreamsResponses(t *testing.T) {
	var gotRequest struct {
		Model  string         `json:"model"`
		Input  []inputMessage `json:"input"`
		Stream bool           `json:"stream"`
	}
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatal(err)
		}
		if gotRequest.Model != "gpt-5.4-mini" {
			t.Fatalf("unexpected model: %q", gotRequest.Model)
		}
		if !gotRequest.Stream {
			t.Fatalf("expected streaming request")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"未来感产品海报，\"}\n\n" +
				"data: {\"type\":\"response.output_text.delta\",\"delta\":\"金属质感，柔和棚拍光，干净背景\"}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"output_text\":\"未来感产品海报，金属质感，柔和棚拍光，干净背景\",\"output\":[]}}\n\n",
		))
	}))
	defer apiServer.Close()

	ui := &webUIServer{
		clientOpts: clientOptions{
			model:      stringPtr("gpt-5.4-mini"),
			imageModel: stringPtr("test-model"),
			baseURL:    stringPtr(apiServer.URL),
			ossURL:     stringPtr(""),
			apiKey:     stringPtr("secret-token"),
			username:   stringPtr(""),
			password:   stringPtr(""),
			verbose:    boolPtr(false),
			logFile:    stringPtr(""),
		},
		fileFormat: fileFormatBase64,
	}

	body := &strings.Builder{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("prompt", "未来感产品海报")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/prompt/auto?stream=1", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()

	ui.handleAutoPrompt(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Body.String(); !strings.Contains(got, "event: text") || !strings.Contains(got, "event: done") {
		t.Fatalf("unexpected stream body: %s", got)
	}
	if !strings.Contains(rr.Body.String(), `"prompt":"未来感产品海报，金属质感，柔和棚拍光，干净背景"`) {
		t.Fatalf("unexpected stream body: %s", rr.Body.String())
	}
}
