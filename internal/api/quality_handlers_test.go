package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/quality"
	"github.com/openv/requirements-platform/internal/domain/users"
)

func newQualityHandler() *Handler {
	return &Handler{
		projectService: &fakeProjectService{byID: map[string]*projects.Project{
			"proj-a": {ID: "proj-a", OrgID: "org-1"},
		}},
		orgService: &fakeOrgService{roles: map[string]map[string]string{"org-1": {}}},
		memberService: &fakeMemberService{roles: map[string]map[string]string{
			"proj-a": {"viewer-a": members.RoleViewer},
		}},
		artifactService: &fakeArtifactService{byID: map[string]*artifacts.Artifact{
			"req-weak": {
				ID:        "req-weak",
				ProjectID: "proj-a",
				Type:      artifacts.TypeRequirement,
				Title:     "Speed",
				Body:      "The system should be fast and user-friendly.",
			},
			"heading-1": {
				ID:        "heading-1",
				ProjectID: "proj-a",
				Type:      artifacts.TypeHeading,
				Title:     "Section",
				Body:      "Intro",
			},
		}},
		exportService: &fakeExportService{data: []byte(`{
			"project_id": "proj-a",
			"artifacts": [
				{"id":"req-weak","project_id":"proj-a","type":"requirement","title":"Speed","body":"The system should be fast and user-friendly."},
				{"id":"heading-1","project_id":"proj-a","type":"heading","title":"Section","body":"Intro"}
			]
		}`)},
	}
}

func reqWithViewer(target, projectID string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/x/"+target+"/quality", nil)
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "viewer-a"}))
	return mux.SetURLVars(r, map[string]string{"id": target})
}

// TestProjectQualityShape confirms the project endpoint lints only requirement
// types and returns the score/finding shape.
func TestProjectQualityShape(t *testing.T) {
	w := httptest.NewRecorder()
	newQualityHandler().GetProjectQuality(w, reqWithViewer("proj-a", "proj-a"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	var report quality.Report
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v (body %q)", err, w.Body.String())
	}
	if len(report.Entries) != 1 {
		t.Fatalf("expected 1 requirement entry (heading skipped), got %d", len(report.Entries))
	}
	entry := report.Entries[0]
	if entry.ArtifactID != "req-weak" {
		t.Fatalf("unexpected entry id %q", entry.ArtifactID)
	}
	if entry.Score >= 100 || len(entry.Findings) == 0 {
		t.Fatalf("weak requirement should have findings and score < 100, got score=%d findings=%d", entry.Score, len(entry.Findings))
	}
}

// TestProjectQualityRequiresViewer confirms an unauthenticated caller is
// rejected before any data is returned.
func TestProjectQualityRequiresViewer(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/projects/proj-a/quality", nil)
	r = mux.SetURLVars(r, map[string]string{"id": "proj-a"})
	newQualityHandler().GetProjectQuality(w, r) // no user in context
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %q)", w.Code, w.Body.String())
	}
}

// TestArtifactQualityShape confirms the single-artifact endpoint returns a
// score for a requirement.
func TestArtifactQualityShape(t *testing.T) {
	w := httptest.NewRecorder()
	newQualityHandler().GetArtifactQuality(w, reqWithViewer("req-weak", "proj-a"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	var score quality.ArtifactScore
	if err := json.Unmarshal(w.Body.Bytes(), &score); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if score.ArtifactID != "req-weak" || score.Band == "" {
		t.Fatalf("unexpected score payload: %+v", score)
	}
}

// TestArtifactQualityRejectsNonRequirement confirms a heading is refused with a
// 400 rather than returning an empty score.
func TestArtifactQualityRejectsNonRequirement(t *testing.T) {
	w := httptest.NewRecorder()
	newQualityHandler().GetArtifactQuality(w, reqWithViewer("heading-1", "proj-a"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
	}
}
