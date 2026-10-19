package genppt

import (
	"regexp"
	"strconv"
	"strings"
)

// svgPathToCustGeom converts the commonly emitted SVG commands to DrawingML.
// Coordinates are normalized to the 100000x100000 path space used by Office.
func svgPathToCustGeom(path, viewBox string) string {
	minx, miny, w, h := 0.0, 0.0, 100.0, 100.0
	if p := strings.Fields(strings.ReplaceAll(viewBox, ",", " ")); len(p) >= 4 {
		minx, _ = strconv.ParseFloat(p[0], 64)
		miny, _ = strconv.ParseFloat(p[1], 64)
		w, _ = strconv.ParseFloat(p[2], 64)
		h, _ = strconv.ParseFloat(p[3], 64)
	}
	if w == 0 {
		w = 100
	}
	if h == 0 {
		h = 100
	}
	norm := func(x, y float64) (int, int) { return int((x - minx) / w * 100000), int((y - miny) / h * 100000) }
	re := regexp.MustCompile(`[A-Za-z]|[-+]?(?:\d*\.\d+|\d+\.?)(?:[eE][-+]?\d+)?`)
	tok := re.FindAllString(path, -1)
	var b strings.Builder
	b.WriteString(`<a:path w="100000" h="100000">`)
	cmd := ""
	i := 0
	var x, y, sx, sy float64
	num := func() float64 {
		if i >= len(tok) {
			return 0
		}
		v, _ := strconv.ParseFloat(tok[i], 64)
		i++
		return v
	}
	pt := func(px, py float64) {
		X, Y := norm(px, py)
		b.WriteString(`<a:pt x="` + itoa(X) + `" y="` + itoa(Y) + `"/>`)
	}
	for i < len(tok) {
		if len(tok[i]) == 1 && strings.ContainsAny(tok[i], "MmLlHhVvCcQqZz") {
			cmd = tok[i]
			i++
		}
		if cmd == "" {
			i++
			continue
		}
		rel := cmd == strings.ToLower(cmd)
		switch strings.ToLower(cmd) {
		case "m", "l":
			x1, y1 := num(), num()
			if rel {
				x1 += x
				y1 += y
			}
			if strings.ToLower(cmd) == "m" {
				b.WriteString(`<a:moveTo><a:pt x="`)
				X, Y := norm(x1, y1)
				b.WriteString(itoa(X) + `" y="` + itoa(Y) + `"/></a:moveTo>`)
				sx, sy = x1, y1
			} else {
				b.WriteString(`<a:lnTo>`)
				pt(x1, y1)
				b.WriteString(`</a:lnTo>`)
			}
			x, y = x1, y1
			if strings.ToLower(cmd) == "m" {
				cmd = map[bool]string{true: "l", false: "L"}[rel]
			}
		case "h":
			x1 := num()
			if rel {
				x1 += x
			}
			b.WriteString(`<a:lnTo>`)
			pt(x1, y)
			b.WriteString(`</a:lnTo>`)
			x = x1
		case "v":
			y1 := num()
			if rel {
				y1 += y
			}
			b.WriteString(`<a:lnTo>`)
			pt(x, y1)
			b.WriteString(`</a:lnTo>`)
			y = y1
		case "c":
			x1, y1, x2, y2, x3, y3 := num(), num(), num(), num(), num(), num()
			if rel {
				x1 += x
				y1 += y
				x2 += x
				y2 += y
				x3 += x
				y3 += y
			}
			b.WriteString(`<a:cubicBezTo><a:pt`)
			X, Y := norm(x1, y1)
			b.WriteString(` x="` + itoa(X) + `" y="` + itoa(Y) + `"/><a:pt`)
			X, Y = norm(x2, y2)
			b.WriteString(` x="` + itoa(X) + `" y="` + itoa(Y) + `"/><a:pt`)
			X, Y = norm(x3, y3)
			b.WriteString(` x="` + itoa(X) + `" y="` + itoa(Y) + `"/></a:cubicBezTo>`)
			x, y = x3, y3
		case "q":
			x1, y1, x2, y2 := num(), num(), num(), num()
			if rel {
				x1 += x
				y1 += y
				x2 += x
				y2 += y
			}
			b.WriteString(`<a:quadBezTo><a:pt`)
			X, Y := norm(x1, y1)
			b.WriteString(` x="` + itoa(X) + `" y="` + itoa(Y) + `"/><a:pt`)
			X, Y = norm(x2, y2)
			b.WriteString(` x="` + itoa(X) + `" y="` + itoa(Y) + `"/></a:quadBezTo>`)
			x, y = x2, y2
		case "z":
			b.WriteString(`<a:close/>`)
			x, y = sx, sy
			cmd = ""
		default:
			i++
		}
	}
	b.WriteString(`</a:path>`)
	return b.String()
}
