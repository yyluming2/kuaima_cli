package app

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPPTPromptsSeparateContentFromVisualStyle(t *testing.T) {
	s := &webUIServer{}
	request := httptest.NewRequest("POST", "/api/ppt/prompts", strings.NewReader(`{"brief":"需要保留的营业收入与客户案例","style":"现代商务"}`))
	recorder := httptest.NewRecorder()
	s.handlePPTPrompts(recorder, request)
	var result map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result["unifiedPrompt"], "营业收入") || strings.Contains(result["unifiedPrompt"], "主题内容") {
		t.Fatal("content leaked into visual prompt")
	}
	if !strings.Contains(result["unifiedPrompt"], "现代商务") || result["outlinePrompt"] == "" {
		t.Fatal("missing visual style or outline instructions")
	}
}

func TestPPTReferenceFiles(t *testing.T) {
	content, err := pptReferenceFileContent([]pptReferenceFile{{Name: "reference.docx", Data: "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(testPPTDOCX(t))}})
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != 1 || content[0].Type != "input_text" || !strings.Contains(content[0].Text, "产品发布") {
		t.Fatalf("missing extracted text: %#v", content)
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "file_data") || strings.Contains(string(encoded), "base64") {
		t.Fatalf("binary document leaked into model payload: %s", encoded)
	}
	for _, file := range []pptReferenceFile{
		{Name: "reference.exe", Data: "data:application/octet-stream;base64,aGVsbG8="},
		{Name: "reference.pdf", Data: "hello"},
		{Name: "reference.docx", Data: "data:application/octet-stream;base64,invalid!"},
		{Name: "reference.doc", Data: "data:application/octet-stream;base64,"},
	} {
		if _, err := pptReferenceFileContent([]pptReferenceFile{file}); err == nil {
			t.Fatalf("accepted invalid file %q", file.Name)
		}
	}
}
