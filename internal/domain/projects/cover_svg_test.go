package projects

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

// render renders src and decodes the image, failing the test on an error.
func render(t *testing.T, src string) image.Image {
	t.Helper()
	data, mime, err := RenderCoverSVG(src)
	if err != nil {
		t.Fatalf("RenderCoverSVG: %v", err)
	}
	if mime != "image/png" {
		t.Fatalf("type %q, want image/png", mime)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 1536 || b.Dy() != 1024 {
		t.Fatalf("size %v, want 1536x1024", b)
	}
	return img
}

func rgba(img image.Image, x, y int) color.RGBA {
	return color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
}

// near reports whether two colours differ by at most tol in every channel.
func near(a, b color.RGBA, tol int) bool {
	d := func(x, y uint8) bool { v := int(x) - int(y); return v <= tol && v >= -tol }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B) && d(a.A, b.A)
}

const svgHead = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1536 1024">`

// Shapes land where their coordinates and transforms put them, in their
// colours, and nothing is drawn outside them.
func TestRenderCoverSVGShapes(t *testing.T) {
	img := render(t, svgHead+`
		<rect width="1536" height="1024" fill="#102030"/>
		<circle cx="200" cy="200" r="100" fill="red"/>
		<g transform="translate(1000 600) scale(2)"><rect width="50" height="50" fill="rgb(0, 128, 255)"/></g>
		<path d="M600 100 h100 v100 h-100 Z" fill="#0f0" opacity="0.5"/>
		<ellipse cx="800" cy="800" rx="120" ry="40" fill="gold"/>
	</svg>`)
	for _, c := range []struct {
		name string
		x, y int
		want color.RGBA
	}{
		{"background", 10, 10, color.RGBA{0x10, 0x20, 0x30, 255}},
		{"circle's centre", 200, 200, color.RGBA{255, 0, 0, 255}},
		{"just outside the circle", 200, 310, color.RGBA{0x10, 0x20, 0x30, 255}},
		{"the scaled square, translated", 1090, 690, color.RGBA{0, 128, 255, 255}},
		{"past the scaled square", 1110, 710, color.RGBA{0x10, 0x20, 0x30, 255}},
		{"half-opaque green over the background", 650, 150, color.RGBA{8, 143, 24, 255}},
		{"the ellipse", 900, 800, color.RGBA{255, 215, 0, 255}},
	} {
		if got := rgba(img, c.x, c.y); !near(got, c.want, 3) {
			t.Errorf("%s at (%d,%d) = %v, want %v", c.name, c.x, c.y, got, c.want)
		}
	}
}

// A viewBox of another size and shape is scaled to cover the frame, centred.
func TestRenderCoverSVGScalesTheViewBoxToCover(t *testing.T) {
	// 300x200 is the frame's 3:2: the left half blue, the right half white.
	img := render(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 300 200">
		<rect width="150" height="200" fill="blue"/><rect x="150" width="150" height="200" fill="white"/></svg>`)
	if got := rgba(img, 700, 500); !near(got, color.RGBA{0, 0, 255, 255}, 2) {
		t.Errorf("left of centre = %v, want blue", got)
	}
	if got := rgba(img, 840, 500); !near(got, color.RGBA{255, 255, 255, 255}, 2) {
		t.Errorf("right of centre = %v, want white", got)
	}
}

// Gradients run between their stops across the shape's box, and a radial
// one from its centre out.
func TestRenderCoverSVGGradients(t *testing.T) {
	img := render(t, svgHead+`<defs>
		<linearGradient id="lr"><stop offset="0" stop-color="#000"/><stop offset="1" stop-color="#fff"/></linearGradient>
		<radialGradient id="rad" cx="50%" cy="50%" r="50%"><stop offset="0" stop-color="red"/><stop offset="100%" stop-color="blue"/></radialGradient>
		</defs>
		<rect width="1536" height="512" fill="url(#lr)"/>
		<rect y="512" width="1536" height="512" fill="url(#rad)"/></svg>`)
	left, mid, right := rgba(img, 2, 100), rgba(img, 768, 100), rgba(img, 1533, 100)
	if left.R > 10 || right.R < 245 || mid.R < 110 || mid.R > 145 {
		t.Errorf("linear gradient: left %v, middle %v, right %v; want black to white", left, mid, right)
	}
	if c := rgba(img, 768, 768); !near(c, color.RGBA{255, 0, 0, 255}, 6) {
		t.Errorf("radial gradient's centre = %v, want red", c)
	}
}

// Strokes draw a line of their width, curves and arcs included, and a
// shape with fill="none" is only its outline.
func TestRenderCoverSVGStrokesAndArcs(t *testing.T) {
	img := render(t, svgHead+`
		<rect width="1536" height="1024" fill="white"/>
		<line x1="100" y1="100" x2="500" y2="100" stroke="black" stroke-width="20"/>
		<path d="M100 400 A200 200 0 0 1 500 400" fill="none" stroke="#00f" stroke-width="10"/>
		<circle cx="1000" cy="500" r="100" fill="none" stroke="green" stroke-width="8"/></svg>`)
	for _, c := range []struct {
		name string
		x, y int
		dark bool
	}{
		{"the line", 300, 105, true},
		{"beside the line", 300, 125, false},
		{"the top of the arc", 300, 200, true},
		{"inside the arc", 300, 300, false},
		{"the circle's outline", 1100, 500, true},
		{"inside the unfilled circle", 1000, 500, false},
	} {
		p := rgba(img, c.x, c.y)
		if dark := p.R < 128 || p.G < 128 || p.B < 128; dark != c.dark {
			t.Errorf("%s at (%d,%d) = %v; drawn = %v, want %v", c.name, c.x, c.y, p, dark, c.dark)
		}
	}
}

// What the renderer leaves out is never drawn and never fetched, and a
// drawing of nothing else, or one that is no drawing, is refused with a
// reason.
func TestRenderCoverSVGRefusals(t *testing.T) {
	// A script and an external image are left out; the rect still renders.
	img := render(t, svgHead+`<script>alert(1)</script><image href="https://example.com/x.png" width="10" height="10"/>
		<rect width="1536" height="1024" fill="#123456"/></svg>`)
	if got := rgba(img, 5, 5); !near(got, color.RGBA{0x12, 0x34, 0x56, 255}, 1) {
		t.Errorf("rect beside a script = %v", got)
	}
	for _, c := range []struct{ name, src, says string }{
		{"text only", svgHead + `<text x="10" y="10">Hello</text></svg>`, "draws nothing"},
		{"transparent only", svgHead + `<rect width="10" height="10" fill="none"/></svg>`, "draws nothing"},
		{"not svg", `<html><body/></html>`, "not an svg element"},
		{"not xml", `this is not a drawing`, "not an svg element"},
		{"malformed", svgHead + `<rect width="10"`, "well-formed"},
		{"too big", svgHead + strings.Repeat(" ", MaxCoverSVGBytes) + `</svg>`, "larger than 256 KB"},
		{"too many elements", svgHead + strings.Repeat(`<g/>`, maxSVGElements+1) + `</svg>`, "more than 4000 elements"},
		{"too deep", svgHead + strings.Repeat(`<g>`, maxSVGDepth+1) + strings.Repeat(`</g>`, maxSVGDepth+1) + `</svg>`, "nest more than"},
	} {
		_, _, err := RenderCoverSVG(c.src)
		if !errors.Is(err, ErrCoverSVG) || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: %v; want ErrCoverSVG saying %q", c.name, err, c.says)
		}
	}
}

// Path data reads as SVG writes it: relative commands, implicit repeats,
// numbers run together, smooth curves.
func TestParsePathData(t *testing.T) {
	p := parsePathData("m10,10 l10-5 20,0 h5 v5 c0,10 10,10 10,0 s10-10 10,0 q5-5 10,0 t10,0 z")
	if p == nil {
		t.Fatal("no path")
	}
	ops := ""
	for _, c := range p.cmds {
		ops += string(c.op)
	}
	if ops != "MLLLLCCCCZ" {
		t.Errorf("commands %q, want MLLLLCCCCZ", ops)
	}
	if got := p.cmds[2].pts; got[0] != 40 || got[1] != 5 {
		t.Errorf("the implicit second l lands at %v, want (40, 5)", got)
	}
	if parsePathData("L10 10") != nil {
		t.Error("a path that does not start with a move was drawn")
	}
}
