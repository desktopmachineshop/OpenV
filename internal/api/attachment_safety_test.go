package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUploadLooksLikeImage(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	cases := []struct {
		name     string
		declared string
		data     []byte
		want     bool
	}{
		{"png bytes as png", "image/png", png, true},
		{"html bytes as png", "image/png", []byte("<html><script>alert(1)</script></html>"), false},
		{"empty", "image/png", nil, false},
		{"svg root", "image/svg+xml", []byte("<svg xmlns='http://www.w3.org/2000/svg'></svg>"), true},
		{"svg with xml prolog", "image/svg+xml", []byte("<?xml version='1.0'?><svg></svg>"), true},
		{"html as svg", "image/svg+xml", []byte("<html><body>x</body></html>"), false},
		{"png bytes as svg", "image/svg+xml", png, false},
		{"tiff little endian", "image/tiff", []byte("II*\x00rest"), true},
		{"tiff wrong magic", "image/tiff", []byte("not a tiff"), false},
		{"gif as jpeg", "image/jpeg", []byte("GIF89a......"), true}, // still an image; the catalog does not pin the subtype
	}
	for _, tc := range cases {
		if got := uploadLooksLikeImage(tc.declared, tc.data); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestReadUploadEnforcesTheCap(t *testing.T) {
	t.Setenv(envMaxUploadMB, "1")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments/upload", nil)
	if _, ok := readUpload(rec, req, strings.NewReader(strings.Repeat("x", 1024*1024+1))); ok || rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized upload accepted: ok=%v code=%d", ok, rec.Code)
	}
	rec = httptest.NewRecorder()
	data, ok := readUpload(rec, req, strings.NewReader("small"))
	if !ok || string(data) != "small" {
		t.Fatalf("small upload refused: ok=%v data=%q", ok, data)
	}
}

func TestSVGIsServedAsASandboxedDownload(t *testing.T) {
	if attachmentDisposition("image/svg+xml") != "attachment" || attachmentDisposition("image/png") != "inline" {
		t.Fatal("disposition rule")
	}
	rec := httptest.NewRecorder()
	setAttachmentSecurityHeaders(rec, "image/svg+xml")
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("svg headers: %v", rec.Header())
	}
	rec = httptest.NewRecorder()
	setAttachmentSecurityHeaders(rec, "image/png")
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "img-src 'self'") {
		t.Fatalf("png headers: %v", rec.Header())
	}
}
