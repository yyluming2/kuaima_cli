package pptximport

import (
	"archive/zip"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"kuaima_cli/internal/ppt"
	"log"
	"math"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"
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
func emu(s string) float64  { v, _ := strconv.ParseFloat(s, 64); return v / 914400 * ppt.LogicalDPI }
func num(s string) float64  { v, _ := strconv.ParseFloat(s, 64); return v }
func xmlBool(s string) bool { return s == "1" || strings.EqualFold(strings.TrimSpace(s), "true") }

type xf struct {
	x, y, w, h, rot float64
	flipH, flipV    bool
}
type ctx struct {
	x, y, sx, sy, rot float64
	flipH, flipV      bool
}

func readXF(n *node) xf {
	if n == nil {
		return xf{}
	}
	o, e := n.child("off"), n.child("ext")
	if o == nil || e == nil {
		return xf{}
	}
	return xf{emu(o.attr("x")), emu(o.attr("y")), emu(e.attr("cx")), emu(e.attr("cy")), num(n.attr("rot")) / 60000, xmlBool(n.attr("flipH")), xmlBool(n.attr("flipV"))}
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
	// Keep theme typeface aliases in the same lookup table. DrawingML run
	// properties commonly contain +mj-ea/+mn-ea rather than a real family.
	var fontScheme *node
	var findFonts func(*node)
	findFonts = func(n *node) {
		if n == nil || fontScheme != nil {
			return
		}
		if n.Name == "fontScheme" {
			fontScheme = n
			return
		}
		for _, k := range n.Kids {
			findFonts(k)
		}
	}
	findFonts(root)
	if fontScheme != nil {
		for _, spec := range []struct{ node, prefix string }{{"majorFont", "+mj-"}, {"minorFont", "+mn-"}} {
			if group := fontScheme.child(spec.node); group != nil {
				for _, face := range []struct{ node, suffix string }{{"latin", "lt"}, {"ea", "ea"}, {"cs", "cs"}} {
					if value := group.child(face.node).attr("typeface"); value != "" {
						colors["font:"+spec.prefix+face.suffix] = value
					}
				}
				// Modern Office themes often leave <a:ea typeface=""> empty and
				// provide locale-specific East Asian faces instead.
				if colors["font:"+spec.prefix+"ea"] == "" {
					for _, script := range []string{"Hans", "Hant", "Jpan", "Hang"} {
						for _, candidate := range group.children("font") {
							if candidate.attr("script") == script && candidate.attr("typeface") != "" {
								colors["font:"+spec.prefix+"ea"] = candidate.attr("typeface")
								break
							}
						}
						if colors["font:"+spec.prefix+"ea"] != "" {
							break
						}
					}
				}
			}
		}
	}
	return colors
}

func resolveTypeface(value string, theme map[string]string) string {
	if strings.HasPrefix(value, "+") && theme != nil {
		if resolved := theme["font:"+strings.ToLower(value)]; resolved != "" {
			return resolved
		}
	}
	return value
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

// roundRectRadius returns the DrawingML adjustment as an editor percentage
// (0..50). DrawingML stores the adjustment in 1/100000 units.
func roundRectRadius(sp *node) (float64, bool) {
	if sp == nil {
		return 0, false
	}
	g := sp.child("prstGeom")
	if g == nil || strings.ToLower(g.attr("prst")) != "roundrect" {
		return 0, false
	}
	if av := g.child("avLst"); av != nil {
		for _, gd := range av.children("gd") {
			name := strings.ToLower(gd.attr("name"))
			if name == "adj" || strings.Contains(name, "adj") {
				formula := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(gd.attr("fmla")), "val"))
				v := num(formula)
				if v > 0 {
					return math.Max(0, math.Min(50, v/1000)), true
				}
			}
		}
	}
	return 0, false
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
		return &ppt.Fill{Type: "solid", Color: parsedColor(c)}
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
					out.Stops = append(out.Stops, ppt.GradientStop{Position: num(stop.attr("pos")) / 1000, Color: *parsedColor(c)})
				}
			}
		}
		return &ppt.Fill{Type: "gradient", Gradient: out}
	}
	return nil
}

func parsedColor(c string) *ppt.Color {
	out := &ppt.Color{Type: "css", RGB: c}
	x := strings.TrimPrefix(strings.TrimSpace(c), "rgba(")
	if x != c {
		x = strings.TrimSuffix(x, ")")
		p := strings.Split(x, ",")
		if len(p) == 4 {
			var r, g, b int
			if _, e := fmt.Sscanf(strings.TrimSpace(p[0]), "%d", &r); e == nil {
				_, e = fmt.Sscanf(strings.TrimSpace(p[1]), "%d", &g)
				if e == nil {
					_, e = fmt.Sscanf(strings.TrimSpace(p[2]), "%d", &b)
				}
				if a, ae := strconv.ParseFloat(strings.TrimSpace(p[3]), 64); e == nil && ae == nil {
					out.Alpha = a
					out.RGB = fmt.Sprintf("#%02X%02X%02X", r, g, b)
				}
			}
		}
	}
	return out
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
	return &ppt.Line{Width: w, Color: parsedColor(c), Dash: dash, BeginArrow: ln.child("headEnd").attr("type"), EndArrow: ln.child("tailEnd").attr("type")}
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
	if v, ok := st["lineSpacingPoints"].(float64); ok {
		t.LineSpacingPoints = v
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
	st := rPrStyle(q, theme)
	text := r.child("t").Text
	if containsEastAsian(text) {
		if f := q.child("ea"); f != nil && f.attr("typeface") != "" {
			st["fontFamily"] = resolveTypeface(f.attr("typeface"), theme)
		}
	}
	return st
}

func containsEastAsian(value string) bool {
	for _, r := range value {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			return true
		}
	}
	return false
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
	// Keep explicit false values as well as true values.  DrawingML uses
	// b="0"/i="0" to turn off a paragraph default for one run; omitting the
	// value here would incorrectly inherit bold/italic from defRPr.
	if v := q.attr("b"); v != "" {
		st["bold"] = xmlBool(v)
	}
	if v := q.attr("i"); v != "" {
		st["italic"] = xmlBool(v)
	}
	if v := q.attr("u"); v != "" {
		st["underline"] = !strings.EqualFold(v, "none")
	}
	// DrawingML keeps separate typefaces for latin, East Asian and complex
	// script text.  Using only latin makes Chinese text silently fall back to
	// an unrelated browser/exporter font, so prefer the first explicit face.
	for _, tag := range []string{"latin", "ea", "cs"} {
		if f := q.child(tag); f != nil && f.attr("typeface") != "" {
			st["fontFamily"] = resolveTypeface(f.attr("typeface"), theme)
			break
		}
	}
	if c := fillStyle(q, theme); c != "" && !strings.Contains(c, "gradient") {
		st["color"] = c
	}
	return st
}
func textBody(n *node, theme map[string]string) (string, map[string]any, []ppt.TextRun) {
	a, b, c, _ := textBodyDetailed(n, theme)
	return a, b, c
}

func textBodyDetailed(n *node, theme map[string]string) (string, map[string]any, []ppt.TextRun, []ppt.TextParagraph) {
	tb := n.child("txBody")
	if tb == nil {
		return "", nil, nil, nil
	}
	var ls []string
	var runs []ppt.TextRun
	var paragraphs []ppt.TextParagraph
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
		para := ppt.TextParagraph{}
		var b strings.Builder
		pp := p.child("pPr")
		if pp != nil {
			if a := pp.attr("algn"); a != "" {
				// Keep a stable compatibility alignment for single-style consumers.
				// Per-paragraph alignment is preserved in TextParagraphs below.
				if _, exists := st["align"]; !exists {
					st["align"] = a
				}
				para.Align = a
			}
			if dr := pp.child("defRPr"); dr != nil {
				if c := fillStyle(dr, theme); c != "" {
					st["color"] = c
				}
			}
			if ls := pp.child("lnSpc"); ls != nil {
				if pct := ls.child("spcPct"); pct != nil {
					st["lineHeight"] = num(pct.attr("val")) / 100000
					para.LineHeight = st["lineHeight"].(float64)
				}
				if pts := ls.child("spcPts"); pts != nil {
					// spcPts is an absolute value in hundredths of a point. Our
					// lineHeight is a multiplier, so divide it by the paragraph's
					// effective font size instead of the old hard-coded 12pt value.
					fontPt := paragraphFontSizePoints(p)
					if fontPt > 0 {
						st["lineHeight"] = num(pts.attr("val")) / 100 / fontPt
						st["lineSpacingPoints"] = num(pts.attr("val")) / 100
						para.LineHeight = st["lineHeight"].(float64)
						para.LineSpacingPoints = st["lineSpacingPoints"].(float64)
					}
				}
			}
		}
		for _, r := range p.Kids {
			if r.Name == "br" {
				b.WriteByte('\n')
				runs = append(runs, ppt.TextRun{Content: "\n", Style: map[string]any{}, Paragraph: pi})
				para.Runs = append(para.Runs, len(runs)-1)
				continue
			}
			if r.Name != "r" {
				continue
			}
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
				runs = append(runs, ppt.TextRun{Content: text, Style: rstyle, Paragraph: pi})
				para.Runs = append(para.Runs, len(runs)-1)
			}
		}
		paragraphs = append(paragraphs, para)
		ls = append(ls, b.String())
	}
	var commonLineHeight float64
	explicitLineHeights := 0
	lineHeightSet, lineHeightMixed := false, false
	for _, para := range paragraphs {
		if para.LineHeight <= 0 {
			continue
		}
		explicitLineHeights++
		if !lineHeightSet {
			commonLineHeight, lineHeightSet = para.LineHeight, true
		} else if math.Abs(commonLineHeight-para.LineHeight) > 0.000001 {
			lineHeightMixed = true
		}
	}
	if explicitLineHeights != 0 && explicitLineHeights != len(paragraphs) {
		lineHeightMixed = true
	}
	if lineHeightSet && !lineHeightMixed {
		st["lineHeight"] = commonLineHeight
	} else {
		delete(st, "lineHeight")
	}
	runs = mergeTextRuns(runs)
	// Element style is the compatibility/default style used by the editor.
	// Populate missing typography from the first effective run instead of
	// allowing UI defaults to replace the actual PPTX font and size.
	if len(runs) > 0 {
		for k, v := range runs[0].Style {
			if _, exists := st[k]; !exists {
				st[k] = v
			}
		}
	}
	for i := range paragraphs {
		paragraphs[i].Runs = paragraphs[i].Runs[:0]
	}
	for i, r := range runs {
		if r.Paragraph >= 0 && r.Paragraph < len(paragraphs) {
			paragraphs[r.Paragraph].Runs = append(paragraphs[r.Paragraph].Runs, i)
		}
	}
	return strings.Join(ls, "\n"), st, runs, paragraphs
}

func paragraphFontSizePoints(p *node) float64 {
	if p == nil {
		return 0
	}
	candidates := []*node{p.child("pPr").child("defRPr")}
	for _, run := range p.children("r") {
		candidates = append(candidates, run.child("rPr"))
	}
	candidates = append(candidates, p.child("endParaRPr"))
	for _, candidate := range candidates {
		if candidate != nil && candidate.attr("sz") != "" {
			return num(candidate.attr("sz")) / 100
		}
	}
	return 0
}

// mergeTextRuns reduces adjacent runs that have the same effective style.
// Paragraph boundaries are preserved: identical formatting across two
// paragraphs must remain two runs so the editor can retain line structure.
func mergeTextRuns(runs []ppt.TextRun) []ppt.TextRun {
	if len(runs) < 2 {
		return runs
	}
	out := make([]ppt.TextRun, 0, len(runs))
	for _, run := range runs {
		last := len(out) - 1
		if last >= 0 && out[last].Paragraph == run.Paragraph && reflect.DeepEqual(out[last].Style, run.Style) {
			out[last].Content += run.Content
			continue
		}
		out = append(out, run)
	}
	return out
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
		nc.flipH = c.flipH != g.flipH
		nc.flipV = c.flipV != g.flipV
		if x := directXF(n); x != nil {
			co, ce := x.child("chOff"), x.child("chExt")
			if co != nil && ce != nil {
				cw, ch := emu(ce.attr("cx")), emu(ce.attr("cy"))
				if cw > 0 && ch > 0 {
					// Group child coordinates are expressed in the group's child
					// coordinate space. Translate the child origin by the scaled
					// chOff; subtracting it in parent space causes visible offsets
					// whenever chExt differs from the group's displayed extents.
					gsx, gsy := g.w/cw*c.sx, g.h/ch*c.sy
					nc = ctx{c.x + g.x*c.sx - emu(co.attr("x"))*gsx, c.y + g.y*c.sy - emu(co.attr("y"))*gsy, gsx, gsy, c.rot + g.rot, nc.flipH, nc.flipV}
				}
			}
		}
		for _, k := range n.Kids {
			im.walk(k, nc)
		}
		return
	}
	if n.Name == "sp" || n.Name == "pic" || n.Name == "cxnSp" {
		// PowerPoint uses hidden non-visual shapes for editing/layout helpers.
		// They must not leak into the editable canvas.
		if nv := n.child("nvSpPr"); nv == nil {
			if nv = n.child("nvCxnSpPr"); nv != nil && nv.child("cNvPr") != nil && xmlBool(nv.child("cNvPr").attr("hidden")) {
				return
			}
		} else if nv.child("cNvPr") != nil && xmlBool(nv.child("cNvPr").attr("hidden")) {
			return
		}
		t := readXF(directXF(n))
		x, y, w, h, r := apply(t, c)
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
		content, style, textRuns, textParagraphs := textBodyDetailed(n, im.theme)
		// A slide shape can be a placeholder whose geometry is inherited from
		// its slide layout.  Such shapes (including valid text placeholders)
		// deliberately omit spPr/xfrm in the slide XML.  Keep their text instead
		// of dropping the element as a zero-sized shape; the layout-independent
		// fallback gives the editor a usable, editable text box.
		if w <= 0 || h <= 0 {
			if content == "" {
				return
			}
			x, y, w, h = 0, 0, 1280, 720
		}
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
			if bl := sp.child("blipFill"); bl != nil && bl.child("blip") != nil {
				if src := im.imageData(im.rel[bl.child("blip").attr("embed")]); src != "" {
					style["background"] = src
					structuredFillValue = &ppt.Fill{Type: "image", Image: &ppt.ImageFill{AssetURL: src, RotateWithShape: bl.attr("rotWithShape") != "0"}}
				}
			}
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
				if radius, ok := roundRectRadius(sp); ok {
					style["radius"] = radius
				}
			}
			if _, ok := style["shape"]; !ok && style["shapeName"] != nil {
				style["shape"] = "custom"
			}
			if v := lineStyle(sp, im.theme); v != "" {
				style["border"] = v
			}
			geom = presetGeometry(sp)
			if structuredFillValue == nil {
				structuredFillValue = structuredFill(sp, im.theme)
			}
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
		// Empty transparent rectangles/connectors are common Office artifacts
		// (guides, animation handles, and placeholders).  They have no visual
		// representation and only obstruct hit testing in the canvas.
		if content == "" && invisibleElement(geom, structuredFillValue, structuredLineValue, style) {
			return
		}
		*im.elems = append(*im.elems, ppt.Element{ID: typ + "-" + id, Type: typ, X: x, Y: y, Width: w, Height: h, Content: content, Style: style, TextRuns: textRuns, TextParagraphs: textParagraphs, Rotation: r, ZIndex: im.zorder, Geometry: geom, Fill: structuredFillValue, Line: structuredLineValue, Effects: structuredEffectsValue, Text: structuredText(style), FlipH: t.flipH != c.flipH, FlipV: t.flipV != c.flipV})
		im.zorder++
	}
}

func invisibleElement(geom *ppt.Geometry, fill *ppt.Fill, line *ppt.Line, style map[string]any) bool {
	if geom == nil && fill == nil && line == nil {
		return true
	}
	if line != nil && line.Color != nil && line.Color.Type != "none" {
		return false
	}
	if fill != nil && fill.Type != "none" {
		return false
	}
	if v, ok := style["background"].(string); ok && v != "" && v != "transparent" {
		if !isTransparentColor(v) {
			return false
		}
	}
	if v, ok := style["border"].(string); ok && v != "" {
		return false
	}
	return true
}

func isTransparentColor(value string) bool {
	v := strings.TrimSpace(strings.ToLower(value))
	if !strings.HasPrefix(v, "rgba(") || !strings.HasSuffix(v, ")") {
		return false
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(v, "rgba("), ")"), ",")
	if len(parts) != 4 {
		return false
	}
	a, err := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
	return err == nil && a <= 0
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
				// Keep the concrete theme typefaces available to the editor even
				// when a slide only inherits them through a DrawingML alias.
				seenThemeFonts := map[string]bool{}
				for _, key := range []string{"font:+mj-lt", "font:+mj-ea", "font:+mn-lt", "font:+mn-ea"} {
					if family := strings.TrimSpace(themeColors[key]); family != "" && !seenThemeFonts[family] {
						seenThemeFonts[family] = true
						p.Theme.Fonts = append(p.Theme.Fonts, family)
					}
				}
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
	var presentationRoot *node
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
				presentationRoot = root
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
	p.FontAssets = embeddedFonts(z, presentationRoot, presentationRels)
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

func embeddedFonts(z *zip.Reader, root *node, rels map[string]string) []ppt.FontAsset {
	if root == nil || rels == nil {
		log.Printf("[ppt-font] embedded font list unavailable: presentation root or relationships missing")
		return nil
	}
	list := root.child("embeddedFontLst")
	if list == nil {
		log.Printf("[ppt-font] no embedded fonts in presentation")
		return nil
	}
	var out []ppt.FontAsset
	presentationKey := embeddedFontKey(root)
	for _, embedded := range list.children("embeddedFont") {
		family := embedded.child("font").attr("typeface")
		key := embeddedFontKey(embedded)
		if len(key) != 16 {
			key = presentationKey
		}
		log.Printf("[ppt-font] found family=%q key=%t", family, len(key) == 16)
		for _, variant := range []struct {
			name   string
			style  string
			weight int
		}{{"regular", "normal", 400}, {"bold", "normal", 700}, {"italic", "italic", 400}, {"boldItalic", "italic", 700}} {
			part := embedded.child(variant.name)
			if part == nil {
				continue
			}
			target := rels[part.relID()]
			if target == "" {
				log.Printf("[ppt-font] missing relationship family=%q variant=%s rid=%q", family, variant.name, part.relID())
				continue
			}
			name := strings.TrimPrefix(target, "/")
			if !strings.HasPrefix(name, "ppt/") {
				name = filepath.ToSlash(filepath.Join("ppt", name))
			}
			raw := zipPart(z, name)
			if len(raw) == 0 {
				log.Printf("[ppt-font] empty font part family=%q variant=%s file=%q", family, variant.name, name)
				continue
			}
			originalSize := len(raw)
			if unwrapped, offset := unwrapFontContainer(raw); offset > 0 {
				raw = unwrapped
				log.Printf("[ppt-font] stripped embedded font container family=%q variant=%s offset=%d bytes=%d", family, variant.name, offset, len(raw))
			}
			didDecode := false
			if decodedBytes, ok := decodeObfuscatedFont(raw, key); ok {
				raw = decodedBytes
				didDecode = true
			}
			mime, usable := fontMIME(raw)
			log.Printf("[ppt-font] family=%q variant=%s file=%q bytes=%d decoded=%t mime=%q usable=%t", family, variant.name, name, originalSize, didDecode, mime, usable)
			asset := ppt.FontAsset{Family: family, Style: variant.style, Weight: variant.weight, FileName: filepath.Base(name), Obfuscated: !usable,
				AssetURL: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw)}
			out = append(out, asset)
		}
	}
	return out
}

// Office obfuscated font parts XOR the first 32 bytes with a 16-byte GUID.
// Different producers place that GUID on different embedded-font nodes, so
// accept the common attribute spellings and both byte orders used in files in
// the wild.
func embeddedFontKey(n *node) []byte {
	var walk func(*node) []byte
	walk = func(cur *node) []byte {
		if cur == nil {
			return nil
		}
		// Producers use both attributes (fontKey/guid) and a child text node
		// for this value, so inspect the whole embeddedFont subtree.
		values := []string{cur.Text}
		for _, attr := range cur.Attr {
			// Standard attributes are named fontKey/guid, while some producers
			// use a private name. A GUID-shaped value is unambiguous here.
			values = append(values, attr.Value)
		}
		for _, value := range values {
			value = strings.Trim(strings.TrimSpace(value), "{}")
			value = strings.TrimPrefix(value, "urn:uuid:")
			value = strings.ReplaceAll(value, "-", "")
			if len(value) == 32 {
				if key, err := hex.DecodeString(value); err == nil {
					return key
				}
			}
		}
		for _, child := range cur.Kids {
			if key := walk(child); key != nil {
				return key
			}
		}
		return nil
	}
	return walk(n)
}

func decodeObfuscatedFont(raw, key []byte) ([]byte, bool) {
	if len(key) != 16 || len(raw) < 32 {
		return raw, false
	}
	// The key is a UUID, and producers disagree about whether the first
	// three UUID fields are encoded in their wire (little-endian) order.  A
	// few producers also reverse the complete 16-byte key.  Try the standard
	// UUID transformation plus the historical variants before giving up.
	variants := [][]byte{
		append([]byte(nil), key...),
		reverseBytes(key),
		uuidWireKey(key),
		reverseBytes(uuidWireKey(key)),
	}
	seen := map[string]bool{}
	for _, b := range variants {
		fingerprint := string(b)
		if seen[fingerprint] {
			continue
		}
		seen[fingerprint] = true
		candidate := append([]byte(nil), raw...)
		for j := 0; j < 32; j++ {
			candidate[j] ^= b[j%16]
		}
		if _, usable := fontMIME(candidate); usable {
			return candidate, true
		}
	}
	return raw, false
}

// Canva exports .fntdata files with a small BSGP/container header in front of
// an otherwise standard OpenType/TrueType stream. Locate the first plausible
// font signature rather than treating the private container header as font
// data. The scan is deliberately bounded: a real font header is near the
// beginning and this avoids accidentally finding a signature in glyph data.
func unwrapFontContainer(raw []byte) ([]byte, int) {
	if _, ok := fontMIME(raw); ok {
		return raw, 0
	}
	limit := len(raw)
	if limit > 4096 {
		limit = 4096
	}
	for i := 1; i+4 <= limit; i++ {
		if _, ok := fontMIME(raw[i:]); ok {
			return raw[i:], i
		}
	}
	return raw, 0
}

func reverseBytes(in []byte) []byte {
	out := append([]byte(nil), in...)
	for left, right := 0, len(out)-1; left < right; left, right = left+1, right-1 {
		out[left], out[right] = out[right], out[left]
	}
	return out
}

// uuidWireKey converts the textual UUID byte order used by OOXML to the
// little-endian representation used by the Windows GUID obfuscation rule.
func uuidWireKey(in []byte) []byte {
	if len(in) != 16 {
		return append([]byte(nil), in...)
	}
	out := append([]byte(nil), in...)
	for left, right := 0, 3; left < right; left, right = left+1, right-1 {
		out[left], out[right] = out[right], out[left]
	}
	for left, right := 4, 5; left < right; left, right = left+1, right-1 {
		out[left], out[right] = out[right], out[left]
	}
	for left, right := 6, 7; left < right; left, right = left+1, right-1 {
		out[left], out[right] = out[right], out[left]
	}
	return out
}

func zipPart(z *zip.Reader, name string) []byte {
	for _, f := range z.File {
		if f.Name == name {
			b, err := f.Open()
			if err != nil {
				return nil
			}
			raw, _ := io.ReadAll(b)
			b.Close()
			return raw
		}
	}
	return nil
}

func fontMIME(raw []byte) (string, bool) {
	if len(raw) < 4 {
		return "", false
	}
	sig := string(raw[:4])
	switch sig {
	case "OTTO":
		return "font/otf", true
	case "ttcf":
		// TrueType collections are valid OpenType containers. Browsers that
		// support collections can load them through FontFace as well.
		return "font/ttf", true
	case "wOFF":
		return "font/woff", true
	case "wOF2":
		return "font/woff2", true
	case "\x00\x01\x00\x00", "true", "typ1":
		return "font/ttf", true
	default:
		// Office may store license-obfuscated font data. Preserve its metadata
		// in JSON, but do not hand undecoded bytes to the browser as a font.
		return "application/vnd.openxmlformats-officedocument.obfuscatedFont", false
	}
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
