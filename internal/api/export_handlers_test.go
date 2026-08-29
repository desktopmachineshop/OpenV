package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// fakeExportService records the formats it was asked for and returns canned
// data. Embedding the interface keeps unused methods loudly unimplemented.
type fakeExportService struct {
	exports.Service
	requested []exports.ExportFormat
	data      []byte
	filename  string
	err       error
}

func (f *fakeExportService) ExportProject(projectID string, format exports.ExportFormat) ([]byte, string, error) {
	f.requested = append(f.requested, format)
	if f.err != nil {
		return nil, "", f.err
	}
	return f.data, f.filename, nil
}

func exportRequest(t *testing.T, format string) *http.Request {
	t.Helper()
	url := "/api/v1/projects/p1/export"
	if format != "" {
		url += "?format=" + format
	}
	r := httptest.NewRequest(http.MethodGet, url, nil)
	// Platform admin passes requireProjectRole without further services.
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "root", IsAdmin: true}))
	return mux.SetURLVars(r, map[string]string{"id": "p1"})
}

func TestExportProjectUnsupportedFormatReturns400(t *testing.T) {
	for _, format := range []string{"xml", "excel", "pdf"} {
		fake := &fakeExportService{}
		h := &Handler{exportService: fake}

		w := httptest.NewRecorder()
		h.ExportProject(w, exportRequest(t, format))

		if w.Code != http.StatusBadRequest {
			t.Errorf("format %q: status = %d, want 400 (body: %s)", format, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "unsupported export format") {
			t.Errorf("format %q: body = %s, want unsupported export format message", format, w.Body.String())
		}
		if len(fake.requested) != 0 {
			t.Errorf("format %q: export service was called for an unsupported format", format)
		}
	}
}

func TestExportProjectJSONHeaders(t *testing.T) {
	fake := &fakeExportService{data: []byte(`{"ok":true}`), filename: "project_Demo_20260101_000000.json"}
	h := &Handler{exportService: fake}

	w := httptest.NewRecorder()
	h.ExportProject(w, exportRequest(t, "")) // default format is json

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	want := `attachment; filename="project_Demo_20260101_000000.json"`
	if got := w.Header().Get("Content-Disposition"); got != want {
		t.Errorf("Content-Disposition = %q, want %q", got, want)
	}
	if w.Body.String() != `{"ok":true}` {
		t.Errorf("body = %q", w.Body.String())
	}
	if len(fake.requested) != 1 || fake.requested[0] != exports.FormatJSON {
		t.Errorf("service formats requested = %v, want [json]", fake.requested)
	}
}

func TestExportProjectCSVHeaders(t *testing.T) {
	fake := &fakeExportService{data: []byte("id,type\n"), filename: "project_My Project_20260101_000000.csv"}
	h := &Handler{exportService: fake}

	w := httptest.NewRecorder()
	h.ExportProject(w, exportRequest(t, "csv"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/csv; charset=utf-8", got)
	}
	want := `attachment; filename="project_My Project_20260101_000000.csv"`
	if got := w.Header().Get("Content-Disposition"); got != want {
		t.Errorf("Content-Disposition = %q, want %q", got, want)
	}
	if len(fake.requested) != 1 || fake.requested[0] != exports.FormatCSV {
		t.Errorf("service formats requested = %v, want [csv]", fake.requested)
	}
}

func TestExportProjectReqIFHeaders(t *testing.T) {
	fake := &fakeExportService{data: []byte(`<?xml version="1.0"?><REQ-IF/>`), filename: "project_Demo_20260101_000000.reqif"}
	h := &Handler{exportService: fake}

	w := httptest.NewRecorder()
	h.ExportProject(w, exportRequest(t, "reqif"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "application/xml; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/xml; charset=utf-8", got)
	}
	want := `attachment; filename="project_Demo_20260101_000000.reqif"`
	if got := w.Header().Get("Content-Disposition"); got != want {
		t.Errorf("Content-Disposition = %q, want %q", got, want)
	}
	if len(fake.requested) != 1 || fake.requested[0] != exports.FormatReqIF {
		t.Errorf("service formats requested = %v, want [reqif]", fake.requested)
	}
}

func TestExportProjectDomainUnsupportedErrorReturns400(t *testing.T) {
	// Defense in depth: if the service itself reports an unsupported format,
	// the handler still answers 400, not 500.
	fake := &fakeExportService{err: fmt.Errorf("wrapped: %w", exports.ErrUnsupportedFormat)}
	h := &Handler{exportService: fake}

	w := httptest.NewRecorder()
	h.ExportProject(w, exportRequest(t, "csv"))

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (body: %s)", w.Code, w.Body.String())
	}
}

func TestExportProjectServiceErrorReturns500(t *testing.T) {
	fake := &fakeExportService{err: fmt.Errorf("database is on fire")}
	h := &Handler{exportService: fake}

	w := httptest.NewRecorder()
	h.ExportProject(w, exportRequest(t, "json"))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (body: %s)", w.Code, w.Body.String())
	}
}
