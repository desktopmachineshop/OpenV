//go:build unix

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"

	"golang.org/x/image/webp"
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

// tourJPEG is a 1×1 JPEG (Go's image/jpeg at quality 50), 598 bytes;
// tourGIF a 1×1 GIF (image/gif), 35 bytes; and tourWebP a 1×1 lossless
// WebP, 34 bytes (S5c's logo uploads). Each decodes and sniffs as its type
// (http.DetectContentType), which is all an image upload checks
// (TestTourFixtures). Their bytes are written out, as the PNGs' are, so that
// no Go upgrade changes what the tour uploads.
const tourJPEG = "\xff\xd8\xff\xdb\x00\x84\x00\x10\x0b\x0c\x0e\x0c\x0a\x10\x0e\x0d\x0e\x12\x11\x10\x13\x18\x28\x1a" +
	"\x18\x16\x16\x18\x31\x23\x25\x1d\x28\x3a\x33\x3d\x3c\x39\x33\x38\x37\x40\x48\x5c\x4e\x40\x44\x57" +
	"\x45\x37\x38\x50\x6d\x51\x57\x5f\x62\x67\x68\x67\x3e\x4d\x71\x79\x70\x64\x78\x5c\x65\x67\x63\x01" +
	"\x11\x12\x12\x18\x15\x18\x2f\x1a\x1a\x2f\x63\x42\x38\x42\x63\x63\x63\x63\x63\x63\x63\x63\x63\x63" +
	"\x63\x63\x63\x63\x63\x63\x63\x63\x63\x63\x63\x63\x63\x63\x63\x63\x63\x63\x63\x63\x63\x63\x63\x63" +
	"\x63\x63\x63\x63\x63\x63\x63\x63\x63\x63\x63\x63\x63\x63\x63\x63\xff\xc0\x00\x11\x08\x00\x01\x00" +
	"\x01\x03\x01\x22\x00\x02\x11\x01\x03\x11\x01\xff\xc4\x01\xa2\x00\x00\x01\x05\x01\x01\x01\x01\x01" +
	"\x01\x00\x00\x00\x00\x00\x00\x00\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x10\x00\x02\x01" +
	"\x03\x03\x02\x04\x03\x05\x05\x04\x04\x00\x00\x01\x7d\x01\x02\x03\x00\x04\x11\x05\x12\x21\x31\x41" +
	"\x06\x13\x51\x61\x07\x22\x71\x14\x32\x81\x91\xa1\x08\x23\x42\xb1\xc1\x15\x52\xd1\xf0\x24\x33\x62" +
	"\x72\x82\x09\x0a\x16\x17\x18\x19\x1a\x25\x26\x27\x28\x29\x2a\x34\x35\x36\x37\x38\x39\x3a\x43\x44" +
	"\x45\x46\x47\x48\x49\x4a\x53\x54\x55\x56\x57\x58\x59\x5a\x63\x64\x65\x66\x67\x68\x69\x6a\x73\x74" +
	"\x75\x76\x77\x78\x79\x7a\x83\x84\x85\x86\x87\x88\x89\x8a\x92\x93\x94\x95\x96\x97\x98\x99\x9a\xa2" +
	"\xa3\xa4\xa5\xa6\xa7\xa8\xa9\xaa\xb2\xb3\xb4\xb5\xb6\xb7\xb8\xb9\xba\xc2\xc3\xc4\xc5\xc6\xc7\xc8" +
	"\xc9\xca\xd2\xd3\xd4\xd5\xd6\xd7\xd8\xd9\xda\xe1\xe2\xe3\xe4\xe5\xe6\xe7\xe8\xe9\xea\xf1\xf2\xf3" +
	"\xf4\xf5\xf6\xf7\xf8\xf9\xfa\x01\x00\x03\x01\x01\x01\x01\x01\x01\x01\x01\x01\x00\x00\x00\x00\x00" +
	"\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x11\x00\x02\x01\x02\x04\x04\x03\x04\x07\x05\x04" +
	"\x04\x00\x01\x02\x77\x00\x01\x02\x03\x11\x04\x05\x21\x31\x06\x12\x41\x51\x07\x61\x71\x13\x22\x32" +
	"\x81\x08\x14\x42\x91\xa1\xb1\xc1\x09\x23\x33\x52\xf0\x15\x62\x72\xd1\x0a\x16\x24\x34\xe1\x25\xf1" +
	"\x17\x18\x19\x1a\x26\x27\x28\x29\x2a\x35\x36\x37\x38\x39\x3a\x43\x44\x45\x46\x47\x48\x49\x4a\x53" +
	"\x54\x55\x56\x57\x58\x59\x5a\x63\x64\x65\x66\x67\x68\x69\x6a\x73\x74\x75\x76\x77\x78\x79\x7a\x82" +
	"\x83\x84\x85\x86\x87\x88\x89\x8a\x92\x93\x94\x95\x96\x97\x98\x99\x9a\xa2\xa3\xa4\xa5\xa6\xa7\xa8" +
	"\xa9\xaa\xb2\xb3\xb4\xb5\xb6\xb7\xb8\xb9\xba\xc2\xc3\xc4\xc5\xc6\xc7\xc8\xc9\xca\xd2\xd3\xd4\xd5" +
	"\xd6\xd7\xd8\xd9\xda\xe2\xe3\xe4\xe5\xe6\xe7\xe8\xe9\xea\xf2\xf3\xf4\xf5\xf6\xf7\xf8\xf9\xfa\xff" +
	"\xda\x00\x0c\x03\x01\x00\x02\x11\x03\x11\x00\x3f\x00\xa1\x45\x14\x57\xac\x79\xc7\xff\xd9"

const tourGIF = "\x47\x49\x46\x38\x39\x61\x01\x00\x01\x00\x80\x00\x00\x2e\x6d\xb4\xff\xff\xff\x2c\x00\x00\x00\x00" +
	"\x01\x00\x01\x00\x00\x02\x02\x44\x01\x00\x3b"

const tourWebP = "\x52\x49\x46\x46\x1a\x00\x00\x00\x57\x45\x42\x50\x56\x50\x38\x4c\x0d\x00\x00\x00\x2f\x00\x00\x00" +
	"\x10\x07\x10\x11\x11\x88\x88\xfe\x07\x00"

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
// requires, the PDF sniffs as a PDF, and the JPEG, GIF and WebP decode to
// 1×1 images and sniff as their types.
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
	for _, f := range []struct {
		name, data, sniff string
		size              int
		decode            func(io.Reader) (image.Image, error)
	}{
		{"JPEG", tourJPEG, "image/jpeg", 598, jpeg.Decode},
		{"GIF", tourGIF, "image/gif", 35, gif.Decode},
		{"WebP", tourWebP, "image/webp", 34, webp.Decode},
	} {
		if got := http.DetectContentType([]byte(f.data)); got != f.sniff || len(f.data) != f.size {
			t.Errorf("the tour %s sniffs as %s in %d bytes (want %s in %d)", f.name, got, len(f.data), f.sniff, f.size)
		}
		img, err := f.decode(strings.NewReader(f.data))
		if err != nil {
			t.Errorf("the tour %s does not decode: %v", f.name, err)
		} else if b := img.Bounds(); b.Dx() != 1 || b.Dy() != 1 {
			t.Errorf("the tour %s is %dx%d, not 1x1", f.name, b.Dx(), b.Dy())
		}
	}
	ct, body := multipartForm([][2]string{{"artifact_id", "x"}}, tourFormFile{"file", "fig.png", "image/png", []byte(tourPNG)})
	if ct != "multipart/form-data; boundary="+tourFormBoundary || !bytes.Contains(body, []byte(tourPNG)) {
		t.Errorf("multipartForm: %s", ct)
	}
}
