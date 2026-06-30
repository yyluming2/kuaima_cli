package app

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed webui/*
var webUIAssets embed.FS

type webUIServer struct {
	clientOpts     clientOptions
	responseSystem string
	defaults       webUIDefaults
	fileFormat     string
	mu             sync.RWMutex
	saveDir        string
}

type webUIDefaults struct {
	Version           string `json:"version"`
	ImageModel        string `json:"image_model"`
	ImageSize         string `json:"image_size"`
	ImageQuality      string `json:"image_quality"`
	ImageCount        int    `json:"image_count"`
	OutputFormat      string `json:"image_output_format"`
	OutputCompression int    `json:"image_output_compression"`
	Background        string `json:"image_background"`
	Moderation        string `json:"image_moderation"`
	ImageUpscale      bool   `json:"image_upscale"`
}

func runWebUI(args []string) error {
	return runWebUIWithOptions(args, webUIRuntimeOptions{})
}

func runWebUIAuto(args []string) error {
	return runWebUIWithOptions(args, webUIRuntimeOptions{HideConsole: true})
}

func runWebUIWithOptions(args []string, runtimeOpts webUIRuntimeOptions) error {
	restoreConfigPath := setPreferHomeConfigPath(true)
	defer restoreConfigPath()

	cfg, err := loadAppConfig()
	if err != nil {
		return err
	}
	fs := newFlagSet("webui")

	if cfg.FileFormat == nil || *cfg.FileFormat == "" {
		defaultFileFormat := "url"
		cfg.FileFormat = &defaultFileFormat
	}
	opts := addResponseFlagsWithConfig(fs, cfg)
	inputOpts := addInputFlagsWithConfig(fs, cfg)
	imageOpts := addImageFlags(fs, cfg)
	saveDir := addSaveImagesFlagWithConfig(fs, cfg, defaultOutputPath())
	addr := fs.String("addr", "127.0.0.1:8790", "local listen address")
	open := fs.Bool("open", true, "open the Web UI in the default browser")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if err := imageOpts.validate(); err != nil {
		return err
	}
	if err := persistConfigFlags(fs, cfg); err != nil {
		return err
	}
	if strings.TrimSpace(*saveDir) == "" {
		return errors.New("webui requires a non-empty -save-images directory")
	}
	absSaveDir, err := filepath.Abs(strings.TrimSpace(*saveDir))
	if err != nil {
		return err
	}
	if cfg.System == nil || *cfg.System == "" {
		defaultSystem := "Just generate the image; do not return a prompt or SVG"
		if imageOpts.size != nil && *imageOpts.size != "" {
			defaultSystem += "; image size is " + *imageOpts.size
		}
		cfg.System = &defaultSystem
	}

	releaseInstance, acquired, err := acquireWebUIInstance()
	if err != nil {
		return err
	}
	if !acquired {
		url := readWebUIInstanceURL()
		if url == "" {
			url = "http://" + strings.TrimSpace(*addr)
		}
		if *open {
			_ = openBrowser(url)
		}
		message := "Web UI 已经在运行: " + url
		if runtimeOpts.HideConsole {
			notifyWebUIAlreadyRunning(message)
		} else {
			fmt.Println(message)
		}
		return nil
	}
	defer releaseInstance()

	ln, err := net.Listen("tcp", strings.TrimSpace(*addr))
	if err != nil {
		return err
	}
	ui := &webUIServer{
		clientOpts:     opts.clientOptions,
		responseSystem: strings.TrimSpace(*opts.system),
		fileFormat:     strings.TrimSpace(*inputOpts.fileFormat),
		defaults: webUIDefaults{
			Version:           AppVersion,
			ImageModel:        opts.imageGenerationModel(),
			ImageSize:         stringValue(imageOpts.size),
			ImageQuality:      stringValue(imageOpts.quality),
			ImageCount:        intValue(imageOpts.count, 1),
			OutputFormat:      stringValue(imageOpts.outputFormat),
			OutputCompression: intValue(imageOpts.outputCompression, -1),
			Background:        stringValue(imageOpts.background),
			Moderation:        stringValue(imageOpts.moderation),
			ImageUpscale:      boolValue(imageOpts.upscale, true),
		},
		saveDir: absSaveDir,
	}
	mux := http.NewServeMux()
	ui.routes(mux)

	url := "http://" + ln.Addr().String()
	_ = writeWebUIInstanceURL(url)
	defer removeWebUIInstanceURL()
	if runtimeOpts.HideConsole {
		hideWebUIConsole()
	} else {
		fmt.Println("Web UI:", url)
	}

	server := &http.Server{Handler: mux}
	stopTray := startWebUITray(url, func() {
		_ = server.Close()
	})
	if stopTray != nil {
		defer stopTray()
	}

	go func() {
		if *open {
			time.Sleep(500 * time.Millisecond)
			if err := openBrowser(url); err != nil {
				if !runtimeOpts.HideConsole {
					fmt.Println("open browser failed:", err)
				}
			}
		}
	}()

	err = server.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *webUIServer) routes(mux *http.ServeMux) {
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/workflows", s.handleWorkflows)
	mux.HandleFunc("/api/workflow/run", s.handleWorkflowRun)
	mux.HandleFunc("/api/prompt/auto", s.handleAutoPrompt)
	mux.HandleFunc("/api/generate", s.handleGenerate)
	mux.HandleFunc("/api/generate/stream", s.handleGenerateStream)
	mux.HandleFunc("/api/agent/stream", s.handleAgentStream)
	mux.HandleFunc("/api/balance", s.handleBalance)
	mux.HandleFunc("/api/account/link", s.handleAccountLink)
	mux.HandleFunc("/api/recharge/info", s.handleRechargeInfo)
	mux.HandleFunc("/api/recharge/pay", s.handleRechargePay)
	mux.HandleFunc("/api/qr", s.handleQR)
	mux.HandleFunc("/api/history/upload", s.handleHistoryReferenceUpload)
	mux.HandleFunc("/api/output/set", s.handleSetOutputDir)
	mux.HandleFunc("/api/output/open", s.handleOpenOutputDir)
	mux.HandleFunc("/outputs/", s.handleOutputFile)
	assets, err := fs.Sub(webUIAssets, "webui")
	if err != nil {
		panic(err)
	}
	mux.Handle("/", http.FileServer(http.FS(assets)))
}

func (s *webUIServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	data, err := json.Marshal(s.defaults)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	value["save_dir"] = s.saveDirectory()
	writeJSON(w, http.StatusOK, value)
}

func (s *webUIServer) handleAutoPrompt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var (
		prompt string
		refs   []imageRef
		err    error
		c      *client
	)
	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	switch {
	case strings.Contains(contentType, "multipart/form-data"):
		if err := r.ParseMultipartForm(80 << 20); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		prompt = strings.TrimSpace(r.FormValue("prompt"))
		needUploadClient := len(r.MultipartForm.File["images"]) > 0 && strings.EqualFold(strings.TrimSpace(s.fileFormat), fileFormatURL)
		if needUploadClient {
			c, err = s.clientOpts.newClient()
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			defer c.Close()
		}
		refs, err = uploadedImageRefs(r, c, s.fileFormat)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	case strings.Contains(contentType, "application/json"):
		var req struct {
			Prompt string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		prompt = strings.TrimSpace(req.Prompt)
	default:
		if err := r.ParseMultipartForm(80 << 20); err == nil && r.MultipartForm != nil {
			prompt = strings.TrimSpace(r.FormValue("prompt"))
			needUploadClient := len(r.MultipartForm.File["images"]) > 0 && strings.EqualFold(strings.TrimSpace(s.fileFormat), fileFormatURL)
			if needUploadClient {
				c, err = s.clientOpts.newClient()
				if err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				defer c.Close()
			}
			refs, err = uploadedImageRefs(r, c, s.fileFormat)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		} else {
			var req struct {
				Prompt string `json:"prompt"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			prompt = strings.TrimSpace(req.Prompt)
		}
	}
	if prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}
	wantStream := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("stream")), "1") ||
		strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("stream")), "true") ||
		strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream")
	if c == nil {
		c, err = s.clientOpts.newClient()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer c.Close()
	}
	if wantStream {
		stream := newWebUIEventStream(w)
		resp, err := c.createResponseStream(r.Context(), responseRequest{
			Model: strings.TrimSpace(*s.clientOpts.model),
			Input: s.autoPromptInput(prompt, refs),
		}, stream.textWriter())
		if err != nil {
			stream.send("error", map[string]string{"error": err.Error()})
			return
		}
		text := normalizeAutoPrompt(resp.text())
		if text == "" {
			stream.send("error", map[string]string{"error": "auto prompt returned empty text"})
			return
		}
		stream.send("done", map[string]string{"prompt": text, "text": text})
		return
	}
	resp, err := c.createResponse(r.Context(), responseRequest{
		Model: strings.TrimSpace(*s.clientOpts.model),
		Input: s.autoPromptInput(prompt, refs),
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	text := normalizeAutoPrompt(resp.text())
	if text == "" {
		writeError(w, http.StatusBadGateway, "auto prompt returned empty text")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"prompt": text})
}

func (s *webUIServer) handleGenerate(w http.ResponseWriter, r *http.Request) {
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
	opts, err := s.imageOptionsFromForm(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	c, err := s.clientOpts.newClient()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer c.Close()
	group := accountGroupFromClient(r.Context(), c)
	limit := imageGenerationCountLimit(group)
	count := opts.responseRunCount()
	if count > limit {
		writeError(w, http.StatusBadRequest, imageGenerationCountLimitMessage(group, count))
		return
	}
	refs, err := uploadedImageRefs(r, c, s.fileFormat)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	mask, err := uploadedMaskRef(r, c, s.fileFormat)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if mask != nil && len(refs) == 0 {
		writeError(w, http.StatusBadRequest, "mask requires at least one reference image")
		return
	}
	ctx := r.Context()
	saveDir, err := s.saveDirectoryFromForm(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var resp *responsePayload
	endpoint := webUIImageEndpoint(r)
	if endpoint == "response" {
		saved, text, err := s.handleGenerateWithResponses(ctx, c, opts, s.modelFromForm(r), prompt, refs, mask, saveDir, limit)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"images": s.publicImageURLs(saved, saveDir),
			"saved":  saved,
			"text":   text,
		})
		return
	}
	resp, err = s.handleGenerateWithImageEndpoint(ctx, c, opts, s.modelFromForm(r), prompt, refs, mask, limit)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	saved, err := saveImagesFromResponse(ctx, c.httpClient, resp, saveDir, boolValue(opts.upscale, s.defaults.ImageUpscale), stringValue(opts.size))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"images": s.publicImageURLs(saved, saveDir),
		"saved":  saved,
		"text":   strings.TrimSpace(resp.OutputText),
	})
}

func (s *webUIServer) handleGenerateStream(w http.ResponseWriter, r *http.Request) {
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
	opts, err := s.imageOptionsFromForm(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	c, err := s.clientOpts.newClient()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer c.Close()
	group := accountGroupFromClient(r.Context(), c)
	limit := imageGenerationCountLimit(group)
	count := opts.responseRunCount()
	if count > limit {
		writeError(w, http.StatusBadRequest, imageGenerationCountLimitMessage(group, count))
		return
	}
	refs, err := uploadedImageRefs(r, c, s.fileFormat)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	mask, err := uploadedMaskRef(r, c, s.fileFormat)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if mask != nil && len(refs) == 0 {
		writeError(w, http.StatusBadRequest, "mask requires at least one reference image")
		return
	}
	stream := newWebUIEventStream(w)

	ctx := r.Context()
	saveDir, err := s.saveDirectoryFromForm(r)
	if err != nil {
		stream.send("error", map[string]string{"error": err.Error()})
		return
	}
	saver := newResponseImageSaver(ctx, c.httpClient, saveDir, boolValue(opts.upscale, s.defaults.ImageUpscale), stringValue(opts.size))
	var saved []string
	saveAndSend := func(candidate imageCandidate) error {
		path, err := saver.saveCandidate(candidate)
		if err != nil {
			return err
		}
		if path == "" {
			return nil
		}
		saved = append(saved, path)
		urls := s.publicImageURLs([]string{path}, saveDir)
		if len(urls) > 0 {
			stream.send("image", map[string]string{"url": urls[0]})
		}
		return nil
	}

	var resp *responsePayload
	endpoint := webUIImageEndpoint(r)
	if endpoint == "response" {
		resp, err = s.handleGenerateWithResponsesStream(ctx, c, opts, s.modelFromForm(r), prompt, refs, mask, stream, saveAndSend, limit)
	} else {
		resp, err = s.handleGenerateWithImageEndpointStream(ctx, c, opts, s.modelFromForm(r), prompt, refs, mask, stream, saveAndSend, limit)
	}
	if err != nil {
		stream.send("error", map[string]string{"error": err.Error()})
		return
	}
	if saver != nil && resp != nil {
		more, err := saver.saveResponse(resp)
		if err != nil {
			stream.send("error", map[string]string{"error": err.Error()})
			return
		}
		for _, path := range more {
			saved = append(saved, path)
			urls := s.publicImageURLs([]string{path}, saveDir)
			if len(urls) > 0 {
				stream.send("image", map[string]string{"url": urls[0]})
			}
		}
	}
	text := ""
	if resp != nil {
		text = strings.TrimSpace(resp.OutputText)
	}
	stream.send("done", map[string]any{
		"images": s.publicImageURLs(saved, saveDir),
		"saved":  saved,
		"text":   text,
	})
}

type agentChatRequest struct {
	Model    string         `json:"model"`
	Messages []inputMessage `json:"messages"`
}

func (s *webUIServer) handleAgentStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req agentChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages are required")
		return
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(*s.clientOpts.model)
	}
	c, err := s.clientOpts.newClient()
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer c.Close()

	messages := append([]inputMessage(nil), req.Messages...)
	if strings.TrimSpace(s.responseSystem) != "" && (len(messages) == 0 || strings.TrimSpace(messages[0].Role) != "system") {
		messages = append([]inputMessage{{
			Role: "system",
			Content: []inputContent{{
				Type: "input_text",
				Text: strings.TrimSpace(s.responseSystem),
			}},
		}}, messages...)
	}

	stream := newWebUIEventStream(w)
	stream.send("status", map[string]string{"message": "正在回复..."})
	resp, err := c.createResponseStream(r.Context(), responseRequest{
		Model:  model,
		Input:  messages,
		Stream: true,
	}, stream.textWriter())
	if err != nil {
		stream.send("error", map[string]string{"error": err.Error()})
		return
	}
	stream.send("done", map[string]string{"text": strings.TrimSpace(resp.OutputText)})
}

func (s *webUIServer) handleGenerateWithImageEndpoint(ctx context.Context, c *client, opts imageOptions, imageModel, prompt string, refs []imageRef, mask *imageRef, maxConcurrency ...int) (*responsePayload, error) {
	runCount := opts.responseRunCount()
	if workflowConcurrencyLimit(maxConcurrency...) > 1 && runCount > 1 {
		return s.handleGenerateWithImageEndpointConcurrent(ctx, c, opts, imageModel, prompt, refs, mask, nil, nil, workflowConcurrencyLimit(maxConcurrency...))
	}
	var combined responsePayload
	var texts []string
	for i := 0; i < runCount; i++ {
		var resp *responsePayload
		var err error
		if len(refs) == 0 {
			req := opts.generationRequest(imageModel, prompt)
			req.N = 1
			resp, err = c.createImageGeneration(ctx, req)
		} else {
			req := opts.editRequest(imageModel, prompt, refs, mask)
			req.N = 1
			req.FileFormat = s.fileFormat
			resp, err = c.createImageEdit(ctx, req)
		}
		if err != nil {
			return &combined, err
		}
		if resp == nil {
			continue
		}
		combined.Output = append(combined.Output, resp.Output...)
		if text := strings.TrimSpace(resp.OutputText); text != "" {
			texts = append(texts, text)
		}
	}
	combined.OutputText = strings.Join(texts, "\n\n")
	return &combined, nil
}

func (s *webUIServer) handleGenerateWithImageEndpointStream(ctx context.Context, c *client, opts imageOptions, imageModel, prompt string, refs []imageRef, mask *imageRef, stream *webUIEventStream, onImage func(imageCandidate) error, maxConcurrency ...int) (*responsePayload, error) {
	runCount := opts.responseRunCount()
	if workflowConcurrencyLimit(maxConcurrency...) > 1 && runCount > 1 {
		return s.handleGenerateWithImageEndpointConcurrent(ctx, c, opts, imageModel, prompt, refs, mask, stream, onImage, workflowConcurrencyLimit(maxConcurrency...))
	}
	var combined responsePayload
	var texts []string
	for i := 0; i < runCount; i++ {
		if runCount > 1 {
			stream.send("status", map[string]string{"message": fmt.Sprintf("正在生成第 %d/%d 张...", i+1, runCount)})
		}
		var resp *responsePayload
		var err error
		if len(refs) == 0 {
			req := opts.generationRequest(imageModel, prompt)
			req.N = 1
			resp, err = c.createImageGenerationStream(ctx, req, onImage)
		} else {
			req := opts.editRequest(imageModel, prompt, refs, mask)
			req.N = 1
			req.FileFormat = s.fileFormat
			resp, err = c.createImageEditStream(ctx, req, onImage)
		}
		if err != nil {
			return &combined, err
		}
		if resp == nil {
			continue
		}
		combined.Output = append(combined.Output, resp.Output...)
		if text := strings.TrimSpace(resp.OutputText); text != "" {
			texts = append(texts, text)
		}
	}
	combined.OutputText = strings.Join(texts, "\n\n")
	return &combined, nil
}

func (s *webUIServer) handleGenerateWithResponsesStream(ctx context.Context, c *client, opts imageOptions, imageModel, prompt string, refs []imageRef, mask *imageRef, stream *webUIEventStream, onImage func(imageCandidate) error, maxConcurrency ...int) (*responsePayload, error) {
	tool := opts.responseTool()
	imageModel = strings.TrimSpace(imageModel)
	addAutoString(tool, "model", &imageModel)
	addResponseImageMask(tool, mask)
	req := responseRequest{
		Model:     strings.TrimSpace(*s.clientOpts.model),
		Input:     s.responseInput(prompt, refs),
		Tools:     []map[string]any{tool},
		Reasoning: defaultImageReasoning,
		Stream:    true,
	}
	runCount := opts.responseRunCount()
	if workflowConcurrencyLimit(maxConcurrency...) > 1 && runCount > 1 {
		return s.handleGenerateWithResponsesConcurrent(ctx, c, req, runCount, stream, onImage, workflowConcurrencyLimit(maxConcurrency...))
	}
	var combined responsePayload
	var texts []string
	for i := 0; i < runCount; i++ {
		if runCount > 1 {
			stream.send("status", map[string]string{"message": fmt.Sprintf("正在生成第 %d/%d 张...", i+1, runCount)})
		}
		resp, err := c.createResponseStreamWithImages(ctx, req, stream.textWriter(), onImage)
		if err != nil {
			return &combined, err
		}
		if resp == nil {
			continue
		}
		combined.Output = append(combined.Output, resp.Output...)
		if text := strings.TrimSpace(resp.OutputText); text != "" {
			texts = append(texts, text)
		}
	}
	combined.OutputText = strings.Join(texts, "\n\n")
	return &combined, nil
}

func (s *webUIServer) handleGenerateWithResponses(ctx context.Context, c *client, opts imageOptions, imageModel, prompt string, refs []imageRef, mask *imageRef, saveDir string, maxConcurrency ...int) ([]string, string, error) {
	tool := opts.responseTool()
	imageModel = strings.TrimSpace(imageModel)
	addAutoString(tool, "model", &imageModel)
	addResponseImageMask(tool, mask)
	req := responseRequest{
		Model:     strings.TrimSpace(*s.clientOpts.model),
		Input:     s.responseInput(prompt, refs),
		Tools:     []map[string]any{tool},
		Reasoning: defaultImageReasoning,
	}
	runCount := opts.responseRunCount()
	if workflowConcurrencyLimit(maxConcurrency...) > 1 && runCount > 1 {
		resp, err := s.handleGenerateWithResponsesConcurrent(ctx, c, req, runCount, nil, nil, workflowConcurrencyLimit(maxConcurrency...))
		if err != nil {
			return nil, "", err
		}
		saved, err := saveImagesFromResponse(ctx, c.httpClient, resp, saveDir, boolValue(opts.upscale, s.defaults.ImageUpscale), stringValue(opts.size))
		if err != nil {
			return saved, strings.TrimSpace(resp.OutputText), err
		}
		return saved, strings.TrimSpace(resp.OutputText), nil
	}
	var saved []string
	var texts []string
	for i := 0; i < runCount; i++ {
		resp, err := c.createResponse(ctx, req)
		if err != nil {
			return saved, strings.Join(texts, "\n\n"), err
		}
		more, err := saveImagesFromResponse(ctx, c.httpClient, resp, saveDir, boolValue(opts.upscale, s.defaults.ImageUpscale), stringValue(opts.size))
		if err != nil {
			return saved, strings.Join(texts, "\n\n"), err
		}
		saved = append(saved, more...)
		if text := strings.TrimSpace(resp.OutputText); text != "" {
			texts = append(texts, text)
		}
	}
	return saved, strings.Join(texts, "\n\n"), nil
}

func (s *webUIServer) handleGenerateWithImageEndpointConcurrent(ctx context.Context, c *client, opts imageOptions, imageModel, prompt string, refs []imageRef, mask *imageRef, stream *webUIEventStream, onImage func(imageCandidate) error, maxConcurrency int) (*responsePayload, error) {
	runCount := opts.responseRunCount()
	return runConcurrentImageResponses(ctx, runCount, maxConcurrency, stream, func(ctx context.Context) (*responsePayload, error) {
		if len(refs) == 0 {
			req := opts.generationRequest(imageModel, prompt)
			req.N = 1
			return c.createImageGeneration(ctx, req)
		}
		req := opts.editRequest(imageModel, prompt, refs, mask)
		req.N = 1
		req.FileFormat = s.fileFormat
		return c.createImageEdit(ctx, req)
	}, onImage)
}

func (s *webUIServer) handleGenerateWithResponsesConcurrent(ctx context.Context, c *client, req responseRequest, runCount int, stream *webUIEventStream, onImage func(imageCandidate) error, maxConcurrency int) (*responsePayload, error) {
	return runConcurrentImageResponses(ctx, runCount, maxConcurrency, stream, func(ctx context.Context) (*responsePayload, error) {
		if stream != nil {
			return c.createResponseStreamWithImages(ctx, req, stream.textWriter(), onImage)
		}
		return c.createResponse(ctx, req)
	}, onImage)
}

func runConcurrentImageResponses(ctx context.Context, runCount, maxConcurrency int, stream *webUIEventStream, run func(context.Context) (*responsePayload, error), onImage func(imageCandidate) error) (*responsePayload, error) {
	if maxConcurrency < 1 {
		maxConcurrency = 1
	}
	if maxConcurrency > runCount {
		maxConcurrency = runCount
	}
	type item struct {
		resp *responsePayload
		err  error
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan int)
	results := make(chan item, runCount)
	var wg sync.WaitGroup
	for i := 0; i < maxConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				resp, err := run(ctx)
				if err != nil {
					cancel()
				}
				results <- item{resp: resp, err: err}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i := 0; i < runCount; i++ {
			select {
			case <-ctx.Done():
				return
			case jobs <- i:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	var combined responsePayload
	var texts []string
	completed := 0
	for item := range results {
		completed++
		if stream != nil && runCount > 1 {
			stream.send("status", map[string]string{"message": fmt.Sprintf("已生成 %d/%d 张...", completed, runCount)})
		}
		if item.err != nil {
			return &combined, item.err
		}
		if item.resp == nil {
			continue
		}
		combined.Output = append(combined.Output, item.resp.Output...)
		if text := strings.TrimSpace(item.resp.OutputText); text != "" {
			texts = append(texts, text)
		}
		if onImage != nil {
			for _, candidate := range imageCandidates(item.resp) {
				if err := onImage(candidate); err != nil {
					return &combined, err
				}
			}
		}
	}
	combined.OutputText = strings.Join(texts, "\n\n")
	return &combined, nil
}

func workflowConcurrencyLimit(values ...int) int {
	if len(values) == 0 || values[0] < 1 {
		return 1
	}
	return values[0]
}

func addResponseImageMask(tool map[string]any, mask *imageRef) {
	if tool == nil || mask == nil || strings.TrimSpace(mask.ImageURL) == "" {
		return
	}
	tool["input_image_mask"] = map[string]any{
		"image_url": strings.TrimSpace(mask.ImageURL),
	}
}

func (s *webUIServer) responseInput(prompt string, refs []imageRef) []inputMessage {
	messages := []inputMessage{}
	if strings.TrimSpace(s.responseSystem) != "" {
		messages = append(messages, inputMessage{
			Role: "system",
			Content: []inputContent{{
				Type: "input_text",
				Text: strings.TrimSpace(s.responseSystem),
			}},
		})
	}
	content := []inputContent{{
		Type: "input_text",
		Text: prompt,
	}}
	for _, ref := range refs {
		content = append(content, inputContent{
			Type:     "input_image",
			ImageURL: ref.ImageURL,
		})
	}
	messages = append(messages, inputMessage{
		Role:    "user",
		Content: content,
	})
	return messages
}

func (s *webUIServer) autoPromptInput(prompt string, refs []imageRef) []inputMessage {
	content := []inputContent{{
		Type: "input_text",
		Text: prompt,
	}}
	for _, ref := range refs {
		content = append(content, inputContent{
			Type:     "input_image",
			ImageURL: ref.ImageURL,
		})
	}
	return []inputMessage{
		{
			Role: "system",
			Content: []inputContent{{
				Type: "input_text",
				Text: "You rewrite rough image ideas into a single high-quality image generation prompt. Output only the rewritten prompt. No bullets, no markdown, no quotes, no explanation. Preserve the user's intent, use the reference images if provided, and add concrete visual details where helpful. Keep the result in Chinese unless the user clearly asks for another language.",
			}},
		},
		{
			Role:    "user",
			Content: content,
		},
	}
}

func normalizeAutoPrompt(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "提示词：")
	text = strings.TrimPrefix(text, "提示词:")
	text = strings.TrimPrefix(text, "Prompt:")
	text = strings.TrimPrefix(text, "prompt:")
	text = strings.Trim(strings.TrimSpace(text), "`\"")
	return strings.Join(strings.Fields(text), " ")
}

func (s *webUIServer) handleBalance(w http.ResponseWriter, r *http.Request) {
	c, err := s.clientOpts.newClient()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer c.Close()
	usage, err := c.getTokenUsage(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, formatWebUsage(usage))
}

func (s *webUIServer) handleAccountLink(w http.ResponseWriter, r *http.Request) {
	username, password, err := resolveRechargeCredentials(r.Context(), *s.clientOpts.baseURL, *s.clientOpts.username, *s.clientOpts.password, *s.clientOpts.verbose, nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	rawURL, err := rechargeURL(*s.clientOpts.baseURL, username, password, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": rawURL})
}

func (s *webUIServer) handleOutputFile(w http.ResponseWriter, r *http.Request) {
	http.StripPrefix("/outputs/", http.FileServer(http.Dir(s.saveDirectory()))).ServeHTTP(w, r)
}

func (s *webUIServer) handleOpenOutputDir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	dir, err := s.saveDirectoryFromBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := openFile(dir); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"save_dir": dir})
}

func (s *webUIServer) handleSetOutputDir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	dir, err := s.saveDirectoryFromBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := persistWebUISaveDir(dir); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"save_dir": dir})
}

func (s *webUIServer) handleRechargeInfo(w http.ResponseWriter, r *http.Request) {
	c, err := s.clientOpts.newClient()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer c.Close()
	info, err := c.getRechargeInfo(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	sort.SliceStable(info.Plans, func(i, j int) bool {
		if info.Plans[i].SortOrder != info.Plans[j].SortOrder {
			return info.Plans[i].SortOrder < info.Plans[j].SortOrder
		}
		return info.Plans[i].ID < info.Plans[j].ID
	})
	writeJSON(w, http.StatusOK, info)
}

func (s *webUIServer) handleRechargePay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Amount        float64 `json:"amount"`
		PlanID        int     `json:"plan_id"`
		PaymentMethod string  `json:"payment_method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Amount <= 0 && req.PlanID <= 0 {
		writeError(w, http.StatusBadRequest, "amount or plan_id is required")
		return
	}
	if req.Amount > 0 && req.PlanID > 0 {
		writeError(w, http.StatusBadRequest, "amount and plan_id cannot both be set")
		return
	}
	if strings.TrimSpace(req.PaymentMethod) == "" {
		req.PaymentMethod = defaultPaymentMethod
	}
	c, err := s.clientOpts.newClient()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer c.Close()
	var payment *paymentData
	if req.Amount > 0 {
		payment, err = c.createRechargePayment(r.Context(), req.Amount, req.PaymentMethod)
	} else {
		payment, err = c.createSubscriptionPayment(r.Context(), req.PlanID, req.PaymentMethod)
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"trade_no": payment.TradeNo,
		"url":      payment.ScanCodeURL,
		"qr":       "/api/qr?data=" + url.QueryEscape(payment.ScanCodeURL),
	})
}

func (s *webUIServer) handleQR(w http.ResponseWriter, r *http.Request) {
	value := strings.TrimSpace(r.URL.Query().Get("data"))
	if value == "" {
		writeError(w, http.StatusBadRequest, "data is required")
		return
	}
	png, err := qrcodePNG(value, 256)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(png)
}

func (s *webUIServer) handleHistoryReferenceUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := r.ParseMultipartForm(80 << 20); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if r.MultipartForm == nil || r.MultipartForm.File == nil {
		writeJSON(w, http.StatusOK, map[string]any{"urls": []string{}})
		return
	}
	files := r.MultipartForm.File["images"]
	if len(files) > 16 {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("too many image inputs: got %d, expected at most 16", len(files)))
		return
	}
	if len(files) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"urls": []string{}})
		return
	}
	saveDir := s.saveDirectory()
	historyDir := filepath.Join(saveDir, "history_refs")
	if err := os.MkdirAll(historyDir, 0755); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	urls := make([]string, 0, len(files))
	stamp := time.Now().UnixNano()
	for index, header := range files {
		file, err := header.Open()
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		data, readErr := io.ReadAll(file)
		closeErr := file.Close()
		if readErr != nil {
			writeError(w, http.StatusBadRequest, readErr.Error())
			return
		}
		if closeErr != nil {
			writeError(w, http.StatusBadRequest, closeErr.Error())
			return
		}
		mediaType := header.Header.Get("Content-Type")
		if mediaType == "" {
			mediaType = mime.TypeByExtension(filepath.Ext(header.Filename))
		}
		if mediaType == "" {
			mediaType = http.DetectContentType(data)
		}
		if !strings.HasPrefix(mediaType, "image/") {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("%s is not an image", header.Filename))
			return
		}

		name := fmt.Sprintf("history-reference-%d-%02d%s", stamp, index+1, extensionForMediaType(mediaType, header.Filename))
		path := filepath.Join(historyDir, name)
		path, err = writeFileUnique(path, data, 0644)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		url := s.publicImageURLs([]string{path}, saveDir)
		if len(url) == 0 {
			writeError(w, http.StatusInternalServerError, "failed to build history reference URL")
			return
		}
		urls = append(urls, url[0])
	}

	writeJSON(w, http.StatusOK, map[string]any{"urls": urls})
}

func (s *webUIServer) modelFromForm(r *http.Request) string {
	if model := strings.TrimSpace(r.FormValue("image_model")); model != "" {
		return model
	}
	return s.defaults.ImageModel
}

func webUIImageEndpoint(r *http.Request) string {
	switch strings.ToLower(strings.TrimSpace(r.FormValue("image_endpoint"))) {
	case "response", "responses":
		return "response"
	default:
		return "image"
	}
}

func (s *webUIServer) imageOptionsFromForm(r *http.Request) (imageOptions, error) {
	size := formDefault(r, "image_size", s.defaults.ImageSize)
	quality := formDefault(r, "image_quality", s.defaults.ImageQuality)
	outputFormat := strings.TrimSpace(r.FormValue("image_output_format"))
	background := strings.TrimSpace(r.FormValue("image_background"))
	moderation := strings.TrimSpace(r.FormValue("image_moderation"))
	action := "auto"
	upscale := s.defaults.ImageUpscale
	if value := strings.TrimSpace(r.FormValue("image_upscale")); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return imageOptions{}, fmt.Errorf("invalid image_upscale: %w", err)
		}
		upscale = parsed
	}
	count, err := formInt(r, "image_count", s.defaults.ImageCount)
	if err != nil {
		return imageOptions{}, err
	}
	compression, err := formInt(r, "image_output_compression", -1)
	if err != nil {
		return imageOptions{}, err
	}
	opts := imageOptions{
		size:              &size,
		quality:           &quality,
		count:             &count,
		outputFormat:      &outputFormat,
		outputCompression: &compression,
		background:        &background,
		moderation:        &moderation,
		action:            &action,
		upscale:           &upscale,
	}
	return opts, opts.validate()
}

func uploadedImageRefs(r *http.Request, c *client, fileFormat string) ([]imageRef, error) {
	if r.MultipartForm == nil || r.MultipartForm.File == nil {
		return nil, nil
	}
	files := r.MultipartForm.File["images"]
	if len(files) > 16 {
		return nil, fmt.Errorf("too many image inputs: got %d, expected at most 16", len(files))
	}
	refs := make([]imageRef, 0, len(files))
	for _, header := range files {
		ref, err := uploadedImageRef(r, c, fileFormat, header)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func uploadedMaskRef(r *http.Request, c *client, fileFormat string) (*imageRef, error) {
	if r.MultipartForm == nil || r.MultipartForm.File == nil {
		return nil, nil
	}
	files := r.MultipartForm.File["mask"]
	if len(files) == 0 {
		return nil, nil
	}
	if len(files) > 1 {
		return nil, fmt.Errorf("too many mask inputs: got %d, expected 1", len(files))
	}
	ref, err := uploadedImageRef(r, c, fileFormat, files[0])
	if err != nil {
		return nil, err
	}
	return &ref, nil
}

func uploadedImageRef(r *http.Request, c *client, fileFormat string, header *multipart.FileHeader) (imageRef, error) {
	file, err := header.Open()
	if err != nil {
		return imageRef{}, err
	}
	data, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil {
		return imageRef{}, readErr
	}
	if closeErr != nil {
		return imageRef{}, closeErr
	}
	mediaType := header.Header.Get("Content-Type")
	if mediaType == "" {
		mediaType = mime.TypeByExtension(filepath.Ext(header.Filename))
	}
	if mediaType == "" {
		mediaType = http.DetectContentType(data)
	}
	if !strings.HasPrefix(mediaType, "image/") {
		return imageRef{}, fmt.Errorf("%s is not an image", header.Filename)
	}
	imageURL := imageDataURL(mediaType, data)
	if strings.EqualFold(strings.TrimSpace(fileFormat), fileFormatURL) {
		if c != nil {
			if uploadedURL, uploadErr := c.uploadLocalData(r.Context(), data, header.Filename); uploadErr == nil && strings.TrimSpace(uploadedURL) != "" {
				imageURL = uploadedURL
			}
		}
	}
	return imageRef{ImageURL: imageURL}, nil
}

func (s *webUIServer) saveDirectory() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.saveDir
}

func (s *webUIServer) saveDirectoryFromForm(r *http.Request) (string, error) {
	value := strings.TrimSpace(r.FormValue("save_dir"))
	if value == "" {
		return s.saveDirectory(), nil
	}
	dir, err := s.setSaveDirectory(value)
	if err != nil {
		return "", err
	}
	return dir, persistWebUISaveDir(dir)
}

func (s *webUIServer) saveDirectoryFromBody(r *http.Request) (string, error) {
	value := strings.TrimSpace(r.FormValue("save_dir"))
	if value == "" && strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		var req struct {
			SaveDir string `json:"save_dir"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		value = req.SaveDir
	}
	if value == "" {
		return s.saveDirectory(), nil
	}
	return s.setSaveDirectory(value)
}

func (s *webUIServer) setSaveDirectory(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("output directory is required")
	}
	dir, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	s.mu.Lock()
	s.saveDir = dir
	s.mu.Unlock()
	return dir, nil
}

func persistWebUISaveDir(dir string) error {
	cfg, err := loadAppConfig()
	if err != nil {
		return err
	}
	cfg.SaveImages = stringPtr(dir)
	return saveAppConfig(cfg)
}

func (s *webUIServer) publicImageURLs(saved []string, saveDir string) []string {
	urls := make([]string, 0, len(saved))
	for _, p := range saved {
		rel, err := filepath.Rel(saveDir, p)
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			continue
		}
		urls = append(urls, "/outputs/"+path.Clean(filepath.ToSlash(rel)))
	}
	return urls
}

func formatWebUsage(usage *tokenUsage) map[string]any {
	subs := make([]map[string]any, 0, len(usage.Subscriptions))
	for _, item := range usage.Subscriptions {
		sub := item.Subscription
		subs = append(subs, map[string]any{
			"status":         sub.Status,
			"total":          sub.AmountTotal,
			"total_text":     formatQuotaAmount(sub.AmountTotal),
			"available":      sub.AmountTotal - sub.AmountUsed,
			"available_text": formatQuotaAmount(sub.AmountTotal - sub.AmountUsed),
			"used":           sub.AmountUsed,
			"used_text":      formatQuotaAmount(sub.AmountUsed),
			"start":          formatTime(sub.StartTime),
			"end":            formatTime(sub.EndTime),
		})
	}
	return map[string]any{
		"name":                 usage.Name,
		"group":                strings.ToLower(strings.TrimSpace(usage.Group)),
		"total_available":      usage.TotalAvailable,
		"total_available_text": formatQuotaAmount(usage.TotalAvailable),
		"total_used":           usage.TotalUsed,
		"total_used_text":      formatQuotaAmount(usage.TotalUsed),
		"subscriptions":        subs,
	}
}

func (s *webUIServer) workflowMaxConcurrency(ctx context.Context, c *client) int {
	return imageGenerationCountLimit(accountGroupFromClient(ctx, c))
}

func formDefault(r *http.Request, name, fallback string) string {
	if value := strings.TrimSpace(r.FormValue(name)); value != "" {
		return value
	}
	return fallback
}

func formInt(r *http.Request, name string, fallback int) (int, error) {
	value := strings.TrimSpace(r.FormValue(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", name, err)
	}
	return parsed, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func intValue(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	if strings.TrimSpace(message) == "" {
		message = http.StatusText(status)
	}
	writeJSON(w, status, map[string]string{"error": message})
}

type webUIEventStream struct {
	w       http.ResponseWriter
	flusher http.Flusher
	mu      sync.Mutex
}

func newWebUIEventStream(w http.ResponseWriter) *webUIEventStream {
	header := w.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache")
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	return &webUIEventStream{w: w, flusher: flusher}
}

func (s *webUIEventStream) send(event string, value any) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.Marshal(value)
	if err != nil {
		data = []byte(`{"error":"failed to encode stream event"}`)
	}
	fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, data)
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

func (s *webUIEventStream) textWriter() io.Writer {
	return webUIStreamTextWriter{s: s}
}

type webUIStreamTextWriter struct {
	s *webUIEventStream
}

func (w webUIStreamTextWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		w.s.send("text", map[string]string{"text": string(p)})
	}
	return len(p), nil
}
