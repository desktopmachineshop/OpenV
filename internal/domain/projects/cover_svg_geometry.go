package projects

import (
	"image"
	"image/color"
	"math"
	"sort"
	"strconv"
	"strings"
)

// The geometry of cover drawings (cover_svg.go): transforms, the shapes and
// path data an element describes, strokes as filled outlines, and the
// gradients a shape is painted with.

// mat is an affine transform: x' = a x + c y + e, y' = b x + d y + f.
type mat [6]float64

var identity = mat{1, 0, 0, 1, 0, 0}

// mul is m after n: n applies first.
func (m mat) mul(n mat) mat {
	return mat{
		m[0]*n[0] + m[2]*n[1], m[1]*n[0] + m[3]*n[1],
		m[0]*n[2] + m[2]*n[3], m[1]*n[2] + m[3]*n[3],
		m[0]*n[4] + m[2]*n[5] + m[4], m[1]*n[4] + m[3]*n[5] + m[5],
	}
}

func (m mat) apply(x, y float64) (float64, float64) {
	return m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]
}

// inverse is m's inverse, the identity for a singular m.
func (m mat) inverse() mat {
	det := m[0]*m[3] - m[1]*m[2]
	if math.Abs(det) < 1e-12 {
		return identity
	}
	return mat{m[3] / det, -m[1] / det, -m[2] / det, m[0] / det,
		(m[2]*m[5] - m[3]*m[4]) / det, (m[1]*m[4] - m[0]*m[5]) / det}
}

// numbers reads the numbers of an attribute: commas, spaces, signs and
// exponents as SVG writes them ("10-5" is 10 and -5, "1.5.5" is 1.5 and .5).
func numbers(s string) []float64 {
	var out []float64
	i := 0
	for i < len(s) {
		c := s[i]
		if !(c == '-' || c == '+' || c == '.' || (c >= '0' && c <= '9')) {
			i++
			continue
		}
		j, dot, exp := i+1, c == '.', false
		for j < len(s) {
			d := s[j]
			switch {
			case d >= '0' && d <= '9':
			case d == '.' && !dot && !exp:
				dot = true
			case (d == 'e' || d == 'E') && !exp:
				exp = true
				if j+1 < len(s) && (s[j+1] == '-' || s[j+1] == '+') {
					j++
				}
			default:
				goto done
			}
			j++
		}
	done:
		if f, err := strconv.ParseFloat(s[i:j], 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			out = append(out, f)
		}
		i = j
	}
	return out
}

// parseTransform reads a transform list: matrix, translate, scale, rotate,
// skewX and skewY, applied left to right as SVG nests them.
func parseTransform(s string) mat {
	m := identity
	for _, part := range strings.Split(s, ")") {
		name, args, ok := strings.Cut(part, "(")
		if !ok {
			continue
		}
		v := numbers(args)
		arg := func(i int, def float64) float64 {
			if i < len(v) {
				return v[i]
			}
			return def
		}
		var t mat
		switch strings.TrimSpace(strings.Trim(name, ", ")) {
		case "matrix":
			if len(v) < 6 {
				continue
			}
			t = mat{v[0], v[1], v[2], v[3], v[4], v[5]}
		case "translate":
			t = mat{1, 0, 0, 1, arg(0, 0), arg(1, 0)}
		case "scale":
			sx := arg(0, 1)
			t = mat{sx, 0, 0, arg(1, sx), 0, 0}
		case "rotate":
			a := arg(0, 0) * math.Pi / 180
			cx, cy := arg(1, 0), arg(2, 0)
			t = mat{1, 0, 0, 1, cx, cy}.mul(mat{math.Cos(a), math.Sin(a), -math.Sin(a), math.Cos(a), 0, 0}).mul(mat{1, 0, 0, 1, -cx, -cy})
		case "skewX":
			t = mat{1, 0, math.Tan(arg(0, 0) * math.Pi / 180), 1, 0, 0}
		case "skewY":
			t = mat{1, math.Tan(arg(0, 0) * math.Pi / 180), 0, 1, 0, 0}
		default:
			continue
		}
		m = m.mul(t)
	}
	return m
}

// svgCmd is one absolute path command: M and L carry a point, C two control
// points and the end, Z nothing.
type svgCmd struct {
	op  byte
	pts []float64
}

// svgPath is a shape's outline. fillable is false for a line, which has no
// inside.
type svgPath struct {
	cmds     []svgCmd
	fillable bool
}

func (p *svgPath) add(op byte, pts ...float64) { p.cmds = append(p.cmds, svgCmd{op, pts}) }

// transform is p with every point through m (a Bézier stays one under an
// affine transform).
func (p *svgPath) transform(m mat) *svgPath {
	out := &svgPath{fillable: p.fillable, cmds: make([]svgCmd, len(p.cmds))}
	for i, c := range p.cmds {
		pts := make([]float64, len(c.pts))
		for j := 0; j+1 < len(c.pts); j += 2 {
			pts[j], pts[j+1] = m.apply(c.pts[j], c.pts[j+1])
		}
		out.cmds[i] = svgCmd{c.op, pts}
	}
	return out
}

// bounds is the box of every point, control points included: x, y, w, h.
func (p *svgPath) bounds() [4]float64 {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, c := range p.cmds {
		for j := 0; j+1 < len(c.pts); j += 2 {
			minX, maxX = math.Min(minX, c.pts[j]), math.Max(maxX, c.pts[j])
			minY, maxY = math.Min(minY, c.pts[j+1]), math.Max(maxY, c.pts[j+1])
		}
	}
	if minX > maxX {
		return [4]float64{}
	}
	return [4]float64{minX, minY, maxX - minX, maxY - minY}
}

// deviceRect is the pixel box the path covers, a pixel of margin around it.
func (p *svgPath) deviceRect() image.Rectangle {
	b := p.bounds()
	if b[2] == 0 && b[3] == 0 && len(p.cmds) == 0 {
		return image.Rectangle{}
	}
	clamp := func(v float64) int { return int(math.Max(-1e6, math.Min(1e6, v))) }
	return image.Rect(clamp(math.Floor(b[0]))-1, clamp(math.Floor(b[1]))-1,
		clamp(math.Ceil(b[0]+b[2]))+1, clamp(math.Ceil(b[1]+b[3]))+1)
}

// shapePath is the outline of a shape element, nil for any other element
// and for a shape with nothing to draw.
func (r *svgRenderer) shapePath(n *svgNode) *svgPath {
	l := func(name string, ref float64) float64 {
		v, _ := parseLength(n.attrs[name], ref)
		return v
	}
	w, h := r.vbW, r.vbH
	diag := math.Hypot(w, h) / math.Sqrt2
	p := &svgPath{fillable: true}
	switch n.name {
	case "rect":
		x, y, rw, rh := l("x", w), l("y", h), l("width", w), l("height", h)
		if rw <= 0 || rh <= 0 {
			return nil
		}
		rx, okx := parseLength(n.attrs["rx"], w)
		ry, oky := parseLength(n.attrs["ry"], h)
		if !okx {
			rx = ry
		}
		if !oky {
			ry = rx
		}
		rx, ry = math.Max(0, math.Min(rx, rw/2)), math.Max(0, math.Min(ry, rh/2))
		if rx == 0 || ry == 0 {
			p.add('M', x, y)
			p.add('L', x+rw, y)
			p.add('L', x+rw, y+rh)
			p.add('L', x, y+rh)
			p.add('Z')
			return p
		}
		p.add('M', x+rx, y)
		p.add('L', x+rw-rx, y)
		arcTo(p, x+rw-rx, y, rx, ry, 0, false, true, x+rw, y+ry)
		p.add('L', x+rw, y+rh-ry)
		arcTo(p, x+rw, y+rh-ry, rx, ry, 0, false, true, x+rw-rx, y+rh)
		p.add('L', x+rx, y+rh)
		arcTo(p, x+rx, y+rh, rx, ry, 0, false, true, x, y+rh-ry)
		p.add('L', x, y+ry)
		arcTo(p, x, y+ry, rx, ry, 0, false, true, x+rx, y)
		p.add('Z')
	case "circle", "ellipse":
		cx, cy := l("cx", w), l("cy", h)
		rx, ry := l("rx", w), l("ry", h)
		if n.name == "circle" {
			rx = l("r", diag)
			ry = rx
		}
		if rx <= 0 || ry <= 0 {
			return nil
		}
		p.add('M', cx+rx, cy)
		arcTo(p, cx+rx, cy, rx, ry, 0, false, true, cx-rx, cy)
		arcTo(p, cx-rx, cy, rx, ry, 0, false, true, cx+rx, cy)
		p.add('Z')
	case "line":
		p.fillable = false
		p.add('M', l("x1", w), l("y1", h))
		p.add('L', l("x2", w), l("y2", h))
	case "polyline", "polygon":
		v := numbers(n.attrs["points"])
		if len(v) < 4 {
			return nil
		}
		p.add('M', v[0], v[1])
		for i := 2; i+1 < len(v); i += 2 {
			p.add('L', v[i], v[i+1])
		}
		if n.name == "polygon" {
			p.add('Z')
		}
	case "path":
		p = parsePathData(n.attrs["d"])
		if p == nil {
			return nil
		}
	default:
		return nil
	}
	return p
}

// parsePathData reads a path's d attribute into absolute commands, every
// curve as cubics. Reading stops at the first command it cannot read, as
// SVG renders a path up to its first error.
func parsePathData(d string) *svgPath {
	p := &svgPath{fillable: true}
	var cx, cy, sx, sy, lcx, lcy float64 // current point, subpath start, last control point
	var last byte
	i := 0
	for i < len(d) {
		ch := d[i]
		if strings.IndexByte("MmLlHhVvCcSsQqTtAaZz", ch) < 0 {
			i++
			continue
		}
		j := i + 1
		for j < len(d) && strings.IndexByte("MmLlHhVvCcSsQqTtAaZz", d[j]) < 0 {
			j++
		}
		args := numbers(d[i+1 : j])
		i = j
		rel := ch >= 'a'
		op := ch &^ 0x20
		if op == 'Z' {
			p.add('Z')
			cx, cy = sx, sy
			last = 'Z'
			continue
		}
		size := map[byte]int{'M': 2, 'L': 2, 'H': 1, 'V': 1, 'C': 6, 'S': 4, 'Q': 4, 'T': 2, 'A': 7}[op]
		if len(args) < size {
			break
		}
		for k := 0; k+size <= len(args); k += size {
			a := args[k : k+size]
			ox, oy := 0.0, 0.0
			if rel {
				ox, oy = cx, cy
			}
			switch op {
			case 'M':
				cx, cy = a[0]+ox, a[1]+oy
				if k == 0 {
					p.add('M', cx, cy)
					sx, sy = cx, cy
				} else {
					p.add('L', cx, cy) // further pairs after M are lines
				}
				lcx, lcy = cx, cy
			case 'L', 'H', 'V', 'T':
				nx, ny := cx, cy
				switch op {
				case 'L':
					nx, ny = a[0]+ox, a[1]+oy
				case 'H':
					nx = a[0] + ox
				case 'V':
					ny = a[0] + oy
				case 'T':
					nx, ny = a[0]+ox, a[1]+oy
					qx, qy := cx, cy
					if last == 'Q' || last == 'T' {
						qx, qy = 2*cx-lcx, 2*cy-lcy
					}
					quadTo(p, cx, cy, qx, qy, nx, ny)
					lcx, lcy = qx, qy
					cx, cy, last = nx, ny, 'T'
					continue
				}
				p.add('L', nx, ny)
				cx, cy = nx, ny
				lcx, lcy = cx, cy
			case 'C', 'S':
				var x1, y1 float64
				var rest []float64
				if op == 'C' {
					x1, y1, rest = a[0]+ox, a[1]+oy, a[2:]
				} else {
					x1, y1, rest = cx, cy, a
					if last == 'C' || last == 'S' {
						x1, y1 = 2*cx-lcx, 2*cy-lcy
					}
				}
				x2, y2, x, y := rest[0]+ox, rest[1]+oy, rest[2]+ox, rest[3]+oy
				p.add('C', x1, y1, x2, y2, x, y)
				lcx, lcy, cx, cy = x2, y2, x, y
			case 'Q':
				qx, qy, x, y := a[0]+ox, a[1]+oy, a[2]+ox, a[3]+oy
				quadTo(p, cx, cy, qx, qy, x, y)
				lcx, lcy, cx, cy = qx, qy, x, y
			case 'A':
				x, y := a[5]+ox, a[6]+oy
				arcTo(p, cx, cy, a[0], a[1], a[2], a[3] != 0, a[4] != 0, x, y)
				cx, cy = x, y
				lcx, lcy = cx, cy
			}
			last = op
		}
	}
	if len(p.cmds) == 0 || p.cmds[0].op != 'M' {
		return nil
	}
	return p
}

// quadTo adds a quadratic Bézier as the cubic it equals.
func quadTo(p *svgPath, x0, y0, qx, qy, x, y float64) {
	p.add('C', x0+2.0/3*(qx-x0), y0+2.0/3*(qy-y0), x+2.0/3*(qx-x), y+2.0/3*(qy-y), x, y)
}

// arcTo adds SVG's elliptical arc from (x0, y0) to (x, y) as cubics, by the
// endpoint-to-centre conversion of SVG 1.1 appendix F.6.
func arcTo(p *svgPath, x0, y0, rx, ry, angle float64, large, sweep bool, x, y float64) {
	if x0 == x && y0 == y {
		return
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	if rx == 0 || ry == 0 {
		p.add('L', x, y)
		return
	}
	phi := angle * math.Pi / 180
	cos, sin := math.Cos(phi), math.Sin(phi)
	dx, dy := (x0-x)/2, (y0-y)/2
	x1p, y1p := cos*dx+sin*dy, -sin*dx+cos*dy
	if l := x1p*x1p/(rx*rx) + y1p*y1p/(ry*ry); l > 1 {
		s := math.Sqrt(l)
		rx, ry = rx*s, ry*s
	}
	num := rx*rx*ry*ry - rx*rx*y1p*y1p - ry*ry*x1p*x1p
	den := rx*rx*y1p*y1p + ry*ry*x1p*x1p
	coef := math.Sqrt(math.Max(0, num/den))
	if large == sweep {
		coef = -coef
	}
	cxp, cyp := coef*rx*y1p/ry, -coef*ry*x1p/rx
	cx, cy := cos*cxp-sin*cyp+(x0+x)/2, sin*cxp+cos*cyp+(y0+y)/2
	ang := func(ux, uy, vx, vy float64) float64 { return math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy) }
	t1 := ang(1, 0, (x1p-cxp)/rx, (y1p-cyp)/ry)
	dt := ang((x1p-cxp)/rx, (y1p-cyp)/ry, (-x1p-cxp)/rx, (-y1p-cyp)/ry)
	if !sweep && dt > 0 {
		dt -= 2 * math.Pi
	} else if sweep && dt < 0 {
		dt += 2 * math.Pi
	}
	n := int(math.Ceil(math.Abs(dt) / (math.Pi / 2)))
	step := dt / float64(n)
	k := 4.0 / 3 * math.Tan(step/4)
	pt := func(t float64) (float64, float64) {
		ex, ey := rx*math.Cos(t), ry*math.Sin(t)
		return cos*ex - sin*ey + cx, sin*ex + cos*ey + cy
	}
	deriv := func(t float64) (float64, float64) {
		ex, ey := -rx*math.Sin(t), ry*math.Cos(t)
		return cos*ex - sin*ey, sin*ex + cos*ey
	}
	t := t1
	for i := 0; i < n; i++ {
		ax, ay := pt(t)
		bx, by := pt(t + step)
		dax, day := deriv(t)
		dbx, dby := deriv(t + step)
		p.add('C', ax+k*dax, ay+k*day, bx-k*dbx, by-k*dby, bx, by)
		t += step
	}
}

// flatten is a device-space path as polylines, each with whether it closes.
func (p *svgPath) flatten() (lines [][]float64, closed []bool) {
	var cur []float64
	flush := func(close bool) {
		if len(cur) >= 4 {
			lines, closed = append(lines, cur), append(closed, close)
		}
		cur = nil
	}
	for _, c := range p.cmds {
		switch c.op {
		case 'M':
			flush(false)
			cur = []float64{c.pts[0], c.pts[1]}
		case 'L':
			cur = append(cur, c.pts[0], c.pts[1])
		case 'C':
			if len(cur) < 2 {
				continue
			}
			x0, y0 := cur[len(cur)-2], cur[len(cur)-1]
			length := math.Hypot(c.pts[0]-x0, c.pts[1]-y0) + math.Hypot(c.pts[2]-c.pts[0], c.pts[3]-c.pts[1]) +
				math.Hypot(c.pts[4]-c.pts[2], c.pts[5]-c.pts[3])
			n := int(math.Max(2, math.Min(64, length/4)))
			for i := 1; i <= n; i++ {
				t := float64(i) / float64(n)
				u := 1 - t
				a, b, cc, d := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
				cur = append(cur, a*x0+b*c.pts[0]+cc*c.pts[2]+d*c.pts[4], a*y0+b*c.pts[1]+cc*c.pts[3]+d*c.pts[5])
			}
		case 'Z':
			if len(cur) >= 2 {
				start := cur[:2]
				cur = append(cur, start[0], start[1])
			}
			flush(true)
		}
	}
	flush(false)
	return lines, closed
}

// stroke draws a device-space path's outline of width w: a quad along each
// segment and a disc at each joint, all wound the same way so that where
// they overlap the rasterizer adds them rather than cancelling them.
func (r *svgRenderer) stroke(p *svgPath, w float64, src image.Image) {
	hw := w / 2
	out := &svgPath{fillable: true}
	poly := func(pts []float64) {
		if signedArea(pts) < 0 {
			for i, j := 0, len(pts)-2; i < j; i, j = i+2, j-2 {
				pts[i], pts[j], pts[i+1], pts[j+1] = pts[j], pts[i], pts[j+1], pts[i+1]
			}
		}
		out.add('M', pts[0], pts[1])
		for i := 2; i+1 < len(pts); i += 2 {
			out.add('L', pts[i], pts[i+1])
		}
		out.add('Z')
	}
	disc := func(x, y float64) {
		n := int(math.Max(8, math.Min(48, hw)))
		pts := make([]float64, 0, 2*n)
		for i := 0; i < n; i++ {
			a := 2 * math.Pi * float64(i) / float64(n)
			pts = append(pts, x+hw*math.Cos(a), y+hw*math.Sin(a))
		}
		poly(pts)
	}
	lines, closed := p.flatten()
	for li, pts := range lines {
		for i := 0; i+3 < len(pts); i += 2 {
			x0, y0, x1, y1 := pts[i], pts[i+1], pts[i+2], pts[i+3]
			l := math.Hypot(x1-x0, y1-y0)
			if l == 0 {
				continue
			}
			nx, ny := -(y1-y0)/l*hw, (x1-x0)/l*hw
			poly([]float64{x0 + nx, y0 + ny, x1 + nx, y1 + ny, x1 - nx, y1 - ny, x0 - nx, y0 - ny})
			if hw >= 1.5 && (i > 0 || closed[li]) {
				disc(x0, y0)
			}
		}
	}
	if len(out.cmds) > 0 {
		r.fill(out, src)
	}
}

func signedArea(pts []float64) float64 {
	a := 0.0
	for i := 0; i+1 < len(pts); i += 2 {
		j := (i + 2) % len(pts)
		a += pts[i]*pts[j+1] - pts[j]*pts[i+1]
	}
	return a / 2
}

// gradientStop is one colour of a gradient at its offset, premultiplied.
type gradientStop struct {
	at         float64
	r, g, b, a float64
}

// gradientImage paints a linear or radial gradient: each pixel is taken back
// through the shape's transform into the gradient's own space.
type gradientImage struct {
	toGradient        mat
	radial            bool
	x1, y1, x2, y2, r float64
	stops             []gradientStop
}

func (g *gradientImage) ColorModel() color.Model { return color.RGBAModel }
func (g *gradientImage) Bounds() image.Rectangle { return image.Rect(-1e9, -1e9, 1e9, 1e9) }

func (g *gradientImage) At(px, py int) color.Color {
	x, y := g.toGradient.apply(float64(px)+0.5, float64(py)+0.5)
	var t float64
	if g.radial {
		t = math.Hypot(x-g.x1, y-g.y1) / g.r
	} else {
		dx, dy := g.x2-g.x1, g.y2-g.y1
		t = ((x-g.x1)*dx + (y-g.y1)*dy) / (dx*dx + dy*dy)
	}
	t = math.Max(0, math.Min(1, t))
	s := g.stops
	i := sort.Search(len(s), func(i int) bool { return s[i].at >= t })
	var c gradientStop
	switch {
	case i == 0:
		c = s[0]
	case i == len(s):
		c = s[len(s)-1]
	default:
		a, b := s[i-1], s[i]
		f := 0.0
		if b.at > a.at {
			f = (t - a.at) / (b.at - a.at)
		}
		c = gradientStop{0, a.r + (b.r-a.r)*f, a.g + (b.g-a.g)*f, a.b + (b.b-a.b)*f, a.a + (b.a-a.a)*f}
	}
	return c.rgba()
}

func (c gradientStop) rgba() color.RGBA {
	return color.RGBA{uint8(c.r*255 + 0.5), uint8(c.g*255 + 0.5), uint8(c.b*255 + 0.5), uint8(c.a*255 + 0.5)}
}

// gradient builds a shape's gradient paint from its element: its stops (or
// those of the gradient it names by href), its geometry in the units it says,
// and its own transform.
func (r *svgRenderer) gradient(n *svgNode, opacity float64, ctm mat, bbox [4]float64) image.Image {
	stopsOf := n
	if len(n.children) == 0 {
		href := n.attrs["href"]
		if g, ok := r.gradients[strings.TrimPrefix(href, "#")]; ok {
			stopsOf = g
		}
	}
	var stops []gradientStop
	for _, s := range stopsOf.children {
		if s.name != "stop" {
			continue
		}
		at, _ := parseUnit(s.attrs["offset"])
		if len(stops) > 0 && at < stops[len(stops)-1].at {
			at = stops[len(stops)-1].at
		}
		c := color.NRGBA{A: 255}
		if v, ok := s.attr("stop-color"); ok {
			if cc, ok := parseColor(v); ok {
				c = cc
			}
		}
		a := float64(c.A) / 255 * opacity
		if v, ok := s.attr("stop-opacity"); ok {
			if f, ok := parseUnit(v); ok {
				a *= f
			}
		}
		stops = append(stops, gradientStop{at, float64(c.R) / 255 * a, float64(c.G) / 255 * a, float64(c.B) / 255 * a, a})
	}
	if len(stops) == 0 {
		return nil
	}
	if len(stops) == 1 {
		return image.NewUniform(stops[0].rgba())
	}
	userSpace := n.attrs["gradientUnits"] == "userSpaceOnUse"
	ref := 1.0
	if userSpace {
		ref = math.Max(r.vbW, r.vbH)
	}
	// A coordinate is a fraction of the shape's box (objectBoundingBox, the
	// default) or a user-space length; a percentage is of the box, or of the
	// drawing's larger side in user space.
	num := func(name string, def float64) float64 {
		v, ok := n.attrs[name]
		if !ok {
			return def * ref
		}
		if v = strings.TrimSpace(v); strings.HasSuffix(v, "%") {
			f, _ := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64)
			return f / 100 * ref
		}
		f, _ := parseLength(v, ref)
		return f
	}
	g := &gradientImage{radial: n.name == "radialGradient", stops: stops}
	if g.radial {
		g.x1, g.y1, g.r = num("cx", 0.5), num("cy", 0.5), num("r", 0.5)
		if g.r <= 0 {
			return image.NewUniform(stops[len(stops)-1].rgba())
		}
	} else {
		g.x1, g.y1, g.x2, g.y2 = num("x1", 0), num("y1", 0), num("x2", 1), num("y2", 0)
		if g.x1 == g.x2 && g.y1 == g.y2 {
			g.x2 += 1e-6
		}
	}
	// Device pixel -> user space -> (bounding-box units) -> gradient space.
	toUser := ctm.inverse()
	space := identity
	if !userSpace {
		if bbox[2] <= 0 || bbox[3] <= 0 {
			return nil
		}
		space = mat{1 / bbox[2], 0, 0, 1 / bbox[3], -bbox[0] / bbox[2], -bbox[1] / bbox[3]}
	}
	gt := identity
	if t, ok := n.attrs["gradientTransform"]; ok {
		gt = parseTransform(t)
	}
	g.toGradient = gt.inverse().mul(space).mul(toUser)
	return g
}
