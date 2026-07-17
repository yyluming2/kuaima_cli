package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

func (c *client) createImageGeneration(ctx context.Context, req imageGenerationRequest) (*responsePayload, error) {
	return c.createImageRequest(ctx, "/v1/images/generations", req, "image generation")
}

func (c *client) createImageEdit(ctx context.Context, req imageEditRequest) (*responsePayload, error) {
	if imageEditUsesMultipart(req) {
		return c.createImageEditMultipart(ctx, req)
	}
	return c.createImageRequest(ctx, "/v1/images/edits", req, "image edit")
}

func (c *client) createImageGenerationStream(ctx context.Context, req imageGenerationRequest, onImage func(imageCandidate) error) (*responsePayload, error) {
	req.Stream = true
	return c.createImageRequestStream(ctx, "/v1/images/generations", req, "image generation", onImage)
}

func (c *client) createImageEditStream(ctx context.Context, req imageEditRequest, onImage func(imageCandidate) error) (*responsePayload, error) {
	req.Stream = true
	if imageEditUsesMultipart(req) {
		return c.createImageEditMultipartStream(ctx, req, onImage)
	}
	return c.createImageRequestStream(ctx, "/v1/images/edits", req, "image edit", onImage)
}

func imageEditUsesMultipart(req imageEditRequest) bool {
	//mark 固定
	return true
	return strings.EqualFold(strings.TrimSpace(req.FileFormat), fileFormatBase64)
}

func (c *client) createImageEditMultipart(ctx context.Context, req imageEditRequest) (*responsePayload, error) {
	body, contentType, err := c.buildImageEditMultipartBody(ctx, req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/images/edits", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", contentType)
	httpReq.Header.Set("Accept", "application/json")
	c.logRequest(httpReq, nil)

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
		return nil, apiRequestError("image edit", httpResp.Status, data)
	}
	return decodeResponse(data)
}

func (c *client) createImageEditMultipartStream(ctx context.Context, req imageEditRequest, onImage func(imageCandidate) error) (*responsePayload, error) {
	body, contentType, err := c.buildImageEditMultipartBody(ctx, req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/images/edits", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", contentType)
	httpReq.Header.Set("Accept", "text/event-stream")
	c.logRequest(httpReq, nil)

	started := time.Now()
	var firstByte time.Time
	httpResp, err := c.httpClient.Do(withFirstByteTrace(httpReq, &firstByte))
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()
	c.logResponseStatus(httpResp)
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		data, _ := io.ReadAll(httpResp.Body)
		c.logResponseBody(data)
		c.logTiming(started, firstByte)
		return nil, apiRequestError("image edit", httpResp.Status, data)
	}

	var rawStream bytes.Buffer
	var outputs []responseOutput
	scanner := bufio.NewScanner(httpResp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 50*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		rawStream.WriteString(line)
		rawStream.WriteByte('\n')
		if !strings.HasPrefix(line, "data:") {
			//兼容一下json格式
			if strings.HasPrefix(line, "{") {
				for scanner.Scan() {
					line2 := scanner.Text()
					rawStream.WriteString(line2)
					rawStream.WriteByte('\n')
					line += line2
				}
			} else {
				continue
			}
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var event struct {
			Type    string `json:"type,omitempty"`
			B64JSON string `json:"b64_json,omitempty"`
			URL     string `json:"url,omitempty"`
			Data    []struct {
				B64JSON  string `json:"b64_json,omitempty"`
				URL      string `json:"url,omitempty"`
				ImageURL string `json:"image_url,omitempty"`
			} `json:"data"`
			Error *apiError `json:"error,omitempty"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		if event.Error != nil {
			return nil, apiErrorf("API error: %s", apiErrorMessage(event.Error))
		}

		candidates := imageStreamEventCandidates(event.B64JSON, "", event.URL, "", event.Data)
		for _, candidate := range candidates {
			item := ensureResponseOutput(&outputs, len(outputs))
			item.Type = "image_generation_call"
			item.Status = "completed"
			if candidate.Kind == "base64" {
				item.Result = candidate.Value
			} else {
				item.Content = []responseContent{{Type: "output_image", ImageURL: candidate.Value}}
			}
			if onImage != nil {
				if err := onImage(candidate); err != nil {
					return nil, err
				}
			}
		}
	}
	c.logResponseBody(rawStream.Bytes())
	c.logTiming(started, firstByte)
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return &responsePayload{Output: outputs, Raw: rawStream.Bytes()}, nil
}

func (c *client) buildImageEditMultipartBody(ctx context.Context, req imageEditRequest) ([]byte, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	writeField := func(key, value string) error {
		return writer.WriteField(key, value)
	}
	if err := writeField("model", req.Model); err != nil {
		return nil, "", err
	}
	if err := writeField("prompt", req.Prompt); err != nil {
		return nil, "", err
	}
	if req.Stream {
		if err := writeField("stream", "true"); err != nil {
			return nil, "", err
		}
	}
	if req.N > 0 {
		if err := writeField("n", strconv.Itoa(req.N)); err != nil {
			return nil, "", err
		}
	}
	if strings.TrimSpace(req.Size) != "" {
		if err := writeField("size", strings.TrimSpace(req.Size)); err != nil {
			return nil, "", err
		}
	}
	if strings.TrimSpace(req.Quality) != "" {
		if err := writeField("quality", strings.TrimSpace(req.Quality)); err != nil {
			return nil, "", err
		}
	}
	if strings.TrimSpace(req.OutputFormat) != "" {
		if err := writeField("output_format", strings.TrimSpace(req.OutputFormat)); err != nil {
			return nil, "", err
		}
	}
	if req.OutputCompression != nil && *req.OutputCompression >= 0 {
		if err := writeField("output_compression", strconv.Itoa(*req.OutputCompression)); err != nil {
			return nil, "", err
		}
	}
	if strings.TrimSpace(req.Background) != "" {
		if err := writeField("background", strings.TrimSpace(req.Background)); err != nil {
			return nil, "", err
		}
	}
	if strings.TrimSpace(req.Moderation) != "" {
		if err := writeField("moderation", strings.TrimSpace(req.Moderation)); err != nil {
			return nil, "", err
		}
	}
	for _, ref := range req.Images {
		if err := c.writeImageMultipartPart(ctx, writer, "image", ref); err != nil {
			return nil, "", err
		}
	}
	if req.Mask != nil {
		if err := c.writeImageMultipartPart(ctx, writer, "mask", *req.Mask); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return body.Bytes(), writer.FormDataContentType(), nil
}

func (c *client) writeImageMultipartPart(ctx context.Context, writer *multipart.Writer, fieldName string, ref imageRef) error {
	data, mediaType, filename, err := c.loadImageMultipartSource(ctx, ref)
	if err != nil {
		return err
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
		"name":     fieldName,
		"filename": filename,
	}))
	if strings.TrimSpace(mediaType) != "" {
		header.Set("Content-Type", mediaType)
	}
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	_, err = part.Write(data)
	return err
}

func (c *client) loadImageMultipartSource(ctx context.Context, ref imageRef) ([]byte, string, string, error) {
	value := strings.TrimSpace(ref.ImageURL)
	if value == "" {
		return nil, "", "", fmt.Errorf("empty image reference")
	}
	if strings.HasPrefix(value, "data:") {
		meta, data, ok := strings.Cut(value, ",")
		if !ok {
			return nil, "", "", fmt.Errorf("invalid data URL image: %q", value)
		}
		mediaType := strings.TrimPrefix(strings.TrimSuffix(meta, ";base64"), "data:")
		bytes, err := base64.StdEncoding.DecodeString(cleanBase64(data))
		if err != nil {
			return nil, "", "", err
		}
		return bytes, mediaType, filenameForImageSource("image", mediaType, value), nil
	}
	if isHTTPURL(value) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, value, nil)
		if err != nil {
			return nil, "", "", err
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, "", "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, "", "", fmt.Errorf("download image failed: %s: %s", value, resp.Status)
		}
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, "", "", err
		}
		mediaType := strings.Split(resp.Header.Get("Content-Type"), ";")[0]
		if mediaType == "" {
			mediaType = http.DetectContentType(data)
		}
		return data, mediaType, filenameForImageSource("image", mediaType, value), nil
	}
	bytes, err := base64.StdEncoding.DecodeString(cleanBase64(value))
	if err != nil {
		return nil, "", "", fmt.Errorf("decode image: %w", err)
	}
	mediaType := http.DetectContentType(bytes)
	return bytes, mediaType, filenameForImageSource("image", mediaType, value), nil
}

func filenameForImageSource(prefix, mediaType, source string) string {
	ext := extensionForMediaType(mediaType, source)
	if ext == "" {
		ext = ".png"
	}
	return prefix + ext
}

func (c *client) createImageRequest(ctx context.Context, path string, req any, operation string) (*responsePayload, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
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
	return decodeResponse(data)
}

func (c *client) createImageRequestStream(ctx context.Context, path string, req any, operation string, onImage func(imageCandidate) error) (*responsePayload, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	c.logRequest(httpReq, body)

	started := time.Now()
	var firstByte time.Time
	httpResp, err := c.httpClient.Do(withFirstByteTrace(httpReq, &firstByte))
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()
	c.logResponseStatus(httpResp)
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		data, _ := io.ReadAll(httpResp.Body)
		c.logResponseBody(data)
		c.logTiming(started, firstByte)
		return nil, apiRequestError(operation, httpResp.Status, data)
	}

	var rawStream bytes.Buffer
	var outputs []responseOutput
	scanner := bufio.NewScanner(httpResp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 50*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		rawStream.WriteString(line)
		rawStream.WriteByte('\n')
		if !strings.HasPrefix(line, "data:") {
			//兼容一下json格式
			if strings.HasPrefix(line, "{") {
				for scanner.Scan() {
					line2 := scanner.Text()
					rawStream.WriteString(line2)
					rawStream.WriteByte('\n')
					line += line2
				}
			} else {
				continue
			}
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var event struct {
			Type            string    `json:"type,omitempty"`
			B64JSON         string    `json:"b64_json,omitempty"`
			PartialImageB64 string    `json:"partial_image_b64,omitempty"`
			URL             string    `json:"url,omitempty"`
			ImageURL        string    `json:"image_url,omitempty"`
			Error           *apiError `json:"error,omitempty"`
			Data            []struct {
				B64JSON  string `json:"b64_json,omitempty"`
				URL      string `json:"url,omitempty"`
				ImageURL string `json:"image_url,omitempty"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		if event.Error != nil {
			return nil, apiErrorf("API error: %s", apiErrorMessage(event.Error))
		}

		candidates := imageStreamEventCandidates(event.B64JSON, event.PartialImageB64, event.URL, event.ImageURL, event.Data)
		for _, candidate := range candidates {
			item := ensureResponseOutput(&outputs, len(outputs))
			item.Type = "image_generation_call"
			item.Status = "completed"
			if candidate.Kind == "base64" {
				item.Result = candidate.Value
			} else {
				item.Content = []responseContent{{Type: "output_image", ImageURL: candidate.Value}}
			}
			if onImage != nil {
				if err := onImage(candidate); err != nil {
					return nil, err
				}
			}
		}
	}
	c.logResponseBody(rawStream.Bytes())
	c.logTiming(started, firstByte)
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return &responsePayload{Output: outputs, Raw: rawStream.Bytes()}, nil
}

func imageStreamEventCandidates(b64JSON, partialImageB64, url, imageURL string, data []struct {
	B64JSON  string `json:"b64_json,omitempty"`
	URL      string `json:"url,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}) []imageCandidate {
	var candidates []imageCandidate
	if strings.TrimSpace(partialImageB64) != "" {
		candidates = append(candidates, imageCandidate{Kind: "base64", Value: partialImageB64})
	}
	if strings.TrimSpace(b64JSON) != "" {
		candidates = append(candidates, imageCandidate{Kind: "base64", Value: b64JSON})
	}
	if strings.TrimSpace(imageURL) != "" {
		candidates = append(candidates, classifyImageValue(imageURL))
	}
	if strings.TrimSpace(url) != "" {
		candidates = append(candidates, classifyImageValue(url))
	}
	for _, item := range data {
		if strings.TrimSpace(item.B64JSON) != "" {
			candidates = append(candidates, imageCandidate{Kind: "base64", Value: item.B64JSON})
		}
		if strings.TrimSpace(item.ImageURL) != "" {
			candidates = append(candidates, classifyImageValue(item.ImageURL))
		}
		if strings.TrimSpace(item.URL) != "" {
			candidates = append(candidates, classifyImageValue(item.URL))
		}
	}
	return candidates
}
