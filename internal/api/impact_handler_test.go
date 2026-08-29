package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// newImpactHandler builds a handler whose project export is served from a
// baseline snapshot, so GetImpact runs without a live export service. The
// snapshot is a small traceability chain:
//
//	need N1 <-derives-from- req R1 <-satisfies- design D1
//	req R1 <-verifies- test T1
func newImpactHandler() *Handler {
	snapshot := `{
		"project_name": "Widget Spec",
		"project_id": "proj-a",
		"artifacts": [
			{"id": "N1", "type": "user-need", "title": "Need 1", "version": 1},
			{"id": "R1", "type": "requirement", "title": "Requirement 1", "version": 1},
			{"id": "D1", "type": "design-item", "title": "Design 1", "version": 1},
			{"id": "T1", "type": "test-case", "title": "Test 1", "version": 1}
		],
		"links": [
			{"id": "l1", "from_id": "R1", "to_id": "N1", "type": "derives-from"},
			{"id": "l2", "from_id": "D1", "to_id": "R1", "type": "satisfies"},
			{"id": "l3", "from_id": "T1", "to_id": "R1", "type": "verifies"}
		]
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
	}
}

func impactRequest(userID, query string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/projects/proj-a/impact?baseline_id=base-own"+query, nil)
	if userID != "" {
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: userID}))
	}
	return mux.SetURLVars(r, map[string]string{"id": "proj-a"})
}

func TestGetImpact_ViewerBothDirections(t *testing.T) {
	w := httptest.NewRecorder()
	newImpactHandler().GetImpact(w, impactRequest("viewer-a", "&artifact=R1"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	var rep vv.ImpactReport
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatalf("decode: %v (body %q)", err, w.Body.String())
	}
	if rep.ArtifactID != "R1" || rep.Direction != vv.DirectionBoth {
		t.Errorf("seed/direction = %q/%q, want R1/both", rep.ArtifactID, rep.Direction)
	}
	// Downstream: D1 + T1 depend on R1. Upstream: N1. Total distinct = 3.
	if rep.Total != 3 {
		t.Errorf("total = %d, want 3", rep.Total)
	}
	if len(rep.Downstream) == 0 || len(rep.Upstream) == 0 {
		t.Errorf("both directions should be populated: down=%d up=%d", len(rep.Downstream), len(rep.Upstream))
	}
}

func TestGetImpact_DirectionFilter(t *testing.T) {
	w := httptest.NewRecorder()
	newImpactHandler().GetImpact(w, impactRequest("viewer-a", "&artifact=R1&direction=upstream"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var rep vv.ImpactReport
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rep.Direction != vv.DirectionUpstream {
		t.Errorf("direction = %q, want upstream", rep.Direction)
	}
	if len(rep.Downstream) != 0 {
		t.Errorf("upstream request should not populate downstream")
	}
	if rep.Total != 1 { // only N1
		t.Errorf("total = %d, want 1", rep.Total)
	}
}

func TestGetImpact_MissingArtifactParam(t *testing.T) {
	w := httptest.NewRecorder()
	newImpactHandler().GetImpact(w, impactRequest("viewer-a", ""))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestGetImpact_UnknownArtifact(t *testing.T) {
	w := httptest.NewRecorder()
	newImpactHandler().GetImpact(w, impactRequest("viewer-a", "&artifact=nope"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %q)", w.Code, w.Body.String())
	}
}

func TestGetImpact_ForbiddenForNonMember(t *testing.T) {
	w := httptest.NewRecorder()
	newImpactHandler().GetImpact(w, impactRequest("stranger", "&artifact=R1"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %q)", w.Code, w.Body.String())
	}
}
