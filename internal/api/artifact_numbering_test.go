package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/projects"
)

// numberingArtifactService serves one small document and records whether the
// handler asked for the whole project.
type numberingArtifactService struct {
	artifacts.Service
	all       []*artifacts.Artifact
	fullReads int
}

func (f *numberingArtifactService) ListArtifacts(projectID, artifactType string) ([]*artifacts.Artifact, error) {
	f.fullReads++
	return f.all, nil
}

func (f *numberingArtifactService) ListArtifactsPage(projectID, artifactType string, limit, offset int) ([]*artifacts.Artifact, int, error) {
	// Serve a page that deliberately omits the root section, so a handler
	// that numbered from the page alone would get "1" wrong.
	page := f.all[1:]
	return page, len(f.all), nil
}

func numberingHandler(svc *numberingArtifactService) *Handler {
	return &Handler{
		artifactService: svc,
		projectService: &fakeProjectService{byID: map[string]*projects.Project{
			"proj-1": {ID: "proj-1", OrgID: "org-1"},
		}},
		memberService: &fakeMemberService{roles: map[string]map[string]string{
			"proj-1": {"viewer": members.RoleViewer},
		}},
	}
}

func numberingDoc() []*artifacts.Artifact {
	intro := &artifacts.Artifact{ID: "intro", ProjectID: "proj-1", Type: artifacts.TypeHeading, Ref: "HDG-1", Title: "Intro", SortOrder: 1}
	parent := "intro"
	background := &artifacts.Artifact{ID: "background", ProjectID: "proj-1", ParentID: &parent, Type: artifacts.TypeHeading, Ref: "HDG-2", Title: "Background", SortOrder: 1}
	req := &artifacts.Artifact{ID: "req", ProjectID: "proj-1", ParentID: &parent, Type: artifacts.TypeRequirement, Ref: "REQ-1", Title: "Brake", SortOrder: 2}
	return []*artifacts.Artifact{intro, background, req}
}

func listArtifacts(t *testing.T, h *Handler, query string) []map[string]interface{} {
	t.Helper()
	r := withUser(httptest.NewRequest(http.MethodGet, "/api/v1/artifacts?project_id=proj-1"+query, nil), "viewer")
	w := httptest.NewRecorder()
	h.ListArtifacts(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body %q)", w.Code, w.Body.String())
	}
	var got []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not an artifact list: %v", err)
	}
	return got
}

// TestListArtifactsNumbersFromTheWholeDocument: a page is a slice through the
// tree, so numbering has to come from every artifact in the project. Here the
// page starts at "Background", whose number (1.1) is only knowable from the
// root section that the page does not contain.
func TestListArtifactsNumbersFromTheWholeDocument(t *testing.T) {
	svc := &numberingArtifactService{all: numberingDoc()}
	got := listArtifacts(t, numberingHandler(svc), "&doc_numbers=1")

	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}
	if got[0]["doc_number"] != "1.1" {
		t.Errorf("background doc_number = %v, want 1.1", got[0]["doc_number"])
	}
	// A requirement is cited by its ref, not by a clause number.
	if _, ok := got[1]["doc_number"]; ok {
		t.Errorf("requirement carries a doc_number: %v", got[1]["doc_number"])
	}
	if got[1]["ref"] != "REQ-1" {
		t.Errorf("requirement ref = %v, want REQ-1", got[1]["ref"])
	}
	if svc.fullReads != 1 {
		t.Errorf("full-project reads = %d, want exactly 1", svc.fullReads)
	}
}

// TestListArtifactsSkipsNumberingByDefault keeps the extra whole-project read
// off the common paginated path: refs still ship (they are stored), numbers
// do not.
func TestListArtifactsSkipsNumberingByDefault(t *testing.T) {
	svc := &numberingArtifactService{all: numberingDoc()}
	got := listArtifacts(t, numberingHandler(svc), "")

	if svc.fullReads != 0 {
		t.Errorf("full-project reads = %d, want 0 without doc_numbers=1", svc.fullReads)
	}
	if _, ok := got[0]["doc_number"]; ok {
		t.Errorf("doc_number served without being asked for: %v", got[0]["doc_number"])
	}
	if got[0]["ref"] != "HDG-2" {
		t.Errorf("ref = %v, want HDG-2 (refs are stored, not derived)", got[0]["ref"])
	}
}
