package app

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf16"
)

func testPPTDOCX(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	file, err := archive.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.Write([]byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>产品发布</w:t></w:r></w:p><w:tbl><w:tr><w:tc><w:p><w:r><w:t>收入</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>100 万元</w:t></w:r></w:p></w:tc></w:tr></w:tbl></w:body></w:document>`))
	if err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func testPPTPDF(texts ...string) []byte {
	var data bytes.Buffer
	data.WriteString("%PDF-1.4\n")
	kids := make([]string, len(texts))
	for i := range texts {
		kids[i] = fmt.Sprintf("%d 0 R", 4+i*2)
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(texts)),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	for i, text := range texts {
		stream := "BT /F1 12 Tf 72 720 Td (" + text + ") Tj ET"
		objects = append(objects, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", 5+i*2), fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	}
	offsets := []int{0}
	for i, obj := range objects {
		offsets = append(offsets, data.Len())
		fmt.Fprintf(&data, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := data.Len()
	fmt.Fprintf(&data, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&data, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&data, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return data.Bytes()
}

func TestPPTDocumentExtraction(t *testing.T) {
	for _, test := range []struct {
		name string
		data []byte
		want string
	}{
		{"资料.docx", testPPTDOCX(t), "产品发布"},
		{"reference.pdf", testPPTPDF("Product launch revenue 100"), "Product launch revenue 100"},
	} {
		t.Run(test.name, func(t *testing.T) {
			text, err := extractPPTDocument(test.name, test.data)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(text, test.want) {
				t.Fatalf("text = %q", text)
			}
		})
	}
	text, err := extractPPTDocument("table.docx", testPPTDOCX(t))
	if err != nil || !strings.Contains(text, "100 万元") {
		t.Fatalf("missing table: %q, %v", text, err)
	}
	for _, test := range []struct {
		name string
		data []byte
	}{
		{"bad.pdf", []byte("invalid")}, {"bad.docx", []byte("invalid")}, {"bad.doc", []byte("invalid")}, {"scan.pdf", testPPTPDF("")},
	} {
		if _, err := extractPPTDocument(test.name, test.data); err == nil {
			t.Fatalf("accepted %s", test.name)
		}
	}
}

func TestPPTDOCTextPieces(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		t.Run(fmt.Sprint(compressed), func(t *testing.T) {
			word := make([]byte, 2048)
			binary.LittleEndian.PutUint16(word, 0xa5ec)
			binary.LittleEndian.PutUint16(word[10:], 0x200)
			binary.LittleEndian.PutUint16(word[32:], 14)
			binary.LittleEndian.PutUint16(word[62:], 22)
			binary.LittleEndian.PutUint16(word[152:], 34)
			value := "产品发布\r收入 100"
			location := 1024
			fc := uint32(location)
			chars := utf16.Encode([]rune(value))
			binary.LittleEndian.PutUint32(word[76:], uint32(len(chars)))
			if compressed {
				value = "Product launch\rRevenue 100"
				binary.LittleEndian.PutUint32(word[76:], uint32(len(value)))
				copy(word[location:], value)
				fc = uint32(location*2) | 0x40000000
			} else {
				for i, c := range chars {
					binary.LittleEndian.PutUint16(word[location+i*2:], c)
				}
			}
			table := make([]byte, 21)
			table[0] = 2
			binary.LittleEndian.PutUint32(table[1:], 16)
			binary.LittleEndian.PutUint32(table[9:], binary.LittleEndian.Uint32(word[76:]))
			binary.LittleEndian.PutUint32(table[15:], fc)
			binary.LittleEndian.PutUint32(word[154+33*8+4:], uint32(len(table)))
			text, err := extractPPTDOCStreams(map[string][]byte{"WordDocument": word, "1Table": table})
			if err != nil {
				t.Fatal(err)
			}
			if text != value {
				t.Fatalf("text = %q, want %q", text, value)
			}
			binary.LittleEndian.PutUint32(table[15:], 0x3fffffff)
			if _, err := extractPPTDOCStreams(map[string][]byte{"WordDocument": word, "1Table": table}); err == nil {
				t.Fatal("accepted invalid piece offset")
			}
		})
	}
}

func TestPPTReferenceParseEndpoint(t *testing.T) {
	s := &webUIServer{}
	request := httptest.NewRequest("POST", "/api/ppt/reference/parse?name=reference.docx", bytes.NewReader(testPPTDOCX(t)))
	recorder := httptest.NewRecorder()
	s.handlePPTReferenceParse(recorder, request)
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), "产品发布") {
		t.Fatalf("response: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	s.handlePPTReferenceParse(recorder, httptest.NewRequest("GET", "/api/ppt/reference/parse", nil))
	if recorder.Code != 405 {
		t.Fatal(recorder.Code)
	}
}

func TestPPTPDFMixedPages(t *testing.T) {
	reference, err := parsePPTPDFReference(testPPTPDF("Revenue 100", "", "Case study", ""))
	if err != nil {
		t.Fatal(err)
	}
	if reference.PageCount != 4 || len(reference.ImagePages) != 2 || reference.ImagePages[0] != 2 || reference.ImagePages[1] != 4 {
		t.Fatalf("wrong scanned pages: %#v", reference)
	}
	if !strings.Contains(reference.Text, "Revenue 100") || !strings.Contains(reference.Text, "## 第 3 页") || strings.Contains(reference.Text, "## 第 2 页") {
		t.Fatalf("incorrect text: %q", reference.Text)
	}
}

func TestPPTScannedPDFParseEndpoint(t *testing.T) {
	s := &webUIServer{}
	recorder := httptest.NewRecorder()
	s.handlePPTReferenceParse(recorder, httptest.NewRequest("POST", "/api/ppt/reference/parse?name=scan.pdf", bytes.NewReader(testPPTPDF("", ""))))
	if recorder.Code != 200 {
		t.Fatalf("scanned PDF rejected: %s", recorder.Body.String())
	}
	var reference pptPDFReference
	if err := json.Unmarshal(recorder.Body.Bytes(), &reference); err != nil {
		t.Fatal(err)
	}
	if reference.Text != "" || len(reference.ImagePages) != 2 {
		t.Fatalf("unexpected result: %#v", reference)
	}
}

func TestPPTPDFRendererAssets(t *testing.T) {
	for _, name := range []string{"webui/pdf.min.js", "webui/pdf.worker.min.js"} {
		data, err := webUIAssets.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) < 100000 || !bytes.Contains(data, []byte("3.11.174")) {
			t.Fatalf("missing PDF.js asset %s", name)
		}
	}
}
