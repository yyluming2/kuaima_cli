package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

func (c *client) createResponseStream(ctx context.Context, req responseRequest, w io.Writer) (*responsePayload, error) {
	return c.createResponseStreamWithImages(ctx, req, w, nil)
}

func (c *client) createResponseStreamWithImages(ctx context.Context, req responseRequest, w io.Writer, onImage func(imageCandidate) error) (*responsePayload, error) {
	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := c.newRequest(ctx, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "text/event-stream")
	c.logRequest(httpReq, body)

	started := time.Now()
	var firstByte time.Time
	httpReq = withFirstByteTrace(httpReq, &firstByte)
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()
	c.logResponseStatus(httpResp)

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		data, _ := io.ReadAll(httpResp.Body)
		c.logResponseBody(data)
		c.logTiming(started, firstByte)
		return nil, apiRequestError("API request", httpResp.Status, data)
	}

	var completed json.RawMessage
	var text bytes.Buffer
	var outputs []responseOutput
	var rawStream bytes.Buffer
	var firstToken time.Time
	scanner := bufio.NewScanner(httpResp.Body)
	//单张4k图base64比较大
	scanner.Buffer(make([]byte, 0, 64*1024), 50*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		rawStream.WriteString(line)
		rawStream.WriteByte('\n')
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var event struct {
			Type            string          `json:"type"`
			Delta           string          `json:"delta"`
			Text            string          `json:"text"`
			Response        json.RawMessage `json:"response"`
			Error           *apiError       `json:"error"`
			OutputIndex     int             `json:"output_index"`
			Item            json.RawMessage `json:"item"`
			PartialImageB64 string          `json:"partial_image_b64"`
			B64JSON         string          `json:"b64_json"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			//return nil, apiErrorf("unmarshal error: %s, %d", err, len(data))
			continue
		}
		if event.Error != nil {
			return nil, apiErrorf("API error: %s", apiErrorMessage(event.Error))
		}
		switch event.Type {
		case "response.output_text.delta", "response.refusal.delta":
			if event.Delta != "" {
				if firstToken.IsZero() {
					firstToken = time.Now()
				}
				fmt.Fprint(w, event.Delta)
				text.WriteString(event.Delta)
			}
		case "response.output_item.added", "response.output_item.done":
			if len(event.Item) > 0 {
				var item responseOutput
				if err := json.Unmarshal(event.Item, &item); err == nil {
					setResponseOutput(&outputs, event.OutputIndex, item)
					if onImage != nil && item.Type == "image_generation_call" && strings.TrimSpace(item.Result) != "" {
						if err := onImage(imageCandidate{Kind: "base64", Value: item.Result}); err != nil {
							return nil, err
						}
					}
				}
			}
		case "response.image_generation_call.partial_image":
			image := event.PartialImageB64
			if image == "" {
				image = event.B64JSON
			}
			if image != "" {
				item := ensureResponseOutput(&outputs, event.OutputIndex)
				if item.Type == "" {
					item.Type = "image_generation_call"
				}
				if item.Status != "completed" {
					item.Result = image
				}
				if onImage != nil {
					if err := onImage(imageCandidate{Kind: "base64", Value: image}); err != nil {
						return nil, err
					}
				}
			}
		case "response.completed":
			completed = event.Response
		case "response.failed":
			if event.Response != nil {
				resp, err := decodeResponse(event.Response)
				if err == nil && resp.Error != nil {
					return nil, apiErrorf("API error: %s", apiErrorMessage(resp.Error))
				}
			}
			return nil, errors.New("API stream failed")
		}
	}
	c.logResponseBody(rawStream.Bytes())
	if firstToken.IsZero() {
		firstToken = firstByte
	}
	c.logTiming(started, firstToken)
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if len(completed) > 0 {
		resp, err := decodeResponse(completed)
		if err != nil {
			return nil, err
		}
		if len(resp.Output) == 0 && len(outputs) > 0 {
			resp.Output = outputs
		}
		if resp.OutputText == "" && text.Len() > 0 {
			resp.OutputText = text.String()
		}
		return resp, nil
	}
	return &responsePayload{OutputText: text.String(), Output: outputs, Raw: rawStream.Bytes()}, nil
}

func setResponseOutput(outputs *[]responseOutput, index int, item responseOutput) {
	if index < 0 {
		index = len(*outputs)
	}
	for len(*outputs) <= index {
		*outputs = append(*outputs, responseOutput{})
	}
	if item.Result == "" && (*outputs)[index].Result != "" {
		item.Result = (*outputs)[index].Result
	}
	(*outputs)[index] = item
}

func ensureResponseOutput(outputs *[]responseOutput, index int) *responseOutput {
	if index < 0 {
		index = len(*outputs)
	}
	for len(*outputs) <= index {
		*outputs = append(*outputs, responseOutput{})
	}
	return &(*outputs)[index]
}
