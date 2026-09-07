package pptximport

// Reader is the protocol-safe OOXML reader. It intentionally uses only the
// standard library so imported presentations remain independent of licenses.
import (
	"archive/zip"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"kuaima_cli/internal/ppt"
	"path/filepath"
	"strings"
)

func parseRels(data []byte) map[string]string {
	type rel struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
	}
	var d struct {
		R []rel `xml:"Relationship"`
	}
	_ = xml.Unmarshal(data, &d)
	out := map[string]string{}
	for _, r := range d.R {
		out[r.ID] = r.Target
	}
	return out
}

// Read validates a PPTX zip and returns an editable presentation. Detailed
// element extraction is added incrementally; unknown OOXML is preserved by
// callers as diagnostics instead of failing the whole import.
func Read(r io.ReaderAt, size int64) (ppt.Presentation, error) {
	z, err := zip.NewReader(r, size)
	if err != nil {
		return ppt.Presentation{}, err
	}
	found := false
	for _, f := range z.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			found = true
			break
		}
	}
	if !found {
		return ppt.Presentation{}, fmt.Errorf("pptx contains no slides")
	}
	p := ppt.New16x9("Imported presentation")
	// Read page size from presentation.xml (EMU units).
	for _, f := range z.File {
		if f.Name == "ppt/presentation.xml" {
			b, _ := f.Open()
			d, _ := io.ReadAll(b)
			b.Close()
			var v struct {
				S struct {
					Cx int64 `xml:"cx,attr"`
					Cy int64 `xml:"cy,attr"`
				} `xml:"sldSz"`
			}
			if xml.Unmarshal(d, &v) == nil && v.S.Cx > 0 && v.S.Cy > 0 {
				p.Width = float64(v.S.Cx) / 914400 * 96
				p.Height = float64(v.S.Cy) / 914400 * 96
			}
		}
	}
	for _, f := range z.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			b, e := f.Open()
			if e != nil {
				continue
			}
			data, _ := io.ReadAll(b)
			b.Close()
			var doc struct {
				Texts []string `xml:"sp>txBody>p>r>t"`
			}
			_ = xml.Unmarshal(data, &doc)
			s := ppt.Slide{ID: "slide-" + strings.TrimSuffix(filepath.Base(f.Name), filepath.Ext(f.Name))[5:], Layout: "imported"}
			for i, t := range doc.Texts {
				s.Elements = append(s.Elements, ppt.Element{ID: fmt.Sprintf("text-%d", i+1), Type: "text", X: 80, Y: 80 + float64(i*60), Width: 1120, Height: 50, Content: t})
			}
			// Import non-text shapes as editable shape elements. Geometry is
			// intentionally conservative when the source omits transform data.
			var shapes []struct {
				Nv struct {
					CnvPr struct {
						ID   string `xml:"id,attr"`
						Name string `xml:"name,attr"`
					} `xml:"cNvPr"`
				} `xml:"nvSpPr"`
			}
			_ = xml.Unmarshal(data, &struct {
				Shapes *[]struct {
					Nv struct {
						CnvPr struct {
							ID   string `xml:"id,attr"`
							Name string `xml:"name,attr"`
						} `xml:"cNvPr"`
					} `xml:"nvSpPr"`
				} `xml:"sp"`
			}{Shapes: &shapes})
			for i, sh := range shapes {
				if sh.Nv.CnvPr.Name != "" {
					s.Elements = append(s.Elements, ppt.Element{ID: fmt.Sprintf("shape-%d", i+1), Type: "shape", X: 80, Y: 220 + float64(i*40), Width: 240, Height: 80, Content: sh.Nv.CnvPr.Name})
				}
			}
			// Extract pictures and resolve their slide relationships to media parts.
			var rels map[string]string
			relName := filepath.ToSlash(filepath.Join(filepath.Dir(f.Name), "_rels", filepath.Base(f.Name)+".rels"))
			for _, rf := range z.File {
				if rf.Name == relName {
					rb, _ := rf.Open()
					rd, _ := io.ReadAll(rb)
					rb.Close()
					rels = parseRels(rd)
				}
			}
			var pics []struct {
				Blip struct {
					Embed string `xml:"embed,attr"`
				} `xml:"blipFill>blip"`
			}
			_ = xml.Unmarshal(data, &struct {
				Pics *[]struct {
					Blip struct {
						Embed string `xml:"embed,attr"`
					} `xml:"blipFill>blip"`
				} `xml:"pic"`
			}{Pics: &pics})
			for i, pic := range pics {
				if target := rels[pic.Blip.Embed]; target != "" {
					target = filepath.ToSlash(filepath.Join(filepath.Dir(f.Name), target))
					for _, mf := range z.File {
						if mf.Name == target {
							mb, _ := mf.Open()
							raw, _ := io.ReadAll(mb)
							mb.Close()
							ext := strings.TrimPrefix(filepath.Ext(target), ".")
							s.Elements = append(s.Elements, ppt.Element{ID: fmt.Sprintf("image-%d", i+1), Type: "image", X: 80, Y: 140 + float64(i*100), Width: 400, Height: 220, AssetURL: "data:image/" + ext + ";base64," + base64.StdEncoding.EncodeToString(raw)})
						}
					}
				}
			}
			p.Slides = append(p.Slides, s)
		}
	}
	if len(p.Slides) == 0 {
		p.Slides = []ppt.Slide{{ID: "slide-1", Layout: "imported"}}
	}
	return p, nil
}
