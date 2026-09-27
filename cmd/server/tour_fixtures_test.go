//go:build unix

package main

import (
	"bytes"
	"fmt"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"testing"
)

// Fixtures the tour's areas share (refactor plan §6.4 S5a–S5e). Their bytes
// live here, in Go source, never as files under testdata/: that directory's
// .gitattributes has `* text eol=lf`, which would rewrite the \r\n in a PNG
// signature. They are written out by hand, not encoded at run time, so a Go
// upgrade cannot change what the tour uploads.

// tourPNG is a 2×2 RGB PNG (red, green / blue, white): the signature, IHDR,
// one zlib IDAT and IEND, 75 bytes.
const tourPNG = "\x89\x50\x4e\x47\x0d\x0a\x1a\x0a\x00\x00\x00\x0d\x49\x48\x44\x52\x00\x00\x00\x02\x00\x00\x00\x02" +
	"\x08\x02\x00\x00\x00\xfd\xd4\x9a\x73\x00\x00\x00\x12\x49\x44\x41\x54\x78\xda\x63\xf8\xcf\xc0\xc0\x00\xc2" +
	"\x0c\xff\x81\x00\x00\x1f\xee\x05\xfb\xf1\xab\xba\x77\x00\x00\x00\x00\x49\x45\x4e\x44\xae\x42\x60\x82"

// tourPNG2 is the same image with a tEXt chunk ("Comment", "tour figure,
// version 2"), 117 bytes: other bytes, the same pixels, for a figure's
// second version.
const tourPNG2 = "\x89\x50\x4e\x47\x0d\x0a\x1a\x0a\x00\x00\x00\x0d\x49\x48\x44\x52\x00\x00\x00\x02\x00\x00\x00\x02" +
	"\x08\x02\x00\x00\x00\xfd\xd4\x9a\x73\x00\x00\x00\x1e\x74\x45\x58\x74\x43\x6f\x6d\x6d\x65\x6e\x74\x00\x74" +
	"\x6f\x75\x72\x20\x66\x69\x67\x75\x72\x65\x2c\x20\x76\x65\x72\x73\x69\x6f\x6e\x20\x32\x61\xbb\xfd\x47\x00" +
	"\x00\x00\x12\x49\x44\x41\x54\x78\xda\x63\xf8\xcf\xc0\xc0\x00\xc2\x0c\xff\x81\x00\x00\x1f\xee\x05\xfb\xf1" +
	"\xab\xba\x77\x00\x00\x00\x00\x49\x45\x4e\x44\xae\x42\x60\x82"

// tourPDF is the least a PDF upload's content check accepts: the %PDF
// magic.
const tourPDF = "%PDF-1.4\n%%EOF\n"

// tourSVG is an empty SVG image.
const tourSVG = `<svg xmlns="http://www.w3.org/2000/svg"></svg>`

// tourFormBoundary is the boundary of every multipart form the tour sends,
// fixed so that the recorded request is the same on every run.
const tourFormBoundary = "TourFormBoundary7MA4YWxkTrZu0gW"

// tourFormFile is one file part of a form.
type tourFormFile struct {
	field, name, contentType string
	data                     []byte
}

// multipartForm builds a multipart/form-data body: the fields, in order,
// then the files. It returns the body's Content-Type and bytes, for
// rawBody.
func multipartForm(fields [][2]string, files ...tourFormFile) (string, []byte) {
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	if err := mw.SetBoundary(tourFormBoundary); err != nil {
		panic(err)
	}
	for _, f := range fields {
		if err := mw.WriteField(f[0], f[1]); err != nil {
			panic(err)
		}
	}
	for _, f := range files {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, f.field, f.name))
		if f.contentType != "" {
			h.Set("Content-Type", f.contentType)
		}
		w, err := mw.CreatePart(h)
		if err != nil {
			panic(err)
		}
		_, _ = w.Write(f.data)
	}
	if err := mw.Close(); err != nil {
		panic(err)
	}
	return mw.FormDataContentType(), b.Bytes()
}

// TestTourFixtures checks the fixtures with no server: the PNGs decode to
// the same 2×2 image and sniff as image/png, as the upload's content check
// requires, and the PDF sniffs as a PDF.
func TestTourFixtures(t *testing.T) {
	var sums []string
	for _, f := range []string{tourPNG, tourPNG2} {
		if got := http.DetectContentType([]byte(f)); got != "image/png" {
			t.Errorf("a tour PNG sniffs as %s", got)
		}
		img, err := png.Decode(bytes.NewReader([]byte(f)))
		if err != nil {
			t.Fatalf("a tour PNG does not decode: %v", err)
		}
		if b := img.Bounds(); b.Dx() != 2 || b.Dy() != 2 {
			t.Errorf("a tour PNG is %dx%d, not 2x2", b.Dx(), b.Dy())
		}
		s, err := summarisePNG([]byte(f))
		if err != nil {
			t.Fatal(err)
		}
		sums = append(sums, s.PixelsSHA256)
	}
	if len(tourPNG) != 75 || len(tourPNG2) != 117 || sums[0] != sums[1] {
		t.Errorf("the tour PNGs are %d and %d bytes (want 75 and 117) with pixels %v (want equal)", len(tourPNG), len(tourPNG2), sums)
	}
	if got := http.DetectContentType([]byte(tourPDF)); got != "application/pdf" {
		t.Errorf("the tour PDF sniffs as %s", got)
	}
	ct, body := multipartForm([][2]string{{"artifact_id", "x"}}, tourFormFile{"file", "fig.png", "image/png", []byte(tourPNG)})
	if ct != "multipart/form-data; boundary="+tourFormBoundary || !bytes.Contains(body, []byte(tourPNG)) {
		t.Errorf("multipartForm: %s", ct)
	}
}
