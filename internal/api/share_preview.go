package api

// The social preview card for a shared project (REQ-149): the image Slack,
// Discord, LinkedIn and the rest show beside a share link. Drawn here with
// the same Go fonts the PDF uses, so the card needs nothing but the API.

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	previewW = 1200
	previewH = 630
)

var (
	previewBG     = color.RGBA{0x1f, 0x2d, 0x3d, 0xff}
	previewAccent = color.RGBA{0x2c, 0x8e, 0xf0, 0xff}
	previewText   = color.RGBA{0xf5, 0xf7, 0xfa, 0xff}
	previewMuted  = color.RGBA{0xb8, 0xc2, 0xcc, 0xff}
)

type previewCard struct {
	Eyebrow  string // "Shared from OpenV" / "Open-source project on OpenV"
	Title    string // project name
	Subtitle string // workspace
	Lines    []string
	Footer   string
}

func previewFace(data []byte, size float64) (font.Face, error) {
	f, err := opentype.Parse(data)
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
}

// wrapLines breaks text into lines no wider than width, at most max lines,
// the last one ellipsised when it had to stop.
func wrapLines(face font.Face, text string, width int, max int) []string {
	measure := func(s string) int { return font.MeasureString(face, s).Ceil() }
	words := strings.Fields(text)
	var lines []string
	cur := ""
	for _, w := range words {
		candidate := w
		if cur != "" {
			candidate = cur + " " + w
		}
		if measure(candidate) <= width || cur == "" {
			cur = candidate
			continue
		}
		lines = append(lines, cur)
		cur = w
		if len(lines) == max {
			break
		}
	}
	if len(lines) < max && cur != "" {
		lines = append(lines, cur)
	} else if len(lines) == max && cur != "" {
		last := lines[max-1]
		for measure(last+"…") > width && len(last) > 1 {
			last = last[:len(last)-1]
		}
		lines[max-1] = strings.TrimSpace(last) + "…"
	}
	return lines
}

func drawText(img draw.Image, face font.Face, c color.Color, x, y int, text string) {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(c), Face: face, Dot: fixed.P(x, y)}
	d.DrawString(text)
}

// renderPreview draws the card as PNG bytes.
func renderPreview(card previewCard) ([]byte, error) {
	bold, err := previewFace(gobold.TTF, 60)
	if err != nil {
		return nil, err
	}
	medium, err := previewFace(goregular.TTF, 30)
	if err != nil {
		return nil, err
	}
	small, err := previewFace(gobold.TTF, 24)
	if err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, previewW, previewH))
	draw.Draw(img, img.Bounds(), image.NewUniform(previewBG), image.Point{}, draw.Src)
	// Accent bar down the left, the way the app's sidebar reads.
	draw.Draw(img, image.Rect(0, 0, 18, previewH), image.NewUniform(previewAccent), image.Point{}, draw.Src)

	const left = 72
	drawText(img, small, previewAccent, left, 96, strings.ToUpper(card.Eyebrow))
	y := 190
	for _, line := range wrapLines(bold, card.Title, previewW-left-72, 2) {
		drawText(img, bold, previewText, left, y, line)
		y += 72
	}
	if card.Subtitle != "" {
		drawText(img, medium, previewMuted, left, y+8, card.Subtitle)
		y += 52
	}
	y += 20
	for _, line := range card.Lines {
		for _, wrapped := range wrapLines(medium, line, previewW-left-72, 2) {
			drawText(img, medium, previewText, left, y, wrapped)
			y += 42
		}
	}
	drawText(img, small, previewMuted, left, previewH-56, card.Footer)
	// Wordmark bottom right.
	mark := "OpenV"
	w := font.MeasureString(bold, mark).Ceil()
	drawText(img, bold, previewText, previewW-72-w, previewH-48, mark)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
