package app

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/ledongthuc/pdf"
	"github.com/richardlehane/mscfb"
	"golang.org/x/text/encoding/charmap"
)

const pptDocumentTextLimit = 2 << 20

func (s *webUIServer) handlePPTReferenceParse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, (10<<20)+1))
	if err != nil || len(data) > 10<<20 {
		writeError(w, 400, "文件读取失败或超过 10 MB")
		return
	}
	if strings.EqualFold(filepath.Ext(r.URL.Query().Get("name")), ".pdf") {
		reference, err := parsePPTPDFReference(data)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, reference)
		return
	}
	text, err := extractPPTDocument(r.URL.Query().Get("name"), data)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"text": text})
}

func extractPPTDocument(name string, data []byte) (text string, err error) {
	// Malformed PDFs and compound documents may panic inside third party decoders.
	defer func() {
		if recover() != nil {
			text = ""
			err = fmt.Errorf("文档损坏或格式不受支持")
		}
	}()
	switch strings.ToLower(filepath.Ext(name)) {
	case ".docx":
		text, err = extractPPTDOCX(data)
	case ".doc":
		text, err = extractPPTDOC(data)
	case ".pdf":
		text, err = extractPPTPDF(data)
	default:
		return "", fmt.Errorf("不支持的文档格式")
	}
	if err != nil {
		return "", err
	}
	text = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\r' {
			return '\n'
		}
		if r == '\x07' {
			return '\t'
		}
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, text))
	if text == "" {
		return "", fmt.Errorf("未提取到文字；扫描 PDF 或图片文档请先进行 OCR")
	}
	if len(text) > pptDocumentTextLimit {
		return "", fmt.Errorf("文档文字超过 2 MB，请拆分文件")
	}
	return text, nil
}

func extractPPTDOCX(data []byte) (string, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("无效的 DOCX：%w", err)
	}
	for _, file := range archive.File {
		if file.Name != "word/document.xml" {
			continue
		}
		if file.UncompressedSize64 > 16<<20 {
			return "", fmt.Errorf("Word 文档内容过大")
		}
		stream, err := file.Open()
		if err != nil {
			return "", err
		}
		defer stream.Close()
		decoder := xml.NewDecoder(io.LimitReader(stream, (16<<20)+1))
		var out strings.Builder
		inText := false
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", err
			}
			switch t := token.(type) {
			case xml.StartElement:
				switch t.Name.Local {
				case "t":
					inText = true
				case "tab":
					out.WriteByte('\t')
				case "br", "cr":
					out.WriteByte('\n')
				}
			case xml.EndElement:
				switch t.Name.Local {
				case "t":
					inText = false
				case "p", "tr":
					out.WriteByte('\n')
				case "tc":
					out.WriteByte('\t')
				}
			case xml.CharData:
				if inText {
					out.Write(t)
				}
			}
			if out.Len() > pptDocumentTextLimit {
				return "", fmt.Errorf("Word 文档文字过多")
			}
		}
		return out.String(), nil
	}
	return "", fmt.Errorf("DOCX 缺少正文内容")
}

type pptPDFReference struct {
	Text       string `json:"text"`
	ImagePages []int  `json:"imagePages,omitempty"`
	PageCount  int    `json:"pageCount"`
}

func extractPPTPDF(data []byte) (string, error) {
	reference, err := parsePPTPDFReference(data)
	return reference.Text, err
}

func parsePPTPDFReference(data []byte) (reference pptPDFReference, err error) {
	defer func() {
		if recover() != nil {
			reference = pptPDFReference{}
			err = fmt.Errorf("PDF 损坏或格式不受支持")
		}
	}()
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return reference, fmt.Errorf("无法读取 PDF（可能损坏或加密）：%w", err)
	}
	reference.PageCount = reader.NumPage()
	if reference.PageCount < 1 || reference.PageCount > 500 {
		return reference, fmt.Errorf("PDF 页数须为 1 到 500 页，请拆分文件")
	}
	var out strings.Builder
	for i := 1; i <= reference.PageCount; i++ {
		text, readErr := reader.Page(i).GetPlainText(nil)
		// A page without usable text is rendered locally, including unsupported encodings.
		if readErr != nil || strings.TrimSpace(text) == "" {
			reference.ImagePages = append(reference.ImagePages, i)
			continue
		}
		fmt.Fprintf(&out, "\n\n## 第 %d 页\n\n%s", i, text)
		if out.Len() > pptDocumentTextLimit {
			return reference, fmt.Errorf("PDF 文档文字过多")
		}
	}
	reference.Text = strings.TrimSpace(out.String())
	return reference, nil
}

func extractPPTDOC(data []byte) (string, error) {
	reader, err := mscfb.New(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("无效的 DOC：%w", err)
	}
	streams := map[string][]byte{}
	for _, file := range reader.File {
		if file.Name != "WordDocument" && file.Name != "0Table" && file.Name != "1Table" {
			continue
		}
		if file.Size > 16<<20 {
			return "", fmt.Errorf("DOC 数据流过大")
		}
		value, err := io.ReadAll(io.LimitReader(file, (16<<20)+1))
		if err != nil {
			return "", err
		}
		streams[file.Name] = value
	}
	return extractPPTDOCStreams(streams)
}

func extractPPTDOCStreams(streams map[string][]byte) (string, error) {
	word := streams["WordDocument"]
	if len(word) < 34 || binary.LittleEndian.Uint16(word) != 0xa5ec {
		return "", fmt.Errorf("DOC 正文无效")
	}
	flags := binary.LittleEndian.Uint16(word[10:])
	if flags&0x8100 != 0 {
		return "", fmt.Errorf("不支持加密的 DOC")
	}
	tableName := "0Table"
	if flags&0x200 != 0 {
		tableName = "1Table"
	}
	table := streams[tableName]
	// FIB has variable-length arrays; locate fcClx/lcbClx (pair 33).
	offset := 34 + int(binary.LittleEndian.Uint16(word[32:]))*2
	if offset+2 > len(word) {
		return "", fmt.Errorf("DOC 文件头不完整")
	}
	lwCount := int(binary.LittleEndian.Uint16(word[offset:]))
	lwStart := offset + 2
	if lwCount < 4 || lwStart+lwCount*4+2 > len(word) {
		return "", fmt.Errorf("DOC 文件头不完整")
	}
	mainLength := binary.LittleEndian.Uint32(word[lwStart+12:])
	offset = lwStart + lwCount*4
	if binary.LittleEndian.Uint16(word[offset:]) <= 33 || offset+2+34*8 > len(word) {
		return "", fmt.Errorf("不支持此旧版 DOC，请转存为 DOCX")
	}
	pair := offset + 2 + 33*8
	start := uint64(binary.LittleEndian.Uint32(word[pair:]))
	size := uint64(binary.LittleEndian.Uint32(word[pair+4:]))
	if start+size > uint64(len(table)) || size == 0 {
		return "", fmt.Errorf("DOC 文本片段表无效")
	}
	clx := table[start : start+size]
	for len(clx) > 0 && clx[0] == 1 {
		if len(clx) < 3 {
			return "", fmt.Errorf("DOC 文本片段表损坏")
		}
		skip := 3 + int(binary.LittleEndian.Uint16(clx[1:]))
		if skip > len(clx) {
			return "", fmt.Errorf("DOC 文本片段表损坏")
		}
		clx = clx[skip:]
	}
	if len(clx) < 5 || clx[0] != 2 {
		return "", fmt.Errorf("DOC 缺少文本片段表")
	}
	length := int(binary.LittleEndian.Uint32(clx[1:]))
	if length < 4 || (length-4)%12 != 0 || length > len(clx)-5 {
		return "", fmt.Errorf("DOC 文本片段表损坏")
	}
	pieces := clx[5 : 5+length]
	count := (length - 4) / 12
	var out strings.Builder
	for i := 0; i < count; i++ {
		cpStart := binary.LittleEndian.Uint32(pieces[i*4:])
		cpEnd := binary.LittleEndian.Uint32(pieces[(i+1)*4:])
		if cpStart >= mainLength {
			break
		}
		if cpEnd > mainLength {
			cpEnd = mainLength
		}
		if cpEnd < cpStart {
			return "", fmt.Errorf("DOC 字符范围无效")
		}
		chars := uint64(cpEnd - cpStart)
		fc := binary.LittleEndian.Uint32(pieces[(count+1)*4+i*8+2:])
		compressed := fc&0x40000000 != 0
		location := uint64(fc & 0x3fffffff)
		byteCount := chars * 2
		if compressed {
			location /= 2
			byteCount = chars
		}
		if location+byteCount > uint64(len(word)) {
			return "", fmt.Errorf("DOC 文本位置无效")
		}
		raw := word[location : location+byteCount]
		if compressed {
			decoded, err := charmap.Windows1252.NewDecoder().Bytes(raw)
			if err != nil {
				return "", err
			}
			out.Write(decoded)
		} else {
			values := make([]uint16, len(raw)/2)
			for j := range values {
				values[j] = binary.LittleEndian.Uint16(raw[j*2:])
			}
			out.WriteString(string(utf16.Decode(values)))
		}
		if out.Len() > pptDocumentTextLimit {
			return "", fmt.Errorf("DOC 文档文字过多")
		}
	}
	return out.String(), nil
}
