package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

const remoteImageDownloadRetries = 3

func loadImageCandidate(ctx context.Context, httpClient *http.Client, candidate imageCandidate) ([]byte, string, error) {
	switch candidate.Kind {
	case "url":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, candidate.Value, nil)
		if err != nil {
			return nil, "", err
		}
		var lastErr error
		for attempt := 0; attempt <= remoteImageDownloadRetries; attempt++ {
			if attempt > 0 {
				delay := time.Duration(250*(1<<(attempt-1))) * time.Millisecond
				select {
				case <-ctx.Done():
					return nil, "", ctx.Err()
				case <-time.After(delay):
				}
			}
			resp, requestErr := httpClient.Do(req)
			if requestErr == nil {
				data, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 && readErr == nil && len(data) > 0 {
					mediaType := strings.Split(resp.Header.Get("Content-Type"), ";")[0]
					if mediaType == "" {
						mediaType = http.DetectContentType(data)
					}
					if attempt > 0 {
						log.Printf("remote image download recovered after retry attempt=%d url=%s", attempt+1, candidate.Value)
					}
					return data, mediaType, nil
				}
				if resp.StatusCode < 500 && resp.StatusCode >= 400 && readErr == nil {
					return nil, "", fmt.Errorf("download image failed: %s: %s", candidate.Value, resp.Status)
				}
				if readErr != nil {
					lastErr = readErr
				} else {
					lastErr = fmt.Errorf("download image failed: %s", resp.Status)
				}
			} else {
				lastErr = requestErr
			}
			if attempt < remoteImageDownloadRetries {
				log.Printf("remote image download failed, retrying attempt=%d/%d url=%s error=%v", attempt+1, remoteImageDownloadRetries, candidate.Value, lastErr)
			}
		}
		return nil, "", fmt.Errorf("download image failed after %d retries: %s: %w", remoteImageDownloadRetries, candidate.Value, lastErr)
	case "data":
		meta, data, ok := strings.Cut(candidate.Value, ",")
		if !ok {
			return nil, "", errors.New("invalid data URL image result")
		}
		mediaType := strings.TrimPrefix(strings.TrimSuffix(meta, ";base64"), "data:")
		bytes, err := base64.StdEncoding.DecodeString(cleanBase64(data))
		return bytes, mediaType, err
	default:
		bytes, err := base64.StdEncoding.DecodeString(cleanBase64(candidate.Value))
		if err != nil {
			return nil, "", fmt.Errorf("decode image: %w", err)
		}
		mediaType := candidate.MediaType
		if mediaType == "" {
			mediaType = http.DetectContentType(bytes)
		}
		return bytes, mediaType, nil
	}
}

func extensionForMediaType(mediaType, source string) string {
	if isHTTPURL(source) {
		if ext := filepath.Ext(pathFromURL(source)); ext != "" {
			return ext
		}
	}
	if mediaType != "" {
		if exts, _ := mime.ExtensionsByType(mediaType); len(exts) > 0 {
			return exts[0]
		}
	}
	return ".png"
}

func pathFromURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	return parsed.Path
}
