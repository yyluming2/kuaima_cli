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
	if n == nil {
		return ""
	}
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
	if n == nil {
		return nil
	}
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

// The editor canvas uses a 128 logical-pixel-per-inch coordinate system
// (1280x720 for the default 10x5.625 inch 16:9 slide). Keep all EMU
// conversions on that scale so imported geometry matches the canvas.
func emu(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v / 914400 * ppt.LogicalDPI }
func num(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }

type xf struct {
	x, y, w, h, rot float64
	flipH, flipV    bool
}
type ctx struct{ x, y, sx, sy, rot float64 }

func readXF(n *node) xf {
	if n == nil {
		return xf{}
	}
	o, e := n.child("off"), n.child("ext")
	if o == nil || e == nil {
		return xf{}
	}
	return xf{emu(o.attr("x")), emu(o.attr("y")), emu(e.attr("cx")), emu(e.attr("cy")), num(n.attr("rot")) / 60000, n.attr("flipH") == "1", n.attr("flipV") == "1"}
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

// parseThemeColors reads the effective DrawingML color scheme from a theme
// part. Theme references in slides (schemeClr) must be resolved against this
// table; the fallback palette is only for malformed/legacy files.
func parseThemeColors(b []byte) map[string]string {
	colors := map[string]string{}
	root, err := parseTree(b)
	if err != nil || root == nil {
		return colors
	}
	var scheme *node
	var find func(*node)
	find = func(n *node) {
		if n == nil || scheme != nil {
			return
		}
		if n.Name == "clrScheme" {
			scheme = n
			return
		}
		for _, k := range n.Kids {
			find(k)
		}
	}
	find(root)
	if scheme == nil {
		return colors
	}
	for _, entry := range scheme.Kids {
		var c string
		if x := entry.child("srgbClr"); x != nil {
			c = "#" + x.attr("val")
		}
		if c == "" {
			if x := entry.child("sysClr"); x != nil {
				if v := x.attr("lastClr"); v != "" {
					c = "#" + v
				}
			}
		}
		if c != "" {
			colors[strings.ToLower(entry.Name)] = c
		}
	}
	return colors
}

func clr(n *node, theme map[string]string) string {
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
		key := strings.ToLower(s.attr("val"))
		if theme != nil {
			if c := theme[key]; c != "" {
				return c
			}
		}
		switch key {
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
func colorValue(n *node, theme map[string]string) string {
	c := clr(n, theme)
	if c == "" && n != nil {
		if n.Name == "srgbClr" {
			c = "#" + n.attr("val")
		}
		if n.Name == "schemeClr" {
			c = clr(&node{Kids: []*node{n}}, theme)
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
func fillStyle(n *node, theme map[string]string) string {
	if n == nil {
		return ""
	}
	if s := n.child("solidFill"); s != nil {
		return colorValue(s, theme)
	}
	if n.child("noFill") != nil {
		return "transparent"
	}
	if g := n.child("gradFill"); g != nil {
		var stops []string
		if gs := g.child("gsLst"); gs != nil {
			for _, x := range gs.children("gs") {
				c := colorValue(x.child("srgbClr"), theme)
				if c == "" {
					c = colorValue(x.child("schemeClr"), theme)
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

func presetGeometry(sp *node) *ppt.Geometry {
	if sp == nil {
		return nil
	}
	if g := sp.child("prstGeom"); g != nil {
		p := g.attr("prst")
		if p != "" {
			typ := "preset"
			switch strings.ToLower(p) {
			case "rect":
				typ = "rect"
			case "roundrect":
				typ = "roundRect"
			case "ellipse", "oval":
				typ = "ellipse"
			}
			return &ppt.Geometry{Type: typ, Preset: p}
		}
	}
	if g := sp.child("custGeom"); g != nil {
		if pg := g.child("pathLst"); pg != nil {
			var d strings.Builder
			for _, path := range pg.children("path") {
				pw, ph := num(path.attr("w")), num(path.attr("h"))
				if pw <= 0 {
					pw = 1
				}
				if ph <= 0 {
					ph = 1
				}
				scale := func(v string, max float64) string { return strconv.FormatFloat(num(v)/max*1000, 'f', 3, 64) }
				for _, cmd := range path.Kids {
					switch cmd.Name {
					case "moveTo":
						if pt := cmd.child("pt"); pt != nil {
							d.WriteString("M " + scale(pt.attr("x"), pw) + " " + scale(pt.attr("y"), ph) + " ")
						}
					case "lnTo":
						if pt := cmd.child("pt"); pt != nil {
							d.WriteString("L " + scale(pt.attr("x"), pw) + " " + scale(pt.attr("y"), ph) + " ")
						}
					case "cubicBezTo":
						pts := cmd.children("pt")
						if len(pts) == 3 {
							d.WriteString("C " + scale(pts[0].attr("x"), pw) + " " + scale(pts[0].attr("y"), ph) + " " + scale(pts[1].attr("x"), pw) + " " + scale(pts[1].attr("y"), ph) + " " + scale(pts[2].attr("x"), pw) + " " + scale(pts[2].attr("y"), ph) + " ")
						}
					case "close":
						d.WriteString("Z ")
					}
				}
			}
			if d.Len() > 0 {
				return &ppt.Geometry{Type: "custom", Path: strings.TrimSpace(d.String()), ViewBox: "0 0 1000 1000"}
			}
		}
	}
	return nil
}

func structuredFill(sp *node, theme map[string]string) *ppt.Fill {
	if sp == nil {
		return nil
	}
	if sf := sp.child("solidFill"); sf != nil {
		c := colorValue(sf, theme)
		if c == "transparent" {
			return &ppt.Fill{Type: "none"}
		}
		return &ppt.Fill{Type: "solid", Color: &ppt.Color{Type: "rgb", RGB: c}}
	}
	if sp.child("noFill") != nil {
		return &ppt.Fill{Type: "none"}
	}
	if gf := sp.child("gradFill"); gf != nil {
		out := &ppt.Gradient{Type: "linear", Angle: 135}
		if gs := gf.child("gsLst"); gs != nil {
			for _, stop := range gs.children("gs") {
				var c string
				if x := stop.child("srgbClr"); x != nil {
					c = colorValue(x, theme)
				}
				if c == "" {
					c = colorValue(stop.child("schemeClr"), theme)
				}
				if c != "" {
					out.Stops = append(out.Stops, ppt.GradientStop{Position: num(stop.attr("pos")) / 1000, Color: ppt.Color{Type: "css", RGB: c}})
				}
			}
		}
		return &ppt.Fill{Type: "gradient", Gradient: out}
	}
	return nil
}
func lineStyle(n *node, theme map[string]string) string {
	if n == nil {
		return ""
	}
	ln := n.child("ln")
	if ln == nil {
		return ""
	}
	if ln.child("noFill") != nil {
		return ""
	}
	w := emu(ln.attr("w"))
	if w <= 0 {
		w = 1
	}
	c := fillStyle(ln, theme)
	if c == "" {
		c = "#64748B"
	}
	dash := "solid"
	if pr := ln.child("prstDash"); pr != nil {
		switch strings.ToLower(pr.attr("val")) {
		case "dash", "dashdot", "sysdash":
			dash = "dashed"
		case "dot", "sysdot":
			dash = "dotted"
		}
	}
	return fmt.Sprintf("%.2fpx %s %s", w, dash, c)
}

func structuredLine(sp *node, theme map[string]string) *ppt.Line {
	if sp == nil || sp.child("ln") == nil {
		return nil
	}
	ln := sp.child("ln")
	if ln.child("noFill") != nil {
		return &ppt.Line{Width: emu(ln.attr("w")), Color: &ppt.Color{Type: "none"}}
	}
	w := emu(ln.attr("w"))
	if w <= 0 {
		w = 1
	}
	c := colorValue(ln, theme)
	if c == "" {
		c = "#64748B"
	}
	dash := "solid"
	if d := ln.child("prstDash"); d != nil {
		dash = d.attr("val")
	}
	return &ppt.Line{Width: w, Color: &ppt.Color{Type: "css", RGB: c}, Dash: dash, BeginArrow: ln.child("headEnd").attr("type"), EndArrow: ln.child("tailEnd").attr("type")}
}

func structuredText(st map[string]any) *ppt.TextStyle {
	if len(st) == 0 {
		return nil
	}
	t := &ppt.TextStyle{}
	if v, ok := st["fontFamily"].(string); ok {
		t.FontFamily = v
	}
	if v, ok := st["fontSize"].(float64); ok {
		t.FontSize = v
	}
	if v, ok := st["bold"].(bool); ok {
		t.Bold = v
	}
	if v, ok := st["italic"].(bool); ok {
		t.Italic = v
	}
	if v, ok := st["underline"].(bool); ok {
		t.Underline = v
	}
	if v, ok := st["align"].(string); ok {
		t.Align = v
	}
	if v, ok := st["verticalAlign"].(string); ok {
		t.VerticalAlign = v
	}
	if v, ok := st["lineHeight"].(float64); ok {
		t.LineHeight = v
	}
	return t
}

func structuredEffects(sp *node, theme map[string]string) *ppt.Effects {
	if sp == nil {
		return nil
	}
	e := sp.child("effectLst")
	if e == nil {
		return nil
	}
	out := &ppt.Effects{}
	if s := e.child("outerShdw"); s != nil {
		out.Shadow = shadowStyle(sp, theme)
	}
	if s := e.child("innerShdw"); s != nil {
		blur, dist := emu(s.attr("blurRad")), emu(s.attr("dist"))
		dir := num(s.attr("dir")) / 60000 * math.Pi / 180
		c := colorValue(s, theme)
		if c == "" {
			c = "rgba(0,0,0,.25)"
		}
		out.InnerShadow = fmt.Sprintf("inset %.1fpx %.1fpx %.1fpx %s", dist*math.Cos(dir), dist*math.Sin(dir), blur, c)
	}
	if g := e.child("glow"); g != nil {
		out.Glow = fmt.Sprintf("%.2fpx %s", emu(g.attr("rad")), colorValue(g, theme))
	}
	if s := e.child("softEdge"); s != nil {
		out.SoftEdge = emu(s.attr("rad"))
	}
	if e.child("reflection") != nil {
		out.Reflection = true
	}
	if out.Shadow == "" && out.InnerShadow == "" && out.Glow == "" && out.SoftEdge == 0 && !out.Reflection {
		return nil
	}
	return out
}
func shadowStyle(n *node, theme map[string]string) string {
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
	c := colorValue(s.child("srgbClr"), theme)
	if c == "" {
		c = colorValue(s.child("schemeClr"), theme)
	}
	if c == "" {
		c = "rgba(0,0,0,.25)"
	}
	return fmt.Sprintf("%.1fpx %.1fpx %.1fpx %s", ox, oy, blur, c)
}
func runStyle(r, p *node, theme map[string]string) map[string]any {
	q := r.child("rPr")
	if q == nil {
		q = p.child("endParaRPr")
	}
	return rPrStyle(q, theme)
}
func rPrStyle(q *node, theme map[string]string) map[string]any {
	st := map[string]any{}
	if q == nil {
		return st
	}
	if z := q.attr("sz"); z != "" {
		// OOXML stores font size in hundredths of a point. The editor model
		// stores CSS-like logical pixels, so convert points using the model DPI.
		st["fontSize"] = (num(z) / 100) * ppt.LogicalDPI / 72
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
	if c := fillStyle(q, theme); c != "" && !strings.Contains(c, "gradient") {
		st["color"] = c
	}
	return st
}
func textBody(n *node, theme map[string]string) (string, map[string]any, []ppt.TextRun) {
	tb := n.child("txBody")
	if tb == nil {
		return "", nil, nil
	}
	var ls []string
	var runs []ppt.TextRun
	st := map[string]any{}
	if bp := tb.child("bodyPr"); bp != nil {
		if v := bp.attr("anchor"); v != "" {
			st["verticalAlign"] = v
		}
		for _, k := range []string{"lIns", "rIns", "tIns", "bIns"} {
			if v := bp.attr(k); v != "" {
				st[map[string]string{"lIns": "marginLeft", "rIns": "marginRight", "tIns": "marginTop", "bIns": "marginBottom"}[k]] = emu(v)
			}
		}
	}
	for pi, p := range tb.children("p") {
		if pi > 0 {
			runs = append(runs, ppt.TextRun{Content: "\n"})
		}
		var b strings.Builder
		pp := p.child("pPr")
		if pp != nil {
			if a := pp.attr("algn"); a != "" {
				st["align"] = a
			}
			if dr := pp.child("defRPr"); dr != nil {
				if c := fillStyle(dr, theme); c != "" {
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
			var text string
			if t := r.child("t"); t != nil {
				text = t.Text
				b.WriteString(text)
			}
			rstyle := runStyle(r, p, theme)
			// A run inherits paragraph defaults unless it overrides them in
			// its own rPr.  Store the effective style on the run so consumers
			// do not need to reconstruct DrawingML inheritance.
			if pp != nil {
				if dr := pp.child("defRPr"); dr != nil {
					defaults := rPrStyle(dr, theme)
					for k, v := range rstyle {
						defaults[k] = v
					}
					rstyle = defaults
				}
			}
			if text != "" {
				// Keep each a:r's formatting instead of folding it into the
				// first run's element style.  The latter is retained below as
				// the default/inherited style for compatibility.
				runs = append(runs, ppt.TextRun{Content: text, Style: rstyle})
			}
		}
		ls = append(ls, b.String())
	}
	return strings.Join(ls, "\n"), st, runs
}

type importer struct {
	z      *zip.Reader
	rel    map[string]string
	theme  map[string]string
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
	if n.Name == "sp" || n.Name == "pic" || n.Name == "cxnSp" {
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
						if v := lineStyle(sp, im.theme); v != "" {
							st["border"] = v
						}
						if v := shadowStyle(sp, im.theme); v != "" {
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
		content, style, textRuns := textBody(n, im.theme)
		typ := "shape"
		if n.Name == "cxnSp" {
			typ = "line"
		}
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
		var geom *ppt.Geometry
		var structuredFillValue *ppt.Fill
		var structuredLineValue *ppt.Line
		var structuredEffectsValue *ppt.Effects
		if sp := n.child("spPr"); sp != nil {
			if c := fillStyle(sp, im.theme); c != "" {
				style["background"] = c
			}
			// Some Office files keep the effective fill in the shape style
			// reference instead of spPr.
			if _, ok := style["background"]; !ok {
				if ps := n.child("style"); ps != nil {
					if fr := ps.child("fillRef"); fr != nil {
						if c := colorValue(fr.child("schemeClr"), im.theme); c != "" {
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
			if v := lineStyle(sp, im.theme); v != "" {
				style["border"] = v
			}
			geom = presetGeometry(sp)
			structuredFillValue = structuredFill(sp, im.theme)
			structuredLineValue = structuredLine(sp, im.theme)
			structuredEffectsValue = structuredEffects(sp, im.theme)
			if n.Name == "cxnSp" && geom == nil {
				geom = &ppt.Geometry{Type: "line"}
			}
			if v := shadowStyle(sp, im.theme); v != "" {
				style["shadow"] = v
			}
		}
		if n.Name == "cxnSp" && geom == nil {
			geom = &ppt.Geometry{Type: "line"}
		}
		*im.elems = append(*im.elems, ppt.Element{ID: typ + "-" + id, Type: typ, X: x, Y: y, Width: w, Height: h, Content: content, Style: style, TextRuns: textRuns, Rotation: r, ZIndex: im.zorder, Geometry: geom, Fill: structuredFillValue, Line: structuredLineValue, Effects: structuredEffectsValue, Text: structuredText(style), FlipH: t.flipH, FlipV: t.flipV})
		im.zorder++
	}
}
func Read(r io.ReaderAt, size int64) (ppt.Presentation, error) {
	z, e := zip.NewReader(r, size)
	if e != nil {
		return ppt.Presentation{}, e
	}
	p := ppt.New16x9("Imported presentation")
	// The theme is shared by all slides. Locate the first theme part (the
	// relationship target can vary, while ppt/theme/theme1.xml is conventional).
	themeColors := map[string]string{}
	for _, f := range z.File {
		if strings.HasPrefix(f.Name, "ppt/theme/") && strings.HasSuffix(f.Name, ".xml") {
			if b, er := f.Open(); er == nil {
				raw, _ := io.ReadAll(b)
				b.Close()
				themeColors = parseThemeColors(raw)
				if len(themeColors) > 0 {
					break
				}
			}
		}
	}
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
				p.Width = float64(v.S.Cx) / 914400 * ppt.LogicalDPI
				p.Height = float64(v.S.Cy) / 914400 * ppt.LogicalDPI
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
		im := importer{z: z, rel: rel, theme: themeColors, elems: &s.Elements, zorder: 1}
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
					if c := clr(bgpr.child("solidFill"), themeColors); c != "" && p.Theme.Background == "#FFFFFF" {
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
