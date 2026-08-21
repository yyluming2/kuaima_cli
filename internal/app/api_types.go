package app

import (
	"encoding/json"
	"io"
	"net/http"
)

type client struct {
	baseURL    string
	ossURL     string
	apiKey     string
	verbose    bool
	logWriter  io.Writer
	logCloser  io.Closer
	httpClient *http.Client
}

var defaultImageReasoning = map[string]any{
	"effort":  "medium",
	"summary": "auto",
}

type responseRequest struct {
	Model      string `json:"model"`
	Input      any    `json:"input"`
	Tools      any    `json:"tools,omitempty"`
	ToolChoice any    `json:"tool_choice,omitempty"`
	Reasoning  any    `json:"reasoning,omitempty"`
	Stream     bool   `json:"stream,omitempty"`
}

type imageGenerationRequest struct {
	Model             string `json:"model"`
	Prompt            string `json:"prompt"`
	Stream            bool   `json:"stream,omitempty"`
	N                 int    `json:"n,omitempty"`
	Size              string `json:"size,omitempty"`
	Quality           string `json:"quality,omitempty"`
	OutputFormat      string `json:"output_format,omitempty"`
	OutputCompression *int   `json:"output_compression,omitempty"`
	Background        string `json:"background,omitempty"`
	Moderation        string `json:"moderation,omitempty"`
}

type imageEditRequest struct {
	Model             string     `json:"model"`
	Prompt            string     `json:"prompt"`
	Stream            bool       `json:"stream,omitempty"`
	Images            []imageRef `json:"images"`
	Mask              *imageRef  `json:"mask,omitempty"`
	FileFormat        string     `json:"-"`
	N                 int        `json:"n,omitempty"`
	Size              string     `json:"size,omitempty"`
	Quality           string     `json:"quality,omitempty"`
	OutputFormat      string     `json:"output_format,omitempty"`
	OutputCompression *int       `json:"output_compression,omitempty"`
	Background        string     `json:"background,omitempty"`
	Moderation        string     `json:"moderation,omitempty"`
}

type imageRef struct {
	FileID   string `json:"file_id,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

type videoGenerationRequest struct {
	Model     string         `json:"model"`
	Mode      string         `json:"mode"`
	Prompt    string         `json:"prompt"`
	Images    []string       `json:"images,omitempty"`
	Image     string         `json:"image,omitempty"`
	LastFrame string         `json:"last_frame,omitempty"`
	Video     string         `json:"video,omitempty"`
	Duration  int            `json:"duration,omitempty"`
	Size      string         `json:"size,omitempty"`
	Ratio     string         `json:"ratio,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type videoGenerationTask struct {
	TaskID   string         `json:"task_id"`
	Status   string         `json:"status"`
	URL      string         `json:"url,omitempty"`
	Format   string         `json:"format,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Error    *apiError      `json:"error,omitempty"`
}

type responsePayload struct {
	ID         string           `json:"id"`
	OutputText string           `json:"output_text"`
	Output     []responseOutput `json:"output"`
	Error      *apiError        `json:"error"`
	Raw        json.RawMessage  `json:"-"`
}

type responseOutput struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	Name      string            `json:"name"`
	CallID    string            `json:"call_id"`
	Arguments string            `json:"arguments"`
	Role      string            `json:"role"`
	Content   []responseContent `json:"content"`
	Result    string            `json:"result"`
	Status    string            `json:"status"`
}

type responseContent struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	ImageURL string `json:"image_url"`
}

type apiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code"`
}
