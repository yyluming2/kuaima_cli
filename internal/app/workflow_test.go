package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkflowNeedsReview(t *testing.T) {
	t.Run("response before image", func(t *testing.T) {
		wf := workflowDefinition{
			Steps: []workflowStepDefinition{
				{ID: "brief", Kind: "response"},
				{ID: "image", Kind: "image"},
			},
		}
		if !workflowNeedsReview(wf) {
			t.Fatal("expected workflow to need review")
		}
	})

	t.Run("image before response", func(t *testing.T) {
		wf := workflowDefinition{
			Steps: []workflowStepDefinition{
				{ID: "image", Kind: "image"},
				{ID: "brief", Kind: "response"},
			},
		}
		if workflowNeedsReview(wf) {
			t.Fatal("expected workflow to skip review")
		}
	})
}

func TestWorkflowStepTextsFromForm(t *testing.T) {
	form := url.Values{}
	form.Set("workflow_step_texts", `{"brief":"  refined copy  ","empty":""}`)
	req := httptest.NewRequest("POST", "/", nil)
	req.Form = form
	req.PostForm = form

	values, err := workflowStepTextsFromForm(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := values["brief"]; got != "refined copy" {
		t.Fatalf("unexpected brief text: %q", got)
	}
	if got := values["empty"]; got != "" {
		t.Fatalf("unexpected empty text: %q", got)
	}
}

func TestWorkflowTemplateStepTextOverride(t *testing.T) {
	data := workflowTemplateData{
		Steps: map[string]workflowStepResult{
			"brief": {Text: "generated copy"},
		},
		Overrides: map[string]string{
			"brief": "edited copy",
		},
	}
	fn := workflowTemplateFuncMap(data)["stepText"].(func(string) string)
	if got := fn("brief"); got != "edited copy" {
		t.Fatalf("unexpected step text: %q", got)
	}
}

func TestWorkflowOnlyStep(t *testing.T) {
	steps := []workflowStepDefinition{
		{ID: "brief", Kind: "response"},
		{ID: "main", Kind: "image"},
	}
	got := workflowOnlyStep(steps, "MAIN")
	if len(got) != 1 || got[0].ID != "main" {
		t.Fatalf("unexpected filtered steps: %#v", got)
	}
	if got := workflowOnlyStep(steps, "missing"); len(got) != 0 {
		t.Fatalf("expected no steps, got %#v", got)
	}
}

func TestWorkflowRunSaveDirCreatesUniqueSubdirectories(t *testing.T) {
	base := t.TempDir()
	first, err := workflowRunSaveDir(base, "Product Pack", "")
	if err != nil {
		t.Fatalf("create first dir: %v", err)
	}
	second, err := workflowRunSaveDir(base, "Product Pack", "")
	if err != nil {
		t.Fatalf("create second dir: %v", err)
	}
	if first == second {
		t.Fatalf("expected unique dirs, got %q", first)
	}
	if filepath.Dir(first) != base || filepath.Dir(second) != base {
		t.Fatalf("expected dirs under %q, got %q and %q", base, first, second)
	}
	if !strings.Contains(filepath.Base(first), "product-pack") {
		t.Fatalf("expected workflow name prefix in %q", first)
	}
	if !strings.HasPrefix(filepath.Base(first), "product-pack-") {
		t.Fatalf("expected workflow name at the start of %q", first)
	}
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("first dir not created: %v", err)
	}
	if _, err := os.Stat(second); err != nil {
		t.Fatalf("second dir not created: %v", err)
	}

	reused, err := workflowRunSaveDir(base, "Product Pack", first)
	if err != nil {
		t.Fatalf("reuse dir: %v", err)
	}
	if reused != first {
		t.Fatalf("expected reused dir %q, got %q", first, reused)
	}
	if _, err := workflowRunSaveDir(base, "Product Pack", filepath.Dir(base)); err == nil {
		t.Fatal("expected outside directory to be rejected")
	}
}

func TestWorkflowRunSaveDirKeepsChineseWorkflowName(t *testing.T) {
	dir, err := workflowRunSaveDir(t.TempDir(), "电商套图", "")
	if err != nil {
		t.Fatalf("create workflow run directory: %v", err)
	}
	if !strings.HasPrefix(filepath.Base(dir), "电商套图-") {
		t.Fatalf("expected Chinese workflow name prefix in %q", dir)
	}
}

func TestWorkflowTextFileContentIncludesGeneratedAndOverrideText(t *testing.T) {
	content := workflowTextFileContent(workflowRuntimeInput{
		StepTexts: map[string]string{"copy": "edited copy"},
	}, workflowRunResult{
		WorkflowName: "Product Pack",
		Prompt:       "brief",
		Steps: []workflowStepResult{
			{ID: "brief", Title: "Brief", Text: "generated brief"},
			{ID: "image", Title: "Image"},
		},
	})
	for _, want := range []string{"# Product Pack", "原始提示：", "brief", "## Brief", "generated brief", "## copy", "edited copy"} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %q in content:\n%s", want, content)
		}
	}
}

func TestWorkflowConcurrencyLimit(t *testing.T) {
	if got := workflowConcurrencyLimit(); got != 1 {
		t.Fatalf("default concurrency = %d", got)
	}
	if got := workflowConcurrencyLimit(0); got != 1 {
		t.Fatalf("zero concurrency = %d", got)
	}
	if got := workflowConcurrencyLimit(3); got != 3 {
		t.Fatalf("vip concurrency = %d", got)
	}
}

func TestRunWorkflowRunsImageStepsConcurrently(t *testing.T) {
	var active int32
	var maxActive int32
	var calls int32
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		current := atomic.AddInt32(&active, 1)
		for {
			seen := atomic.LoadInt32(&maxActive)
			if current <= seen || atomic.CompareAndSwapInt32(&maxActive, seen, current) {
				break
			}
		}
		atomic.AddInt32(&calls, 1)
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt32(&active, -1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","output":[{"type":"image_generation_call","status":"completed","result":"ZmFrZQ=="}],"output_text":""}`))
	}))
	defer apiServer.Close()

	c := &client{
		baseURL:    apiServer.URL,
		apiKey:     "secret-token",
		httpClient: apiServer.Client(),
	}
	ui := &webUIServer{fileFormat: fileFormatBase64}
	count := 1
	saveDir := t.TempDir()
	result, err := ui.runWorkflow(context.Background(), c, workflowDefinition{
		ID:   "concurrent",
		Name: "Concurrent",
		Steps: []workflowStepDefinition{
			{ID: "one", Kind: "image", Title: "One", Prompt: "one", Endpoint: "image", Count: "1"},
			{ID: "two", Kind: "image", Title: "Two", Prompt: "two", Endpoint: "image", Count: "1"},
		},
	}, workflowRuntimeInput{
		BaseOptions:    imageOptions{count: &count},
		ImageModel:     "test-image-model",
		Endpoint:       "image",
		SaveDir:        saveDir,
		PublicSaveDir:  saveDir,
		MaxConcurrency: 2,
	}, nil)
	if err != nil {
		t.Fatalf("run workflow: %v", err)
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Fatalf("expected at least 2 image requests, got %d", calls)
	}
	if atomic.LoadInt32(&maxActive) < 2 {
		t.Fatalf("expected concurrent image requests, max active was %d", maxActive)
	}
	if len(result.Steps) != 2 || len(result.Images) != 2 {
		t.Fatalf("unexpected workflow result: %#v", result)
	}
}

func TestRunWorkflowUsesStepSizeForUpscaling(t *testing.T) {
	var imageReqSizes []string
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if size, _ := payload["size"].(string); size != "" {
			imageReqSizes = append(imageReqSizes, size)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","output":[{"type":"image_generation_call","status":"completed","result":"` + encoded + `"}],"output_text":""}`))
	}))
	defer apiServer.Close()

	c := &client{
		baseURL:    apiServer.URL,
		apiKey:     "secret-token",
		httpClient: apiServer.Client(),
	}
	ui := &webUIServer{fileFormat: fileFormatBase64}
	count := 1
	saveDir := t.TempDir()

	result, err := ui.runWorkflow(context.Background(), c, workflowDefinition{
		ID:   "resolution",
		Name: "Resolution",
		Steps: []workflowStepDefinition{
			{ID: "image", Kind: "image", Title: "Image", Prompt: "img", Endpoint: "image", Count: "1", Size: "1536x1024"},
		},
	}, workflowRuntimeInput{
		BaseOptions:    imageOptions{count: &count, upscale: boolPtr(true), size: stringPtr("1024x1024")},
		ImageModel:     "test-image-model",
		Endpoint:       "image",
		SaveDir:        saveDir,
		PublicSaveDir:  saveDir,
		MaxConcurrency: 1,
	}, nil)
	if err != nil {
		t.Fatalf("run workflow: %v", err)
	}
	if len(imageReqSizes) != 1 || imageReqSizes[0] != "1536x1024" {
		t.Fatalf("expected step size to be used, got %#v", imageReqSizes)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("unexpected workflow result: %#v", result)
	}
	if len(result.Saved) != 1 {
		t.Fatalf("expected one saved image, got %#v", result.Saved)
	}
	f, err := os.Open(result.Saved[0])
	if err != nil {
		t.Fatalf("open saved image: %v", err)
	}
	defer f.Close()
	dim, _, err := image.DecodeConfig(f)
	if err != nil {
		t.Fatalf("decode saved image config: %v", err)
	}
	if dim.Width != 1024 || dim.Height != 1024 {
		t.Fatalf("expected saved image to upscale within the step target, got %dx%d", dim.Width, dim.Height)
	}
}

func TestCommerceWorkflowSizes(t *testing.T) {
	if got := commerceWorkflowSize("2k", "main"); got != "2048x2048" {
		t.Fatalf("unexpected main size: %q", got)
	}
	if got := commerceWorkflowSize("4k", "detail"); got != "2160x3840" {
		t.Fatalf("unexpected detail size: %q", got)
	}
	if got := commerceWorkflowSize("unknown", "detail"); got != "1024x1536" {
		t.Fatalf("unexpected fallback detail size: %q", got)
	}
}

func TestBuiltinWorkflowTemplatesRender(t *testing.T) {
	data := workflowTemplateData{
		Prompt: "",
		Params: map[string]string{
			"product_brief":    "test product",
			"platform":         "淘宝",
			"copy_language":    "简体中文",
			"audience":         "年轻女性",
			"visual_style":     "高级感",
			"image_resolution": "1k",
			"tone":             "真实",
			"image_count":      "1",
		},
		Steps: map[string]workflowStepResult{
			"brief": {Text: "core selling point"},
			"copy":  {Text: "copy text"},
		},
		Overrides: map[string]string{},
	}
	catalog, err := builtinWorkflowCatalog()
	if err != nil {
		t.Fatalf("load builtin workflows: %v", err)
	}
	for _, workflow := range catalog.Workflows {
		for _, step := range workflow.Steps {
			if _, err := renderWorkflowTemplate(step.Prompt, data); err != nil {
				t.Fatalf("render prompt for %s/%s: %v", workflow.ID, step.ID, err)
			}
			if step.Size != "" {
				if _, err := renderWorkflowTemplate(step.Size, data); err != nil {
					t.Fatalf("render size for %s/%s: %v", workflow.ID, step.ID, err)
				}
			}
		}
	}
}
