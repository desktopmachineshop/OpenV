package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/orgs"
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

func TestStoreUploadEnforcesTheCap(t *testing.T) {
	dir := t.TempDir()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments/upload", nil)
	limit := int64(1024 * 1024)

	over := filepath.Join(dir, "over")
	if _, _, ok := storeUpload(rec, req, strings.NewReader(strings.Repeat("x", int(limit)+1)), over, limit); ok ||
		rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized upload accepted: ok=%v code=%d", ok, rec.Code)
	}
	// A refusal leaves nothing behind for a cleanup job to find later.
	if _, err := os.Stat(over); !os.IsNotExist(err) {
		t.Fatalf("a refused upload left its partial file at %s", over)
	}

	rec = httptest.NewRecorder()
	under := filepath.Join(dir, "under")
	head, size, ok := storeUpload(rec, req, strings.NewReader("small"), under, limit)
	if !ok || size != 5 || string(head) != "small" {
		t.Fatalf("small upload refused: ok=%v size=%d head=%q", ok, size, head)
	}
	stored, err := os.ReadFile(under)
	if err != nil || string(stored) != "small" {
		t.Fatalf("stored file is %q (%v)", stored, err)
	}
}

// The head is what the format checks read, and a file larger than the peek
// window must still hand back a full 512 bytes of it.
func TestStoreUploadAnswersTheHeadOfALargeFile(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments/upload", nil)
	path := filepath.Join(t.TempDir(), "big")
	body := "\x89PNG\r\n\x1a\n" + strings.Repeat("y", 4096)

	head, size, ok := storeUpload(rec, req, strings.NewReader(body), path, int64(len(body)))
	if !ok {
		t.Fatalf("upload at exactly the limit refused: code=%d", rec.Code)
	}
	if len(head) != uploadHeadBytes {
		t.Fatalf("head is %d bytes, want %d", len(head), uploadHeadBytes)
	}
	if size != int64(len(body)) {
		t.Fatalf("size is %d, want %d", size, len(body))
	}
	if !strings.HasPrefix(string(head), "\x89PNG") {
		t.Fatalf("head does not start at the start of the file: %q", head[:8])
	}
}

// The figure cap is a workspace limit now (issue #364): it follows the plan
// where no operator override is set, and the override still wins where it is.
func TestUploadLimitFollowsTheWorkspacePlan(t *testing.T) {
	h := &Handler{}
	t.Setenv(envMaxUploadMB, "")
	if got, want := h.uploadLimitBytes(""), int64(defaultMaxUploadMB)*bytesPerMB; got != want {
		t.Fatalf("no workspace gave %d, want the free plan's %d", got, want)
	}
	t.Setenv(envMaxUploadMB, "7")
	if got, want := h.uploadLimitBytes(""), int64(7)*bytesPerMB; got != want {
		t.Fatalf("OPENV_MAX_UPLOAD_MB=7 gave %d, want %d", got, want)
	}
	// Nonsense is ignored rather than read as "no cap".
	t.Setenv(envMaxUploadMB, "banana")
	if got, want := h.uploadLimitBytes(""), int64(defaultMaxUploadMB)*bytesPerMB; got != want {
		t.Fatalf("a bad override gave %d, want %d", got, want)
	}
}

// Every plan must allow a real CAD file, which is what the 25 MB this shipped
// with did not (issue #364).
func TestEveryPlanAllowsAFigureWorthUploading(t *testing.T) {
	for _, plan := range []string{orgs.PlanSingle, orgs.PlanBusinessLite, orgs.PlanBusiness, orgs.PlanOpenSource} {
		mb, ok := orgs.LimitInt(orgs.PlanDefaults(plan), orgs.LimitMaxUploadMB)
		if !ok || mb < 100 {
			t.Errorf("plan %s caps a figure at %d MB", plan, mb)
		}
	}
	for _, plan := range []string{orgs.PlanSelfHost, orgs.PlanEnterprise} {
		if mb, _ := orgs.LimitInt(orgs.PlanDefaults(plan), orgs.LimitMaxUploadMB); mb != 0 {
			t.Errorf("plan %s should ration nothing, got %d MB", plan, mb)
		}
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

// Widening what can be attached must not widen what the API will render. Only
// an inert picture is served inline; everything else is handed over as a file
// under a policy that permits nothing, which is what makes storing a PDF or a
// CAD model safe without trusting its contents.
func TestOnlyInertPicturesAreServedInline(t *testing.T) {
	inline := []string{"image/png", "image/jpeg", "image/gif", "image/webp", "image/tiff", "image/bmp"}
	for _, mime := range inline {
		if got := attachmentDisposition(mime); got != "inline" {
			t.Errorf("attachmentDisposition(%q) = %q, want inline", mime, got)
		}
	}
	download := []string{
		"image/svg+xml",   // a document that can carry script
		"application/pdf", // likewise
		"model/step", "model/stl", "model/3mf",
		"image/vnd.dxf", // registered under image/, renderable by nothing
		"application/octet-stream",
	}
	for _, mime := range download {
		if got := attachmentDisposition(mime); got != "attachment" {
			t.Errorf("attachmentDisposition(%q) = %q, want attachment", mime, got)
		}
	}
}

func TestNonPicturesAreServedUnderASandbox(t *testing.T) {
	for _, mime := range []string{"application/pdf", "model/step", "image/svg+xml", "image/vnd.dxf"} {
		w := httptest.NewRecorder()
		setAttachmentSecurityHeaders(w, mime)
		csp := w.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "sandbox") || !strings.Contains(csp, "default-src 'none'") {
			t.Errorf("%s served under %q; want a sandbox that permits nothing", mime, csp)
		}
		if !strings.Contains(csp, "frame-ancestors 'none'") {
			t.Errorf("%s may be framed: %q", mime, csp)
		}
		if w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s may be sniffed into another type", mime)
		}
	}

	w := httptest.NewRecorder()
	setAttachmentSecurityHeaders(w, "image/png")
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "img-src 'self'") {
		t.Errorf("a picture should still be renderable as one: %q", csp)
	}
}

// The image sniff is shared with avatars and workspace logos, which must stay
// pictures; the figure sniff is the one that knows about the wider catalogue.
func TestFigureSniffAcceptsTheCatalogueAndTheImageSniffDoesNot(t *testing.T) {
	pdf := []byte("%PDF-1.7\nnot really, but the header is what is checked")
	if uploadLooksLikeImage("application/pdf", pdf) {
		t.Error("the image sniff accepted a PDF; avatars and logos depend on it refusing one")
	}
	if !uploadLooksLikeFigure("application/pdf", "datasheet.pdf", pdf) {
		t.Error("the figure sniff refused a PDF")
	}
	if uploadLooksLikeFigure("application/pdf", "datasheet.pdf", []byte("<html>")) {
		t.Error("the figure sniff accepted a mis-named PDF")
	}
	if !uploadLooksLikeFigure("image/png", "pump.png", []byte("\x89PNG\r\n\x1a\n")) {
		t.Error("the figure sniff refused a real PNG")
	}
	if uploadLooksLikeFigure("image/png", "pump.png", []byte("%PDF-1.7")) {
		t.Error("the figure sniff let a PDF through as a PNG; it would be served inline")
	}
}
