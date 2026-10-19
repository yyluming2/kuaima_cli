package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type imageCandidate struct {
	Kind      string
	Value     string
	MediaType string
}

type responseImageSaver struct {
	ctx          context.Context
	httpClient   *http.Client
	dir          string
	autoUpscale  bool
	targetWidth  int
	targetHeight int
	seen         map[string]bool
	saved        []string
}

func newResponseImageSaver(ctx context.Context, httpClient *http.Client, dir string, autoUpscale bool, targetSize string) *responseImageSaver {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	targetWidth, targetHeight := parseImageDimensions(targetSize)
	if !autoUpscale {
		targetWidth, targetHeight = 0, 0
	}
	return &responseImageSaver{
		ctx:          ctx,
		httpClient:   httpClient,
		dir:          dir,
		autoUpscale:  autoUpscale,
		targetWidth:  targetWidth,
		targetHeight: targetHeight,
		seen:         map[string]bool{},
	}
}

func saveImagesFromResponse(ctx context.Context, httpClient *http.Client, resp *responsePayload, dir string, autoUpscale bool, targetSize string) ([]string, error) {
	return newResponseImageSaver(ctx, httpClient, dir, autoUpscale, targetSize).saveResponse(resp)
}

func (s *responseImageSaver) saveResponse(resp *responsePayload) ([]string, error) {
	return s.saveCandidates(imageCandidates(resp))
}

func (s *responseImageSaver) saveCandidates(candidates []imageCandidate) ([]string, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	var saved []string
	for _, candidate := range candidates {
		path, err := s.saveCandidate(candidate)
		if err != nil {
			// Some providers include an empty/placeholder result alongside the
			// actual image. Ignore malformed base64 candidates so PPT retries can
			// still use a valid image returned later in the response.
			if strings.Contains(err.Error(), "decode image:") {
				continue
			}
			return saved, err
		}
		if path != "" {
			saved = append(saved, path)
		}
	}
	return saved, nil
}

func (s *responseImageSaver) saveCandidate(candidate imageCandidate) (string, error) {
	if s == nil {
		return "", nil
	}
	if strings.TrimSpace(candidate.Value) == "" {
		return "", nil
	}
	if s.seen[candidate.Value] {
		return "", nil
	}
	s.seen[candidate.Value] = true

	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return "", err
	}
	data, mediaType, err := loadImageCandidate(s.ctx, s.httpClient, candidate)
	if err != nil {
		return "", err
	}
	data, mediaType, err = s.upscaleImageIfNeeded(data, mediaType)
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("response-image-%02d%s", len(s.saved)+1, extensionForMediaType(mediaType, candidate.Value))
	path := filepath.Join(s.dir, name)
	path, err = writeFileUnique(path, data, 0644)
	if err != nil {
		return "", err
	}
	s.saved = append(s.saved, path)
	return path, nil
}

func (s *responseImageSaver) upscaleImageIfNeeded(data []byte, mediaType string) ([]byte, string, error) {
	if s == nil || !s.autoUpscale || s.targetWidth <= 0 || s.targetHeight <= 0 {
		return data, mediaType, nil
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return data, mediaType, nil
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return data, mediaType, nil
	}
	if cfg.Width >= s.targetWidth || cfg.Height >= s.targetHeight {
		return data, mediaType, nil
	}
	if format != "png" && format != "jpeg" {
		return data, mediaType, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	scale := math.Min(float64(s.targetWidth)/float64(cfg.Width), float64(s.targetHeight)/float64(cfg.Height))
	if scale <= 1 {
		return data, mediaType, nil
	}
	newWidth := int(math.Round(float64(cfg.Width) * scale))
	newHeight := int(math.Round(float64(cfg.Height) * scale))
	if newWidth <= cfg.Width && newHeight <= cfg.Height {
		return data, mediaType, nil
	}
	resized := resizeBilinear(img, newWidth, newHeight)
	var buf bytes.Buffer
	switch format {
	case "png":
		if err := png.Encode(&buf, resized); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), "image/png", nil
	case "jpeg":
		if err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: 95}); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), "image/jpeg", nil
	default:
		return data, mediaType, nil
	}
}

func writeFileUnique(path string, data []byte, perm os.FileMode) (string, error) {
	for i := 0; ; i++ {
		candidate := numberedPath(path, i)
		file, err := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := file.Write(data); err != nil {
			closeErr := file.Close()
			if closeErr != nil {
				return "", fmt.Errorf("%w; close file: %v", err, closeErr)
			}
			return "", err
		}
		if err := file.Close(); err != nil {
			return "", err
		}
		return candidate, nil
	}
}

func numberedPath(path string, index int) string {
	if index == 0 {
		return path
	}
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	return fmt.Sprintf("%s-%02d%s", stem, index, ext)
}
