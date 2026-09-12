package api

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/evidence"
)

// The evidence cap is deliberately its own knob. A 25 MB ceiling is right for
// an image pasted into a requirement and useless for an instrument capture, so
// the two must not share a setting.
func TestEvidenceUploadCapIsSeparateFromTheFigureCap(t *testing.T) {
	t.Setenv(envMaxUploadMB, "25")
	t.Setenv(envMaxEvidenceMB, "")
	if got := maxEvidenceBytes(); got != defaultMaxEvidenceMB*1024*1024 {
		t.Fatalf("default evidence cap is %d, want %d MB", got, defaultMaxEvidenceMB)
	}
	if maxEvidenceBytes() == maxUploadBytes() {
		t.Fatal("the evidence cap collapsed onto the figure cap")
	}

	t.Setenv(envMaxEvidenceMB, "512")
	if got, want := maxEvidenceBytes(), int64(512*1024*1024); got != want {
		t.Fatalf("OPENV_MAX_EVIDENCE_MB=512 gave %d, want %d", got, want)
	}
	// Nonsense falls back to the default rather than to "unlimited".
	for _, bad := range []string{"0", "-4", "lots"} {
		t.Setenv(envMaxEvidenceMB, bad)
		if got := maxEvidenceBytes(); got != defaultMaxEvidenceMB*1024*1024 {
			t.Errorf("OPENV_MAX_EVIDENCE_MB=%q gave %d, want the default", bad, got)
		}
	}
}

// The upload is streamed, which means walking to the file part rather than
// parsing the form — ParseMultipartForm would buffer the whole capture first.
// Walking has to skip the ordinary fields a browser puts before the file.
func TestNextFilePartSkipsOrdinaryFormFields(t *testing.T) {
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	if err := mw.WriteField("note", "taken on rig 2"); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("captured_by", "J. Patel"); err != nil {
		t.Fatal(err)
	}
	fw, err := mw.CreateFormFile("file", "sweep.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte("t,db\n0,42\n")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/v1/evidence-bundles/b-1/files", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	reader, err := req.MultipartReader()
	if err != nil {
		t.Fatal(err)
	}

	part, err := nextFilePart(reader)
	if err != nil {
		t.Fatalf("walking to the file part: %v", err)
	}
	if part == nil {
		t.Fatal("no file part found past the ordinary fields")
	}
	defer part.Close()
	if part.FileName() != "sweep.csv" {
		t.Fatalf("stopped at %q, want sweep.csv", part.FileName())
	}
}

// A request carrying no file at all must be told so, not treated as an empty
// upload.
func TestNextFilePartReportsAFormWithNoFile(t *testing.T) {
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	if err := mw.WriteField("note", "no file attached"); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/v1/evidence-bundles/b-1/files", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	reader, err := req.MultipartReader()
	if err != nil {
		t.Fatal(err)
	}
	part, err := nextFilePart(reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if part != nil {
		t.Fatalf("found a file part %q in a form that has none", part.FileName())
	}
}

// Evidence can be any format, including ones that carry script. Unlike a
// figure, it is therefore never rendered on the API origin: always a download,
// never sniffed, under a policy that permits nothing.
func TestEvidenceDownloadsAreNeverRenderable(t *testing.T) {
	rec := httptest.NewRecorder()

	// The header block the download handler sets, exercised directly: the
	// handler's own body path needs a project, a bundle and a file on disk,
	// which the local end-to-end run covers.
	file := &evidence.File{Filename: "report.html", MimeType: "text/html", FileSize: 12, SHA256: "deadbeef"}
	writeEvidenceDownloadHeaders(rec, file)

	got := rec.Header()
	if ct := got.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type is %q; an uploaded text/html must not be served as itself", ct)
	}
	if cd := got.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") {
		t.Errorf("Content-Disposition is %q, want an attachment", cd)
	}
	if !strings.Contains(got.Get("Content-Disposition"), `"report.html"`) {
		t.Errorf("the download loses its filename: %q", got.Get("Content-Disposition"))
	}
	if got.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("evidence downloads must not be sniffable")
	}
	if csp := got.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("Content-Security-Policy is %q, want default-src 'none'", csp)
	}
	// The recorded digest travels with the bytes so a downloader can check
	// what they got against the record.
	if got.Get("X-Evidence-SHA256") != "deadbeef" {
		t.Errorf("the digest did not travel with the download: %q", got.Get("X-Evidence-SHA256"))
	}
}
