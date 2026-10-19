package app

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestPPTXExportDownload(t *testing.T) {
	s := &webUIServer{}
	server := httptest.NewServer(http.HandlerFunc(s.handlePPTXExport))
	defer server.Close()
	response, err := http.Post(server.URL, "application/json", strings.NewReader(`{"title":"test","width":1280,"height":720,"slides":[{"id":"s1","elements":[]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("export status %d: %s", response.StatusCode, data)
	}
	if response.Header.Get("Content-Length") != strconv.Itoa(len(data)) || len(response.TransferEncoding) != 0 {
		t.Fatalf("download must have an exact length without chunked encoding: %v", response.Header)
	}
	if response.Header.Get("Content-Disposition") != `attachment; filename="presentation.pptx"` {
		t.Fatalf("unexpected download disposition: %v", response.Header)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("invalid PPTX: %v", err)
	}
	if _, err := archive.Open("ppt/presentation.xml"); err != nil {
		t.Fatalf("missing presentation: %v", err)
	}
}

func TestPPTXExportJSONTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc((&webUIServer{}).handlePPTXExport))
	defer server.Close()
	request, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(`{"title":"test","width":1280,"height":720,"slides":[{"id":"s1","elements":[]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(response.Header.Get("Content-Type"), "application/json") || response.Header.Get("Content-Disposition") != "" {
		t.Fatalf("unexpected JSON transport response: %d %v", response.StatusCode, response.Header)
	}
	var file struct{ Filename, ContentType, Data string }
	if err := json.NewDecoder(response.Body).Decode(&file); err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(file.Data)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Open("ppt/presentation.xml"); err != nil {
		t.Fatal(err)
	}
	if file.Filename != "presentation.pptx" || file.ContentType != "application/vnd.openxmlformats-officedocument.presentationml.presentation" {
		t.Fatalf("unexpected file metadata: %s %s", file.Filename, file.ContentType)
	}
}
