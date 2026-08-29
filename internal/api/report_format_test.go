package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/reports"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// newReportFormatHandler builds a handler whose report service is sourced from a
// baseline snapshot, so GenerateReport can run without a live export service.
func newReportFormatHandler() *Handler {
	snapshot := `{
		"project_name": "Widget Spec",
		"artifacts": [
			{"id": "req1", "type": "requirement", "title": "System shall boot", "version": 1}
		],
		"links": []
	}`
	baselineSvc := baselines.NewService(&fakeBaselineRepo{byID: map[string]*baselines.Baseline{
		"base-own": {
			ID:        "base-own",
			ProjectID: "proj-a",
			Name:      "Own baseline",
			Snapshot:  json.RawMessage(snapshot),
		},
	}})
	return &Handler{
		projectService: &fakeProjectService{byID: map[string]*projects.Project{
			"proj-a": {ID: "proj-a", OrgID: "org-1"},
		}},
		orgService: &fakeOrgService{roles: map[string]map[string]string{"org-1": {}}},
		memberService: &fakeMemberService{roles: map[string]map[string]string{
			"proj-a": {"viewer-a": members.RoleViewer},
		}},
		baselineService: baselineSvc,
		reportService:   reports.NewService(nil, baselineSvc),
	}
}

func reportRequest(query string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/projects/proj-a/report?baseline_id=base-own"+query, nil)
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "viewer-a"}))
	return mux.SetURLVars(r, map[string]string{"id": "proj-a"})
}

// TestGenerateReportDOCXFormat locks in the DOCX content type, a quoted and
// sanitized .docx Content-Disposition filename, and that the body is a valid
// zip carrying word/document.xml.
func TestGenerateReportDOCXFormat(t *testing.T) {
	w := httptest.NewRecorder()
	newReportFormatHandler().GenerateReport(w, reportRequest("&format=docx"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		t.Errorf("Content-Type = %q, want the wordprocessingml docx type", ct)
	}

	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, `filename="`) {
		t.Errorf("Content-Disposition %q should carry a quoted filename", cd)
	}
	if !strings.Contains(cd, ".docx") {
		t.Errorf("Content-Disposition %q should name a .docx file", cd)
	}

	body := w.Body.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("response body is not a valid zip: %v", err)
	}
	var hasDoc bool
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			hasDoc = true
		}
	}
	if !hasDoc {
		t.Error("docx zip is missing word/document.xml")
	}
}

// TestGenerateReportDefaultsToPDF confirms the format-less path still returns a
// PDF, preserving existing behavior.
func TestGenerateReportDefaultsToPDF(t *testing.T) {
	w := httptest.NewRecorder()
	newReportFormatHandler().GenerateReport(w, reportRequest(""))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type = %q, want application/pdf", ct)
	}
	if !bytes.HasPrefix(w.Body.Bytes(), []byte("%PDF")) {
		t.Error("default report body is not a PDF")
	}
}

// TestGenerateReportUnsupportedFormat confirms an unknown format is a 400.
func TestGenerateReportUnsupportedFormat(t *testing.T) {
	w := httptest.NewRecorder()
	newReportFormatHandler().GenerateReport(w, reportRequest("&format=xml"))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
	}
}
