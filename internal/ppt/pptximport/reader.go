package pptximport

import (
	"archive/zip"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"kuaima_cli/internal/ppt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type node struct {
	Name string
	Attr []xml.Attr
	Kids []*node
	Text string
}

func parseTree(b []byte) (*node, error) {
	d := xml.NewDecoder(strings.NewReader(string(b)))
	var root *node
	var st []*node
	for {
		t, e := d.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		switch x := t.(type) {
		case xml.StartElement:
			n := &node{Name: x.Name.Local, Attr: x.Attr}
			if len(st) > 0 {
				st[len(st)-1].Kids = append(st[len(st)-1].Kids, n)
			} else {
				root = n
			}
			st = append(st, n)
		case xml.CharData:
			if len(st) > 0 {
				st[len(st)-1].Text += string(x)
			}
		case xml.EndElement:
			if len(st) > 0 {
				st = st[:len(st)-1]
			}
		}
	}
	return root, nil
}
func (n *node) attr(k string) string {
	for _, a := range n.Attr {
		if a.Name.Local == k {
			return a.Value
		}
	}
	return ""
}
func (n *node) relID() string {
	for _, a := range n.Attr {
		if a.Name.Local == "id" && a.Name.Space != "" {
			return a.Value
		}
	}
	return ""
}
func (n *node) child(k string) *node {
	for _, c := range n.Kids {
		if c.Name == k {
			return c
		}
	}
	return nil
}
func (n *node) children(k string) []*node {
	var o []*node
	for _, c := range n.Kids {
		if c.Name == k {
			o = append(o, c)
		}
	}
	return o
}
func emu(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v / 914400 * 96 }
func num(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }

type xf struct{ x, y, w, h, rot float64 }
type ctx struct{ x, y, sx, sy, rot float64 }

func readXF(n *node) xf {
	if n == nil {
		return xf{}
	}
	o, e := n.child("off"), n.child("ext")
	if o == nil || e == nil {
		return xf{}
	}
	return xf{emu(o.attr("x")), emu(o.attr("y")), emu(e.attr("cx")), emu(e.attr("cy")), num(n.attr("rot")) / 60000}
}
func directXF(n *node) *node {
	for _, c := range n.Kids {
		if c.Name == "spPr" || c.Name == "grpSpPr" {
			if x := c.child("xfrm"); x != nil {
				return x
			}
		}
	}
	return nil
}
func apply(t xf, c ctx) (float64, float64, float64, float64, float64) {
	return c.x + t.x*c.sx, c.y + t.y*c.sy, t.w * c.sx, t.h * c.sy, c.rot + t.rot
}
func parseRels(b []byte) map[string]string {
	var v struct {
		R []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	_ = xml.Unmarshal(b, &v)
	o := map[string]string{}
	for _, r := range v.R {
		o[r.ID] = r.Target
	}
	return o
}
func clr(n *node) string {
	if n == nil {
		return ""
	}
	if s := n.child("srgbClr"); s != nil {
		return "#" + s.attr("val")
	}
	// PowerPoint frequently stores text/shape colors as theme references
	// (schemeClr) rather than explicit RGB values. Resolve the common theme
	// slots to the model's stable palette so they render in the editor.
	if s := n.child("schemeClr"); s != nil {
		switch strings.ToLower(s.attr("val")) {
		case "bg1", "lt1":
			return "#FFFFFF"
		case "tx1", "dk1":
			return "#172033"
		case "bg2", "lt2":
			return "#F3F4F6"
		case "tx2", "dk2":
			return "#374151"
		case "accent1":
			return "#4472C4"
		case "accent2":
			return "#ED7D31"
		case "accent3":
			return "#A5A5A5"
		case "accent4":
			return "#FFC000"
		case "accent5":
			return "#5B9BD5"
		case "accent6":
			return "#70AD47"
		}
	}
	return ""
}
func colorValue(n *node) string {
	c := clr(n)
	if c == "" && n != nil {
		if n.Name == "srgbClr" {
			c = "#" + n.attr("val")
		}
		if n.Name == "schemeClr" {
			c = clr(&node{Kids: []*node{n}})
		}
	}
	if c == "" {
		return ""
	}
	// Apply common DrawingML luminance transforms so theme colors match PPTX.
	var mod, off = 1.0, 0.0
	var scan func(*node)
	scan = func(x *node) {
		if x == nil {
			return
		}
		if m := x.child("lumMod"); m != nil {
			mod = num(m.attr("val")) / 100000
		}
		if o := x.child("lumOff"); o != nil {
			off = num(o.attr("val")) / 100000
		}
		for _, k := range x.Kids {
			scan(k)
		}
	}
	scan(n)
	if mod != 1 || off != 0 {
		var r, g, b int
		fmt.Sscanf(c, "#%02x%02x%02x", &r, &g, &b)
		r = int(math.Max(0, math.Min(255, float64(r)*mod+255*off)))
		g = int(math.Max(0, math.Min(255, float64(g)*mod+255*off)))
		b = int(math.Max(0, math.Min(255, float64(b)*mod+255*off)))
		c = fmt.Sprintf("#%02X%02X%02X", r, g, b)
	}
	a := 1.0
	var findAlpha func(*node)
	findAlpha = func(x *node) {
		if x == nil {
			return
		}
		if aNode := x.child("alpha"); aNode != nil {
			a = num(aNode.attr("val")) / 100000
			return
		}
		for _, k := range x.Kids {
			findAlpha(k)
			if a != 1 {
				return
			}
		}
	}
	findAlpha(n)
	if a >= .999 {
		return c
	}
	if a < 0 {
		a = 0
	}
	var r, g, b int
	fmt.Sscanf(c, "#%02x%02x%02x", &r, &g, &b)
	return fmt.Sprintf("rgba(%d,%d,%d,%.3f)", r, g, b, a)
}
func fillStyle(n *node) string {
	if n == nil {
		return ""
	}
	if s := n.child("solidFill"); s != nil {
		return colorValue(s)
	}
	if g := n.child("gradFill"); g != nil {
		var stops []string
		if gs := g.child("gsLst"); gs != nil {
			for _, x := range gs.children("gs") {
				c := colorValue(x.child("srgbClr"))
				if c == "" {
					c = colorValue(x.child("schemeClr"))
				}
				if c != "" {
					if p := x.attr("pos"); p != "" {
						c += " " + fmt.Sprintf("%.2f%%", num(p)/1000)
					}
					stops = append(stops, c)
				}
			}
		}
		if len(stops) >= 2 {
			return "linear-gradient(135deg," + strings.Join(stops, ",") + ")"
		}
	}
	return ""
}
func lineStyle(n *node) string {
	if n == nil {
		return ""
	}
	ln := n.child("ln")
	if ln == nil {
		return ""
	}
	w := emu(ln.attr("w"))
	if w <= 0 {
		w = 1
	}
	c := fillStyle(ln)
	if c == "" {
		c = "#64748B"
	}
	return fmt.Sprintf("%.2fpx solid %s", w, c)
}
func shadowStyle(n *node) string {
	if n == nil {
		return ""
	}
	e := n.child("effectLst")
	if e == nil {
		return ""
	}
	s := e.child("outerShdw")
	if s == nil {
		return ""
	}
	blur := emu(s.attr("blurRad"))
	if blur < 0 {
		blur = 0
	}
	dist := emu(s.attr("dist"))
	dir := num(s.attr("dir")) / 60000 * 3.14159265 / 180
	ox, oy := dist*0.75, dist*0.75
	if dist > 0 {
		ox, oy = dist*math.Cos(dir), dist*math.Sin(dir)
	}
	c := colorValue(s.child("srgbClr"))
	if c == "" {
		c = colorValue(s.child("schemeClr"))
	}
	if c == "" {
		c = "rgba(0,0,0,.25)"
	}
	return fmt.Sprintf("%.1fpx %.1fpx %.1fpx %s", ox, oy, blur, c)
}
func runStyle(r, p *node) map[string]any {
	st := map[string]any{}
	q := r.child("rPr")
	if q == nil {
		q = p.child("endParaRPr")
	}
	if q == nil {
		return st
	}
	if z := q.attr("sz"); z != "" {
		st["fontSize"] = num(z) / 100
	}
	if q.attr("b") == "1" {
		st["bold"] = true
	}
	if q.attr("i") == "1" {
		st["italic"] = true
	}
	if q.attr("u") != "" && q.attr("u") != "none" {
		st["underline"] = true
	}
	if f := q.child("latin"); f != nil && f.attr("typeface") != "" {
		st["fontFamily"] = f.attr("typeface")
	}
	if c := fillStyle(q); c != "" && !strings.Contains(c, "gradient") {
		st["color"] = c
	}
	return st
}
func textBody(n *node) (string, map[string]any) {
	tb := n.child("txBody")
	if tb == nil {
		return "", nil
	}
	var ls []string
	st := map[string]any{}
	for _, p := range tb.children("p") {
		var b strings.Builder
		if pp := p.child("pPr"); pp != nil {
			if dr := pp.child("defRPr"); dr != nil {
				if c := fillStyle(dr); c != "" {
					st["color"] = c
				}
			}
			if ls := pp.child("lnSpc"); ls != nil {
				if pct := ls.child("spcPct"); pct != nil {
					st["lineHeight"] = num(pct.attr("val")) / 100000
				}
				if pts := ls.child("spcPts"); pts != nil {
					// spacing in hundredths of a point; normalize to a CSS multiplier
					st["lineHeight"] = num(pts.attr("val")) / 1200
				}
			}
		}
		for _, r := range p.children("r") {
			if t := r.child("t"); t != nil {
				b.WriteString(t.Text)
			}
			for k, v := range runStyle(r, p) {
				if _, ok := st[k]; !ok {
					st[k] = v
				}
			}
		}
		ls = append(ls, b.String())
	}
	return strings.Join(ls, "\n"), st
}

type importer struct {
	z      *zip.Reader
	rel    map[string]string
	elems  *[]ppt.Element
	zorder int
}

func (im *importer) imageData(target string) string {
	if strings.HasPrefix(target, "/") {
		target = strings.TrimPrefix(target, "/")
	}
	if !strings.HasPrefix(target, "ppt/") {
		target = filepath.ToSlash(filepath.Join("ppt/slides", target))
	}
	for _, f := range im.z.File {
		if f.Name == target {
			b, e := f.Open()
			if e != nil {
				return ""
			}
			raw, _ := io.ReadAll(b)
			b.Close()
			ext := strings.TrimPrefix(filepath.Ext(target), ".")
			return "data:image/" + ext + ";base64," + base64.StdEncoding.EncodeToString(raw)
		}
	}
	return ""
}
func (im *importer) walk(n *node, c ctx) {
	if n == nil {
		return
	}
	if n.Name == "grpSp" {
		g := readXF(directXF(n))
		nc := c
		if x := directXF(n); x != nil {
			co, ce := x.child("chOff"), x.child("chExt")
			if co != nil && ce != nil {
				cw, ch := emu(ce.attr("cx")), emu(ce.attr("cy"))
				if cw > 0 && ch > 0 {
					nc = ctx{c.x + (g.x-emu(co.attr("x")))*c.sx, c.y + (g.y-emu(co.attr("y")))*c.sy, g.w / cw * c.sx, g.h / ch * c.sy, c.rot + g.rot}
				}
			}
		}
		for _, k := range n.Kids {
			im.walk(k, nc)
		}
		return
	}
	if n.Name == "sp" || n.Name == "pic" {
		t := readXF(directXF(n))
		x, y, w, h, r := apply(t, c)
		if w <= 0 || h <= 0 {
			return
		}
		var idn *node
		if n.Name == "sp" {
			idn = n.child("nvSpPr")
		} else {
			idn = n.child("nvPicPr")
		}
		id := "element"
		if idn != nil && idn.child("cNvPr") != nil && idn.child("cNvPr").attr("id") != "" {
			id = idn.child("cNvPr").attr("id")
		}
		if n.Name == "pic" {
			bl := n.child("blipFill")
			if bl != nil && bl.child("blip") != nil {
				if src := im.imageData(im.rel[bl.child("blip").attr("embed")]); src != "" {
					st := map[string]any{}
					if sp := n.child("spPr"); sp != nil {
						if v := lineStyle(sp); v != "" {
							st["border"] = v
						}
						if v := shadowStyle(sp); v != "" {
							st["shadow"] = v
						}
					}
					if cr := bl.child("srcRect"); cr != nil {
						st["crop"] = map[string]any{"left": num(cr.attr("l")) / 100000, "top": num(cr.attr("t")) / 100000, "right": num(cr.attr("r")) / 100000, "bottom": num(cr.attr("b")) / 100000}
					}
					*im.elems = append(*im.elems, ppt.Element{ID: "image-" + id, Type: "image", X: x, Y: y, Width: w, Height: h, AssetURL: src, Rotation: r, ZIndex: im.zorder, Style: st})
					im.zorder++
				}
			}
			return
		}
		content, style := textBody(n)
		typ := "shape"
		if content != "" {
			typ = "text"
		}
		if style == nil {
			style = map[string]any{}
		}
		if idn != nil && idn.child("cNvPr") != nil {
			if name := idn.child("cNvPr").attr("name"); name != "" {
				style["shapeName"] = name
			}
		}
		if sp := n.child("spPr"); sp != nil {
			if c := fillStyle(sp); c != "" {
				style["background"] = c
			}
			// Some Office files keep the effective fill in the shape style
			// reference instead of spPr.
			if _, ok := style["background"]; !ok {
				if ps := n.child("style"); ps != nil {
					if fr := ps.child("fillRef"); fr != nil {
						if c := colorValue(fr.child("schemeClr")); c != "" {
							style["background"] = c
						}
					}
				}
			}
			if g := sp.child("prstGeom"); g != nil && g.attr("prst") != "" {
				style["shape"] = g.attr("prst")
			}
			if _, ok := style["shape"]; !ok && style["shapeName"] != nil {
				style["shape"] = "custom"
			}
			if v := lineStyle(sp); v != "" {
				style["border"] = v
			}
			if v := shadowStyle(sp); v != "" {
				style["shadow"] = v
			}
		}
		*im.elems = append(*im.elems, ppt.Element{ID: typ + "-" + id, Type: typ, X: x, Y: y, Width: w, Height: h, Content: content, Style: style, Rotation: r, ZIndex: im.zorder})
		im.zorder++
	}
}
func Read(r io.ReaderAt, size int64) (ppt.Presentation, error) {
	z, e := zip.NewReader(r, size)
	if e != nil {
		return ppt.Presentation{}, e
	}
	p := ppt.New16x9("Imported presentation")
	// Preserve the order declared by p:sldIdLst (it may differ from slideN
	// filenames, especially after slides are rearranged in PowerPoint).
	slideOrder := map[string]int{}
	var presentationRels map[string]string
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
			if xml.Unmarshal(d, &v) == nil && v.S.Cx > 0 {
				p.Width = float64(v.S.Cx) / 914400 * 96
				p.Height = float64(v.S.Cy) / 914400 * 96
			}
			if root, er := parseTree(d); er == nil {
				if ids := root.child("sldIdLst"); ids != nil {
					for i, id := range ids.Kids {
						if rid := id.relID(); rid != "" {
							slideOrder[rid] = i
						}
					}
				}
			}
		}
		if f.Name == "ppt/_rels/presentation.xml.rels" {
			b, _ := f.Open()
			d, _ := io.ReadAll(b)
			b.Close()
			presentationRels = parseRels(d)
		}
	}
	// zip entry order is not presentation order. Sort slide XML by its numeric
	// suffix (the normal OOXML ordering) before parsing.
	slides := make([]*zip.File, 0)
	for _, f := range z.File {
		if !strings.HasPrefix(f.Name, "ppt/slides/slide") || !strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		slides = append(slides, f)
	}
	sort.SliceStable(slides, func(i, j int) bool {
		return slideNumber(slides[i].Name) < slideNumber(slides[j].Name)
	})
	if len(slideOrder) > 0 && presentationRels != nil {
		// Build a filename -> declared index map from the presentation rels.
		declared := map[string]int{}
		for rid, target := range presentationRels {
			if idx, ok := slideOrder[rid]; ok {
				name := strings.TrimPrefix(target, "/")
				if !strings.HasPrefix(name, "ppt/") {
					name = filepath.ToSlash(filepath.Join("ppt", name))
				}
				declared[name] = idx
			}
		}
		sort.SliceStable(slides, func(i, j int) bool {
			a, aok := declared[slides[i].Name]
			b, bok := declared[slides[j].Name]
			if aok && bok {
				return a < b
			}
			if aok != bok {
				return aok
			}
			return slideNumber(slides[i].Name) < slideNumber(slides[j].Name)
		})
	}
	found := false
	for _, f := range slides {
		found = true
		b, _ := f.Open()
		d, _ := io.ReadAll(b)
		b.Close()
		root, er := parseTree(d)
		if er != nil {
			continue
		}
		rel := map[string]string{}
		rn := filepath.ToSlash(filepath.Join(filepath.Dir(f.Name), "_rels", filepath.Base(f.Name)+".rels"))
		for _, rf := range z.File {
			if rf.Name == rn {
				rb, _ := rf.Open()
				rd, _ := io.ReadAll(rb)
				rb.Close()
				rel = parseRels(rd)
			}
		}
		s := ppt.Slide{ID: "slide-" + strings.TrimSuffix(filepath.Base(f.Name), filepath.Ext(f.Name))[5:], Layout: "imported"}
		im := importer{z: z, rel: rel, elems: &s.Elements, zorder: 1}
		if cs := root.child("cSld"); cs != nil {
			if bg := cs.child("bg"); bg != nil {
				bl := bg.child("blipFill")
				if bl == nil && bg.child("bgPr") != nil {
					bl = bg.child("bgPr").child("blipFill")
				}
				if bl != nil && bl.child("blip") != nil {
					if src := im.imageData(rel[bl.child("blip").attr("embed")]); src != "" {
						s.Elements = append(s.Elements, ppt.Element{ID: "background", Type: "image", X: 0, Y: 0, Width: p.Width, Height: p.Height, AssetURL: src, ZIndex: 0})
					}
				}
				if bgpr := bg.child("bgPr"); bgpr != nil {
					if c := clr(bgpr.child("solidFill")); c != "" && p.Theme.Background == "#FFFFFF" {
						p.Theme.Background = c
					}
				}
			}
			if st := cs.child("spTree"); st != nil {
				for _, k := range st.Kids {
					im.walk(k, ctx{sx: 1, sy: 1})
				}
			}
		}
		// A slide can inherit a picture from its slide master. Materialize the
		// largest master picture as a background element when the slide itself
		// has no background image.
		if !hasBackgroundImage(s.Elements) {
			if bg := masterBackground(z, p.Width, p.Height); bg.AssetURL != "" {
				s.Elements = append([]ppt.Element{bg}, s.Elements...)
			}
		}
		p.Slides = append(p.Slides, s)
	}
	if !found {
		return ppt.Presentation{}, fmt.Errorf("pptx contains no slides")
	}
	return p, nil
}

func slideNumber(name string) int {
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	n, _ := strconv.Atoi(strings.TrimPrefix(base, "slide"))
	return n
}

func hasBackgroundImage(es []ppt.Element) bool {
	for _, e := range es {
		if e.Type == "image" && e.ID == "background" {
			return true
		}
	}
	return false
}

func masterBackground(z *zip.Reader, w, h float64) ppt.Element {
	var best ppt.Element
	bestArea := 0.0
	for _, f := range z.File {
		if !strings.HasPrefix(f.Name, "ppt/slideMasters/slideMaster") || !strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		b, e := f.Open()
		if e != nil {
			continue
		}
		raw, _ := io.ReadAll(b)
		b.Close()
		root, e := parseTree(raw)
		if e != nil {
			continue
		}
		rels := map[string]string{}
		rn := filepath.ToSlash(filepath.Join(filepath.Dir(f.Name), "_rels", filepath.Base(f.Name)+".rels"))
		for _, rf := range z.File {
			if rf.Name == rn {
				rb, _ := rf.Open()
				rd, _ := io.ReadAll(rb)
				rb.Close()
				rels = parseRels(rd)
				break
			}
		}
		var walk func(*node)
		walk = func(n *node) {
			if n == nil {
				return
			}
			if n.Name == "pic" {
				xf := readXF(directXF(n))
				area := xf.w * xf.h
				bl := n.child("blipFill")
				if bl != nil && bl.child("blip") != nil {
					if src := (&importer{z: z, rel: rels}).imageData(rels[bl.child("blip").attr("embed")]); src != "" && area > bestArea {
						bestArea = area
						best = ppt.Element{ID: "background", Type: "image", X: 0, Y: 0, Width: w, Height: h, AssetURL: src, ZIndex: 0}
					}
				}
			}
			for _, k := range n.Kids {
				walk(k)
			}
		}
		walk(root)
	}
	return best
}
