package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/quality"
	"github.com/openv/requirements-platform/internal/domain/users"
)

func newQualityHandler(t *testing.T) *Handler {
	return newTestHandler(t, func(h *Handler) {
		h.projectService = &fakeProjectService{byID: map[string]*projects.Project{
			"proj-a": {ID: "proj-a", OrgID: "org-1"},
		}}
		h.orgService = &fakeOrgService{roles: map[string]map[string]string{"org-1": {}}}
		h.memberService = &fakeMemberService{roles: map[string]map[string]string{
			"proj-a": {"viewer-a": members.RoleViewer},
		}}
		h.artifactService = &fakeArtifactService{byID: map[string]*artifacts.Artifact{
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
		}}
		h.exportService = &fakeExportService{data: []byte(`{
			"project_id": "proj-a",
			"artifacts": [
				{"id":"req-weak","project_id":"proj-a","type":"requirement","title":"Speed","body":"The system should be fast and user-friendly."},
				{"id":"heading-1","project_id":"proj-a","type":"heading","title":"Section","body":"Intro"}
			]
		}`)}
	})
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
	newQualityHandler(t).GetProjectQuality(w, reqWithViewer("proj-a", "proj-a"))
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
	newQualityHandler(t).GetProjectQuality(w, r) // no user in context
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %q)", w.Code, w.Body.String())
	}
}

// TestArtifactQualityShape confirms the single-artifact endpoint returns a
// score for a requirement.
func TestArtifactQualityShape(t *testing.T) {
	w := httptest.NewRecorder()
	newQualityHandler(t).GetArtifactQuality(w, reqWithViewer("req-weak", "proj-a"))
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
	newQualityHandler(t).GetArtifactQuality(w, reqWithViewer("heading-1", "proj-a"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
	}
}

// qualityLinks serves a fixed link list to the lint's GetLinksFrom and
// GetLinksTo.
type qualityLinks struct {
	links.Service
	all []*links.Link
}

func (f *qualityLinks) GetLinksFrom(id string) ([]*links.Link, error) {
	var out []*links.Link
	for _, l := range f.all {
		if l.FromID == id {
			out = append(out, l)
		}
	}
	return out, nil
}

func (f *qualityLinks) GetLinksTo(id string) ([]*links.Link, error) {
	var out []*links.Link
	for _, l := range f.all {
		if l.ToID == id {
			out = append(out, l)
		}
	}
	return out, nil
}

// unlinkedCitations lists the unlinked-citation findings' matches.
func unlinkedCitations(findings []quality.Finding) []string {
	var out []string
	for _, f := range findings {
		if f.Rule == quality.RuleUnlinkedCitation {
			out = append(out, f.Match)
		}
	}
	return out
}

// TestTheReportAndTheLintAgreeOnACrossProjectCitation pins OpenV REQ-164's
// "the same rule when it lints one artifact and when it lints a whole
// project" for a citation of an artifact of another project the artifact
// refines (REQ-145): the project report now counts it as linked, as the
// artifact's own lint does, where it used to flag it, since it named a
// link's ends from the project's own artifacts only. A citation of an
// artifact it holds no link to is flagged by both.
func TestTheReportAndTheLintAgreeOnACrossProjectCitation(t *testing.T) {
	h := newQualityHandler(t)
	arts := h.artifactService.(*fakeArtifactService).byID
	arts["req-cites"] = &artifacts.Artifact{ID: "req-cites", ProjectID: "proj-a", Ref: "REQ-1",
		Type: artifacts.TypeRequirement, Title: "Archive",
		Body: "The archive shall keep each record #REQ-4 names, as ##REQ-7 does."}
	arts["far-req"] = &artifacts.Artifact{ID: "far-req", ProjectID: "proj-b", Ref: "REQ-4",
		Type: artifacts.TypeRequirement, Title: "Records", Body: "Records shall be kept."}
	h.linkService = &qualityLinks{all: []*links.Link{{ID: "l1", FromID: "req-cites", ToID: "far-req", Type: "refines"}}}
	h.exportService = &fakeExportService{data: []byte(`{
		"project_id": "proj-a",
		"artifacts": [
			{"id":"req-cites","project_id":"proj-a","ref":"REQ-1","type":"requirement","title":"Archive",
			 "body":"The archive shall keep each record #REQ-4 names, as ##REQ-7 does."}
		],
		"links": [{"id":"l1","from_id":"req-cites","to_id":"far-req","type":"refines"}],
		"linked_artifacts": [{"id":"far-req","project_id":"proj-b","project_name":"Other","ref":"REQ-4",
			"type":"requirement","title":"Records","status":"draft"}]
	}`)}

	w := httptest.NewRecorder()
	h.GetArtifactQuality(w, reqWithViewer("req-cites", "proj-a"))
	if w.Code != http.StatusOK {
		t.Fatalf("lint: status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	var alone quality.ArtifactScore
	if err := json.Unmarshal(w.Body.Bytes(), &alone); err != nil {
		t.Fatalf("decode the lint: %v", err)
	}

	w = httptest.NewRecorder()
	h.GetProjectQuality(w, reqWithViewer("proj-a", "proj-a"))
	if w.Code != http.StatusOK {
		t.Fatalf("report: status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	var report quality.Report
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode the report: %v", err)
	}
	if len(report.Entries) != 1 {
		t.Fatalf("report entries = %d, want 1", len(report.Entries))
	}

	want := []string{"##REQ-7"}
	if got := unlinkedCitations(alone.Findings); !slices.Equal(got, want) {
		t.Errorf("the lint flagged %v, want %v", got, want)
	}
	if got := unlinkedCitations(report.Entries[0].Findings); !slices.Equal(got, want) {
		t.Errorf("the report flagged %v, want %v: #REQ-4 cites the linked REQ-4 of another project", got, want)
	}
	if alone.Score != report.Entries[0].Score {
		t.Errorf("the lint scores %d and the report %d: the two must agree", alone.Score, report.Entries[0].Score)
	}
}
