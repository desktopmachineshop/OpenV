package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/quality"
	"github.com/openv/requirements-platform/internal/domain/users"
)

func strPtr(s string) *string { return &s }

func mapFixture() ([]*artifacts.Artifact, []*links.Link) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	arts := []*artifacts.Artifact{
		{ID: "h1", Ref: "HDG-1", Type: "heading", Title: "System", SortOrder: 1, Status: "draft", CreatedAt: base},
		{ID: "r1", Ref: "REQ-1", ParentID: strPtr("h1"), Type: "requirement", Title: "Authentication", SortOrder: 1, Status: "approved", CreatedAt: base},
		{ID: "r2", Ref: "REQ-2", ParentID: strPtr("h1"), Type: "requirement", Title: "Session timeout", SortOrder: 2, Status: "draft", CreatedAt: base},
		{ID: "t1", Ref: "TC-1", Type: "test-case", Title: "Login round-trip", SortOrder: 2, Status: "draft", CreatedAt: base},
		// Pre-ref artifact (e.g. from an old baseline snapshot).
		{ID: "0a1b2c3d-0000-0000-0000-000000000000", Type: "hazard", Title: "Legacy hazard", SortOrder: 3, Status: "draft", CreatedAt: base},
	}
	lks := []*links.Link{
		{ID: "l1", FromID: "t1", ToID: "r1", Type: "verifies"},
		{ID: "l2", FromID: "r2", ToID: "r1", Type: "refines", Suspect: true},
	}
	return arts, lks
}

// TestBuildAIMap: the outline nests by hierarchy, shows refs, hides the
// draft status, annotates links in both directions, marks suspect links,
// and falls back to a uuid-prefix pseudo-ref for pre-ref artifacts.
func TestBuildAIMap(t *testing.T) {
	arts, lks := mapFixture()
	got := buildAIMap("Demo", "live state", quality.DefaultRuleSet().Describe(), arts, lks, time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC))

	checks := []string{
		"# Demo — AI map",
		"Source: live state · Generated: 2026-02-03T04:05:06Z",
		"Artifacts: 5 (HAZ 1, HDG 1, REQ 2, TC 1) · Links: 2",
		"HDG-1 System\n",
		"  REQ-1 Authentication [approved] {←refines REQ-2?; ←verifies TC-1}",
		"  REQ-2 Session timeout {→refines REQ-1?}",
		"TC-1 Login round-trip {→verifies REQ-1}",
		"#0a1b2c3d Legacy hazard",
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("map missing %q\n--- got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "[draft]") {
		t.Errorf("draft status should be omitted:\n%s", got)
	}
}

type aiMapArtifactService struct {
	artifacts.Service
	arts []*artifacts.Artifact
}

func (f *aiMapArtifactService) GetArtifactsByProject(projectID string) ([]*artifacts.Artifact, error) {
	return f.arts, nil
}

type aiMapLinkService struct {
	links.Service
	lks []*links.Link
}

func (f *aiMapLinkService) GetAllLinks(projectID string) ([]*links.Link, error) { return f.lks, nil }

type fakeBaselineService struct {
	baselines.Service
	baseline *baselines.Baseline
}

func (f *fakeBaselineService) GetProjectBaseline(projectID, id string) (*baselines.Baseline, error) {
	if f.baseline != nil && f.baseline.ID == id && f.baseline.ProjectID == projectID {
		return f.baseline, nil
	}
	return nil, baselines.ErrNotFound
}

func aiMapRequest(query string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/projects/p1/ai-map"+query, nil)
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "root", IsAdmin: true}))
	return mux.SetURLVars(r, map[string]string{"id": "p1"})
}

// TestProjectAIMapHandler: the endpoint renders live state as markdown, and
// ?baseline_id renders from the baseline snapshot (or 404s when the
// baseline is not this project's).
func TestProjectAIMapHandler(t *testing.T) {
	arts, lks := mapFixture()
	h := &Handler{
		projectService: &fakeProjectService{byID: map[string]*projects.Project{
			"p1": {ID: "p1", Name: "Demo"},
		}},
		artifactService: &aiMapArtifactService{arts: arts},
		linkService:     &aiMapLinkService{lks: lks},
		baselineService: &fakeBaselineService{baseline: &baselines.Baseline{
			ID:        "b1",
			ProjectID: "p1",
			Name:      "v0.2.0",
			Snapshot:  []byte(`{"artifacts":[{"id":"r1","ref":"REQ-1","type":"requirement","title":"Old auth","sort_order":1,"status":"approved"}],"links":[]}`),
			CreatedAt: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		}},
	}

	w := httptest.NewRecorder()
	h.ProjectAIMap(w, aiMapRequest(""))
	if w.Code != http.StatusOK {
		t.Fatalf("live: status = %d (body: %s)", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if !strings.Contains(w.Body.String(), "REQ-1 Authentication [approved]") {
		t.Errorf("live map body missing artifact line:\n%s", w.Body.String())
	}

	w = httptest.NewRecorder()
	h.ProjectAIMap(w, aiMapRequest("?baseline_id=b1"))
	if w.Code != http.StatusOK {
		t.Fatalf("baseline: status = %d (body: %s)", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "REQ-1 Old auth [approved]") {
		t.Errorf("baseline map should render the snapshot, got:\n%s", body)
	}
	if !strings.Contains(body, `baseline "v0.2.0" (b1, captured 2026-01-15T00:00:00Z)`) {
		t.Errorf("baseline map should stamp its provenance, got:\n%s", body)
	}

	w = httptest.NewRecorder()
	h.ProjectAIMap(w, aiMapRequest("?baseline_id=someone-elses"))
	if w.Code != http.StatusNotFound {
		t.Errorf("foreign baseline: status = %d, want 404", w.Code)
	}
}
