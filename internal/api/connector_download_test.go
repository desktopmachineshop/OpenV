package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDownloadConnectorServesTheSingleExecutable locks the download contract.
//
// What operators get from this endpoint has regressed twice — once to a zip
// when the single-file build was in place, and once to nothing at all when a
// compose mount shadowed the executables baked into the image. The handler
// itself is the last common step in every one of those paths, so the shapes
// it serves are worth pinning: a single executable when one is there, the
// legacy zip only as a fallback, and an actionable 404 otherwise.
func TestDownloadConnectorServesTheSingleExecutable(t *testing.T) {
	dir := t.TempDir()
	h := &Handler{connectorDistDir: dir}

	// Nothing built yet: say so, and say what to run — never a 500.
	w := httptest.NewRecorder()
	h.DownloadConnector(w, httptest.NewRequest(http.MethodGet, "/api/v1/public/connector/download?os=windows", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("empty dist: status = %d, want 404", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "make connector-dist") {
		t.Errorf("empty dist: body %q does not say how to build the download", body)
	}

	// Only a zip present: the legacy fallback still works.
	if err := os.WriteFile(filepath.Join(dir, "openv-connector-windows.zip"), []byte("PK\x03\x04zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	h.DownloadConnector(w, httptest.NewRequest(http.MethodGet, "/api/v1/public/connector/download?os=windows", nil))
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/zip" {
		t.Errorf("zip fallback: status %d, type %q", w.Code, w.Header().Get("Content-Type"))
	}

	// With the executable present it wins, and is served under a name the
	// user can double-click rather than the per-OS build name.
	if err := os.WriteFile(filepath.Join(dir, "openv-connector-windows.exe"), []byte("MZexe"), 0o644); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	h.DownloadConnector(w, httptest.NewRequest(http.MethodGet, "/api/v1/public/connector/download?os=windows", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("exe: status = %d", w.Code)
	}
	if got := w.Body.String(); got != "MZexe" {
		t.Errorf("served %q, want the executable rather than the zip", got)
	}
	if disp := w.Header().Get("Content-Disposition"); disp != "attachment; filename=openv-connector.exe" {
		t.Errorf("Content-Disposition = %q", disp)
	}

	// Linux is served without the .exe suffix.
	if err := os.WriteFile(filepath.Join(dir, "openv-connector-linux"), []byte("ELF"), 0o644); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	h.DownloadConnector(w, httptest.NewRequest(http.MethodGet, "/api/v1/public/connector/download?os=linux", nil))
	if w.Code != http.StatusOK || w.Header().Get("Content-Disposition") != "attachment; filename=openv-connector" {
		t.Errorf("linux: status %d, disposition %q", w.Code, w.Header().Get("Content-Disposition"))
	}
}
