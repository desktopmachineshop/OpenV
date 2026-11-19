package projects

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"strconv"
	"strings"

	"golang.org/x/image/vector"
)

// Drawing a project's cover from SVG: an agent cannot make pixels, but it
// can write a drawing, so the server renders the drawing to a PNG and
// stores that. The SVG itself is never served (it could carry script); only
// the pixels it renders to are. This is a small renderer of its own, over
// golang.org/x/image/vector, for what an illustration needs: rect, circle,
// ellipse, line, polyline, polygon and path (arcs included), in groups with
// transforms, filled and stroked in colours or linear and radial gradients,
// with opacity. Text, embedded images, filters, masks, clip paths and
// patterns are left out, and nothing is ever fetched.

// Cover drawings render at coverImageSize's 1536x1024; a viewBox of another
// shape is scaled to cover it, centred, as a cover photo is cropped.
const (
	coverWidth, coverHeight = 1536, 1024
	// MaxCoverSVGBytes caps a drawing's source.
	MaxCoverSVGBytes = 256 * 1024
	maxSVGElements   = 4000
	maxSVGDepth      = 32
)

// ErrCoverSVG refuses a drawing that cannot be rendered; the API answers 400
// with its reason.
var ErrCoverSVG = errors.New("the cover drawing cannot be rendered")

func svgError(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", ErrCoverSVG, fmt.Sprintf(format, args...))
}

// svgNode is one element of the parsed drawing.
type svgNode struct {
	name     string
	attrs    map[string]string
	children []*svgNode
}

// attr is the element's attribute or style property, the style winning.
func (n *svgNode) attr(name string) (string, bool) {
	if style, ok := n.attrs["style"]; ok {
		for _, decl := range strings.Split(style, ";") {
			k, v, ok := strings.Cut(decl, ":")
			if ok && strings.TrimSpace(k) == name {
				return strings.TrimSpace(v), true
			}
		}
	}
	v, ok := n.attrs[name]
	return strings.TrimSpace(v), ok
}

func parseSVG(src string) (*svgNode, error) {
	if len(src) > MaxCoverSVGBytes {
		return nil, svgError("the drawing is larger than %d KB", MaxCoverSVGBytes/1024)
	}
	dec := xml.NewDecoder(strings.NewReader(src))
	dec.Strict = false
	var root *svgNode
	var stack []*svgNode
	count := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, svgError("it is not well-formed XML (%v)", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			count++
			if count > maxSVGElements {
				return nil, svgError("it has more than %d elements", maxSVGElements)
			}
			if len(stack) >= maxSVGDepth {
				return nil, svgError("its elements nest more than %d deep", maxSVGDepth)
			}
			n := &svgNode{name: t.Name.Local, attrs: map[string]string{}}
			for _, a := range t.Attr {
				n.attrs[a.Name.Local] = a.Value
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, svgError("it has more than one root element")
				}
				root = n
			} else {
				p := stack[len(stack)-1]
				p.children = append(p.children, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if root == nil || root.name != "svg" {
		return nil, svgError("its root element is not an svg element")
	}
	return root, nil
}

// paint is what a fill or stroke is: nothing, a colour or a gradient.
type paint struct {
	none bool
	c    color.NRGBA
	ref  string // a gradient's id
}

// svgStyle is the inherited presentation of an element.
type svgStyle struct {
	fill, stroke                        paint
	strokeWidth                         float64
	opacity, fillOpacity, strokeOpacity float64
}

func defaultStyle() svgStyle {
	return svgStyle{fill: paint{c: color.NRGBA{A: 255}}, stroke: paint{none: true}, strokeWidth: 1,
		opacity: 1, fillOpacity: 1, strokeOpacity: 1}
}

// inherit applies n's own presentation over the parent's.
func (s svgStyle) inherit(n *svgNode) svgStyle {
	if v, ok := n.attr("fill"); ok {
		s.fill = parsePaint(v, s.fill)
	}
	if v, ok := n.attr("stroke"); ok {
		s.stroke = parsePaint(v, s.stroke)
	}
	if v, ok := n.attr("stroke-width"); ok {
		if w, ok := parseLength(v, 1); ok && w >= 0 {
			s.strokeWidth = w
		}
	}
	unit := func(name string, into *float64, multiply bool) {
		if v, ok := n.attr(name); ok {
			if f, ok := parseUnit(v); ok {
				if multiply {
					*into *= f
				} else {
					*into = f
				}
			}
		}
	}
	// opacity compounds down the tree; the fill and stroke opacities are
	// inherited properties, overridden.
	unit("opacity", &s.opacity, true)
	unit("fill-opacity", &s.fillOpacity, false)
	unit("stroke-opacity", &s.strokeOpacity, false)
	return s
}

// parseUnit reads 0.5 or 50% as a fraction, clamped to 0..1.
func parseUnit(v string) (float64, bool) {
	v = strings.TrimSpace(v)
	pct := strings.HasSuffix(v, "%")
	f, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64)
	if err != nil {
		return 0, false
	}
	if pct {
		f /= 100
	}
	return math.Max(0, math.Min(1, f)), true
}

// parseLength reads a number with an optional px, or a percentage of ref.
func parseLength(v string, ref float64) (float64, bool) {
	v = strings.TrimSpace(v)
	if strings.HasSuffix(v, "%") {
		f, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64)
		return f / 100 * ref, err == nil
	}
	f, err := strconv.ParseFloat(strings.TrimSuffix(v, "px"), 64)
	return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
}

var namedColors = map[string]color.NRGBA{
	"black": {0, 0, 0, 255}, "white": {255, 255, 255, 255}, "red": {255, 0, 0, 255}, "green": {0, 128, 0, 255},
	"blue": {0, 0, 255, 255}, "yellow": {255, 255, 0, 255}, "orange": {255, 165, 0, 255}, "purple": {128, 0, 128, 255},
	"gray": {128, 128, 128, 255}, "grey": {128, 128, 128, 255}, "silver": {192, 192, 192, 255}, "navy": {0, 0, 128, 255},
	"teal": {0, 128, 128, 255}, "maroon": {128, 0, 0, 255}, "olive": {128, 128, 0, 255}, "lime": {0, 255, 0, 255},
	"aqua": {0, 255, 255, 255}, "cyan": {0, 255, 255, 255}, "fuchsia": {255, 0, 255, 255}, "magenta": {255, 0, 255, 255},
	"brown": {165, 42, 42, 255}, "pink": {255, 192, 203, 255}, "gold": {255, 215, 0, 255}, "skyblue": {135, 206, 235, 255},
	"darkblue": {0, 0, 139, 255}, "darkgreen": {0, 100, 0, 255}, "darkgray": {169, 169, 169, 255}, "darkgrey": {169, 169, 169, 255},
	"lightgray": {211, 211, 211, 255}, "lightgrey": {211, 211, 211, 255}, "lightblue": {173, 216, 230, 255},
	"beige": {245, 245, 220, 255}, "tan": {210, 180, 140, 255}, "coral": {255, 127, 80, 255}, "salmon": {250, 128, 114, 255},
	"indigo": {75, 0, 130, 255}, "violet": {238, 130, 238, 255}, "khaki": {240, 230, 140, 255}, "crimson": {220, 20, 60, 255},
	"steelblue": {70, 130, 180, 255}, "slategray": {112, 128, 144, 255}, "forestgreen": {34, 139, 34, 255},
	"transparent": {0, 0, 0, 0},
}

// parsePaint reads a fill or stroke; one it cannot read keeps the inherited.
func parsePaint(v string, inherited paint) paint {
	v = strings.TrimSpace(v)
	switch {
	case v == "none":
		return paint{none: true}
	case v == "inherit" || v == "":
		return inherited
	case strings.HasPrefix(v, "url("):
		id := strings.TrimSuffix(strings.TrimPrefix(v, "url("), ")")
		id = strings.Trim(strings.TrimSpace(id), `'"`)
		return paint{ref: strings.TrimPrefix(id, "#")}
	}
	if c, ok := parseColor(v); ok {
		return paint{c: c}
	}
	return inherited
}

func parseColor(v string) (color.NRGBA, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	if c, ok := namedColors[v]; ok {
		return c, true
	}
	if v == "currentcolor" {
		return color.NRGBA{A: 255}, true
	}
	if strings.HasPrefix(v, "#") {
		h := v[1:]
		if len(h) == 3 || len(h) == 4 {
			var b strings.Builder
			for _, r := range h {
				b.WriteRune(r)
				b.WriteRune(r)
			}
			h = b.String()
		}
		if len(h) == 6 {
			h += "ff"
		}
		if len(h) != 8 {
			return color.NRGBA{}, false
		}
		n, err := strconv.ParseUint(h, 16, 32)
		if err != nil {
			return color.NRGBA{}, false
		}
		return color.NRGBA{uint8(n >> 24), uint8(n >> 16), uint8(n >> 8), uint8(n)}, true
	}
	if strings.HasPrefix(v, "rgb") {
		open, close := strings.Index(v, "("), strings.LastIndex(v, ")")
		if open < 0 || close < open {
			return color.NRGBA{}, false
		}
		parts := strings.FieldsFunc(v[open+1:close], func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
		if len(parts) < 3 {
			return color.NRGBA{}, false
		}
		var ch [4]uint8
		ch[3] = 255
		for i := 0; i < 3; i++ {
			f, ok := parseLength(parts[i], 255)
			if !ok {
				return color.NRGBA{}, false
			}
			ch[i] = uint8(math.Max(0, math.Min(255, math.Round(f))))
		}
		if len(parts) > 3 {
			if a, ok := parseUnit(parts[3]); ok {
				ch[3] = uint8(math.Round(a * 255))
			}
		}
		return color.NRGBA{ch[0], ch[1], ch[2], ch[3]}, true
	}
	return color.NRGBA{}, false
}

// RenderCoverSVG renders a drawing to a cover image: a PNG of 1536x1024, or a
// JPEG on white if the PNG would pass MaxCoverBytes. It answers the image's
// bytes and type, or ErrCoverSVG with a reason the drawing's author can act
// on, including a drawing with nothing this renderer draws.
func RenderCoverSVG(src string) ([]byte, string, error) {
	root, err := parseSVG(src)
	if err != nil {
		return nil, "", err
	}
	r := newSVGRenderer(root)
	r.walk(root, r.base, defaultStyle(), 0)
	if r.drawn == 0 || blank(r.canvas) {
		return nil, "", svgError("it draws nothing this renderer can draw: use shapes and paths filled or stroked with colours or gradients (text, images and filters are left out)")
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, r.canvas); err != nil {
		return nil, "", err
	}
	if buf.Len() <= MaxCoverBytes {
		return buf.Bytes(), "image/png", nil
	}
	flat := image.NewRGBA(r.canvas.Bounds())
	draw.Draw(flat, flat.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), r.canvas, image.Point{}, draw.Over)
	buf.Reset()
	if err := jpeg.Encode(&buf, flat, &jpeg.Options{Quality: 88}); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "image/jpeg", nil
}

// blank reports whether nothing was painted at all.
func blank(img *image.RGBA) bool {
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0 {
			return false
		}
	}
	return true
}

// svgRenderer draws a parsed drawing onto its canvas.
type svgRenderer struct {
	canvas    *image.RGBA
	base      mat
	vbW, vbH  float64
	gradients map[string]*svgNode
	drawn     int
	z         *vector.Rasterizer
}

func newSVGRenderer(root *svgNode) *svgRenderer {
	r := &svgRenderer{canvas: image.NewRGBA(image.Rect(0, 0, coverWidth, coverHeight)), gradients: map[string]*svgNode{},
		z: vector.NewRasterizer(1, 1)}
	minX, minY, w, h := 0.0, 0.0, 0.0, 0.0
	if vb := numbers(root.attrs["viewBox"]); len(vb) == 4 && vb[2] > 0 && vb[3] > 0 {
		minX, minY, w, h = vb[0], vb[1], vb[2], vb[3]
	} else {
		w, _ = parseLength(root.attrs["width"], coverWidth)
		h, _ = parseLength(root.attrs["height"], coverHeight)
		if w <= 0 || h <= 0 {
			w, h = coverWidth, coverHeight
		}
	}
	r.vbW, r.vbH = w, h
	// Scale to cover the canvas, centred: the overflow is cropped, as a
	// cover photo is.
	s := math.Max(coverWidth/w, coverHeight/h)
	r.base = mat{s, 0, 0, s, (coverWidth-w*s)/2 - minX*s, (coverHeight-h*s)/2 - minY*s}
	var collect func(n *svgNode)
	collect = func(n *svgNode) {
		if (n.name == "linearGradient" || n.name == "radialGradient") && n.attrs["id"] != "" {
			r.gradients[n.attrs["id"]] = n
		}
		for _, c := range n.children {
			collect(c)
		}
	}
	collect(root)
	return r
}

// walk draws n and its children under the transform ctm.
func (r *svgRenderer) walk(n *svgNode, ctm mat, style svgStyle, depth int) {
	switch n.name {
	case "defs", "linearGradient", "radialGradient", "title", "desc", "metadata", "style", "script",
		"clipPath", "mask", "pattern", "filter", "symbol", "marker", "text", "image", "use", "foreignObject":
		return
	}
	if v, ok := n.attr("display"); ok && v == "none" {
		return
	}
	if v, ok := n.attr("visibility"); ok && (v == "hidden" || v == "collapse") {
		return
	}
	if t, ok := n.attrs["transform"]; ok && depth > 0 {
		ctm = ctm.mul(parseTransform(t))
	}
	style = style.inherit(n)
	if p := r.shapePath(n); p != nil {
		r.drawShape(p, ctm, style)
	}
	for _, c := range n.children {
		r.walk(c, ctm, style, depth+1)
	}
}

// drawShape fills and strokes one shape's path, in user space under ctm.
func (r *svgRenderer) drawShape(p *svgPath, ctm mat, style svgStyle) {
	if len(p.cmds) == 0 {
		return
	}
	bbox := p.bounds()
	if !style.fill.none && p.fillable {
		if src := r.paintImage(style.fill, style.fillOpacity*style.opacity, ctm, bbox); src != nil {
			r.fill(p.transform(ctm), src)
		}
	}
	if !style.stroke.none && style.strokeWidth > 0 {
		if src := r.paintImage(style.stroke, style.strokeOpacity*style.opacity, ctm, bbox); src != nil {
			width := style.strokeWidth * math.Sqrt(math.Abs(ctm[0]*ctm[3]-ctm[1]*ctm[2]))
			r.stroke(p.transform(ctm), width, src)
		}
	}
}

// fill rasterizes a device-space path, its bounding box only, onto the canvas.
func (r *svgRenderer) fill(p *svgPath, src image.Image) {
	rect := p.deviceRect().Intersect(r.canvas.Bounds())
	if rect.Empty() {
		return
	}
	r.z.Reset(rect.Dx(), rect.Dy())
	ox, oy := float32(rect.Min.X), float32(rect.Min.Y)
	open := false
	for _, c := range p.cmds {
		switch c.op {
		case 'M':
			if open {
				r.z.ClosePath()
			}
			r.z.MoveTo(float32(c.pts[0])-ox, float32(c.pts[1])-oy)
			open = true
		case 'L':
			r.z.LineTo(float32(c.pts[0])-ox, float32(c.pts[1])-oy)
		case 'C':
			r.z.CubeTo(float32(c.pts[0])-ox, float32(c.pts[1])-oy, float32(c.pts[2])-ox, float32(c.pts[3])-oy,
				float32(c.pts[4])-ox, float32(c.pts[5])-oy)
		case 'Z':
			r.z.ClosePath()
			open = false
		}
	}
	if open {
		r.z.ClosePath()
	}
	r.z.Draw(r.canvas, rect, src, rect.Min)
	r.drawn++
}

// paintImage is the source a fill or stroke draws with, nil for nothing
// (a gradient with no stops, a reference to nothing, full transparency).
func (r *svgRenderer) paintImage(p paint, opacity float64, ctm mat, bbox [4]float64) image.Image {
	if p.ref != "" {
		g, ok := r.gradients[p.ref]
		if !ok {
			return nil
		}
		return r.gradient(g, opacity, ctm, bbox)
	}
	c := p.c
	c.A = uint8(math.Round(float64(c.A) * opacity))
	if c.A == 0 {
		return nil
	}
	return image.NewUniform(c)
}
