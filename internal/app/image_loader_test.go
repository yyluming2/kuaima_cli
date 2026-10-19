package app

import (
	"bytes"
	"context"
	"image"
	"net/http"
	"testing"
	"time"
)

func TestLoadImageCandidateWithStandardizedRemotePNG(t *testing.T) {
	const imageURL = "https://download.xmimage2.cc.cd/r2/chatgpt2api/standardized/img-7dc42a24ab34d943ed37147b5c68cb75.png"
	httpClient := &http.Client{Timeout: 30 * time.Second}

	data, mediaType, err := loadImageCandidate(context.Background(), httpClient, imageCandidate{
		Kind:  "url",
		Value: imageURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("downloaded image is empty")
	}
	if mediaType == "" {
		t.Fatal("expected a media type")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("downloaded data is not a valid image: %v", err)
	}
	if format != "png" {
		t.Fatalf("expected PNG image, got %q", format)
	}
	if config.Width <= 0 || config.Height <= 0 {
		t.Fatalf("expected positive image dimensions, got %dx%d", config.Width, config.Height)
	}
}
