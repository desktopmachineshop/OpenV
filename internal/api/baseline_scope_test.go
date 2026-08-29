package api

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// fakeBaselineRepo backs the real baselines.DefaultService, so the ownership
// check under test is the production one, not a fake's reimplementation.
type fakeBaselineRepo struct {
	baselines.Repository
	byID map[string]*baselines.Baseline
}

func (f *fakeBaselineRepo) GetByID(id string) (*baselines.Baseline, error) {
	if b, ok := f.byID[id]; ok {
		return b, nil
	}
	return nil, errors.New("baseline not found")
}

// fakeVVService is declared in vv_result_handler_test.go; the baseline-scope
// endpoints additionally read latest results and runs.
func (f *fakeVVService) LatestResults(projectID string) (map[string]*vv.TestResult, error) {
	return map[string]*vv.TestResult{}, nil
}

func (f *fakeVVService) ListRuns(projectID string) ([]*vv.TestRun, error) {
	return nil, nil
}

// TestBaselineLoadsScopedToProject locks in that every endpoint resolving a
// ?baseline_id= against a project URL refuses a baseline from another project
// with a 404 that is indistinguishable from a missing ID — the foreign
// snapshot must never influence the response. A viewer on proj-a supplies the
// ID of a proj-b baseline whose snapshot carries a canary string.
func TestBaselineLoadsScopedToProject(t *testing.T) {
	const canary = "FOREIGN-SECRET-PRODUCT"

	newHandler := func() *Handler {
		baselineSvc := baselines.NewService(&fakeBaselineRepo{byID: map[string]*baselines.Baseline{
			"base-own": {
				ID:        "base-own",
				ProjectID: "proj-a",
				Name:      "Own baseline",
				Snapshot:  json.RawMessage(`{"project_name":"Own Product"}`),
			},
			"base-foreign": {
				ID:        "base-foreign",
				ProjectID: "proj-b",
				Name:      canary,
				Snapshot:  json.RawMessage(`{"project_name":"` + canary + `"}`),
			},
		}})
		return &Handler{
			projectService: &fakeProjectService{byID: map[string]*projects.Project{
				"proj-a": {ID: "proj-a", OrgID: "org-1"},
				"proj-b": {ID: "proj-b", OrgID: "org-2"},
			}},
			orgService: &fakeOrgService{roles: map[string]map[string]string{"org-1": {}}},
			memberService: &fakeMemberService{roles: map[string]map[string]string{
				"proj-a": {"viewer-a": members.RoleViewer},
			}},
			baselineService: baselineSvc,
			vvService:       &fakeVVService{},
			reportService:   reports.NewService(nil, baselineSvc),
		}
	}

	endpoints := []struct {
		name string
		call func(h *Handler, w http.ResponseWriter, r *http.Request)
	}{
		{"vv coverage", (*Handler).GetCoverage},
		{"vv matrix", (*Handler).GetMatrix},
		{"vv gaps", (*Handler).GetGaps},
		{"vv report", (*Handler).GetVVReport},
		{"project report", (*Handler).GenerateReport},
	}

	request := func(baselineID string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/projects/proj-a/x?baseline_id="+baselineID, nil)
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "viewer-a"}))
		return mux.SetURLVars(r, map[string]string{"id": "proj-a"})
	}

	for _, ep := range endpoints {
		t.Run(ep.name+"/foreign baseline is 404", func(t *testing.T) {
			w := httptest.NewRecorder()
			ep.call(newHandler(), w, request("base-foreign"))
			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, http.StatusNotFound, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "baseline not found") {
				t.Fatalf("body %q should read as a plain missing baseline", w.Body.String())
			}
			if strings.Contains(w.Body.String(), canary) {
				t.Fatalf("response leaked the foreign project's snapshot: %q", w.Body.String())
			}
		})

		t.Run(ep.name+"/own baseline still works", func(t *testing.T) {
			w := httptest.NewRecorder()
			ep.call(newHandler(), w, request("base-own"))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
			}
		})
	}
}
