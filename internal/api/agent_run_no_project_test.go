package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// TestListingTheRunsWithNoProject pins GET /agent-runs?project=none, the
// workspace Runs page's listing (#379 bug 168): the active workspace's runs
// with no project, scoped as requireRunAccess scopes such a run. A
// workspace admin, or a platform admin, lists every one of them; anyone
// else only the ones they launched. A value it does not know, or a
// project_id beside it, is refused before anything is listed.
func TestListingTheRunsWithNoProject(t *testing.T) {
	const orgID = "org-1"
	newFixture := func() (*Handler, *fakeRunService) {
		runSvc := &fakeRunService{}
		h := newTestHandler(t, func(h *Handler) {
			h.RunService = runSvc
			h.OrgService = &fakeOrgService{roles: map[string]map[string]string{
				orgID: {"admin": orgs.RoleAdmin, "member": orgs.RoleMember},
			}}
			h.ProjectService = &fakeProjectService{byID: map[string]*projects.Project{
				"proj-1": {ID: "proj-1", OrgID: orgID},
			}}
			h.MemberService = &fakeMemberService{roles: map[string]map[string]string{
				"proj-1": {"member": members.RoleViewer},
			}}
		})
		return h, runSvc
	}
	list := func(h *Handler, user *users.User, query string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/agent-runs"+query, nil)
		ctx := context.WithValue(r.Context(), ctxUser, user)
		ctx = context.WithValue(ctx, ctxActiveOrg, orgID)
		w := httptest.NewRecorder()
		h.ListAgentRuns(w, r.WithContext(ctx))
		return w
	}

	for _, tc := range []struct {
		name         string
		user         *users.User
		wantLauncher string
	}{
		{"a member lists only the runs they launched", &users.User{ID: "member"}, "member"},
		{"a workspace admin lists every one", &users.User{ID: "admin"}, ""},
		{"a platform admin lists every one", &users.User{ID: "root", IsAdmin: true}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, runSvc := newFixture()
			if w := list(h, tc.user, "?project=none&limit=200"); w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
			}
			if len(runSvc.listFilters) != 1 {
				t.Fatalf("List called %d times, want 1", len(runSvc.listFilters))
			}
			filter := runSvc.listFilters[0]
			if !filter.NoProject || filter.ProjectID != "" {
				t.Errorf("filter = %+v, want the runs with no project", filter)
			}
			if filter.OrgID != orgID {
				t.Errorf("filter.OrgID = %q, want the active workspace %q", filter.OrgID, orgID)
			}
			if filter.LaunchedBy != tc.wantLauncher {
				t.Errorf("filter.LaunchedBy = %q, want %q", filter.LaunchedBy, tc.wantLauncher)
			}
			if filter.Limit != 200 {
				t.Errorf("filter.Limit = %d, want 200", filter.Limit)
			}
		})
	}

	t.Run("without project=none the listing is as before", func(t *testing.T) {
		h, runSvc := newFixture()
		if w := list(h, &users.User{ID: "admin"}, ""); w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if runSvc.listFilters[0].NoProject {
			t.Errorf("filter = %+v, want every run of the workspace", runSvc.listFilters[0])
		}
	})

	for _, tc := range []struct {
		name, query, want string
	}{
		{"a value it does not know", "?project=all", `project must be "none"`},
		{"a project beside it", "?project=none&project_id=proj-1", "project=none cannot be combined with project_id"},
	} {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			h, runSvc := newFixture()
			w := list(h, &users.User{ID: "admin"}, tc.query)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
			}
			var body struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Error != tc.want {
				t.Errorf("answer %q, want the error %q", w.Body.String(), tc.want)
			}
			if len(runSvc.listFilters) != 0 {
				t.Errorf("List called with %+v, want no listing", runSvc.listFilters)
			}
		})
	}
}

// TestARunWithNoProjectIsNoMembersToKnowOf pins #379 bug 172: a run with
// no project is its launcher's and its workspace admins' alone, and a
// member who is neither may not read it, so every route that looks the run
// up answers them as it answers an outsider and an id no row has, 404
// "agent run not found", byte for byte. The workspace guard's 403 "workspace
// admin access required" told them the run id exists. Who passes is
// unchanged: the launcher and a workspace admin read it, and a project's
// viewer, who may read its run, is still refused its cancel with the 403.
func TestARunWithNoProjectIsNoMembersToKnowOf(t *testing.T) {
	const orgID = "org-1"
	launcher := "launcher"
	project := "proj-1"
	newFixture := func() (*Handler, *fakeRunService) {
		runSvc := &fakeRunService{byID: map[string]*agentruns.Run{
			"run-none":    {ID: "run-none", OrgID: orgID, AgentID: "agent-1", Status: agentruns.StatusFailed, LaunchedBy: &launcher},
			"run-project": {ID: "run-project", OrgID: orgID, AgentID: "agent-1", ProjectID: &project, Status: agentruns.StatusFailed, LaunchedBy: &launcher},
		}}
		h := newTestHandler(t, func(h *Handler) {
			h.RunService = runSvc
			h.OrgService = &fakeOrgService{roles: map[string]map[string]string{
				orgID: {"admin": orgs.RoleAdmin, "member": orgs.RoleMember, "viewer": orgs.RoleMember, launcher: orgs.RoleMember},
			}}
			h.ProjectService = &fakeProjectService{byID: map[string]*projects.Project{
				project: {ID: project, OrgID: orgID},
			}}
			h.MemberService = &fakeMemberService{roles: map[string]map[string]string{
				project: {"viewer": members.RoleViewer},
			}}
		})
		return h, runSvc
	}
	call := func(h *Handler, route func(*Handler, http.ResponseWriter, *http.Request), method, userID, runID string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/agent-runs/"+runID, nil)
		ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: userID})
		ctx = context.WithValue(ctx, ctxActiveOrg, orgID)
		w := httptest.NewRecorder()
		route(h, w, mux.SetURLVars(r.WithContext(ctx), map[string]string{"id": runID}))
		return w
	}

	for _, route := range []struct {
		name    string
		method  string
		handler func(*Handler, http.ResponseWriter, *http.Request)
	}{
		{"GET /agent-runs/{id}", http.MethodGet, (*Handler).GetAgentRun},
		{"GET /agent-runs/{id}/tree", http.MethodGet, (*Handler).GetAgentRunTree},
		{"GET /agent-runs/{id}/logs", http.MethodGet, (*Handler).GetAgentRunLogs},
		{"GET /agent-runs/{id}/stream", http.MethodGet, (*Handler).StreamAgentRun},
		{"POST /agent-runs/{id}/cancel", http.MethodPost, (*Handler).CancelAgentRun},
		{"POST /agent-runs/{id}/retry", http.MethodPost, (*Handler).RetryAgentRun},
	} {
		t.Run(route.name, func(t *testing.T) {
			h, runSvc := newFixture()
			phantom := call(h, route.handler, route.method, "outsider", "run-missing")
			assertNotFound(t, phantom, "agent run not found")
			for _, caller := range []string{"member", "outsider"} {
				w := call(h, route.handler, route.method, caller, "run-none")
				if w.Code != phantom.Code || w.Body.String() != phantom.Body.String() {
					t.Errorf("%s: %d %q, want the answer to a run no row has, %d %q",
						caller, w.Code, w.Body.String(), phantom.Code, phantom.Body.String())
				}
			}
			if len(runSvc.retryCalls) != 0 {
				t.Errorf("Retry called for %v, want nothing retried", runSvc.retryCalls)
			}
		})
	}

	t.Run("who reads the run is unchanged", func(t *testing.T) {
		h, _ := newFixture()
		for _, caller := range []string{launcher, "admin"} {
			if w := call(h, (*Handler).GetAgentRun, http.MethodGet, caller, "run-none"); w.Code != http.StatusOK {
				t.Errorf("%s: status = %d, want 200 (body %q)", caller, w.Code, w.Body.String())
			}
		}
		if w := call(h, (*Handler).GetAgentRun, http.MethodGet, "viewer", "run-project"); w.Code != http.StatusOK {
			t.Errorf("a project viewer: status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		w := call(h, (*Handler).CancelAgentRun, http.MethodPost, "viewer", "run-project")
		if w.Code != http.StatusForbidden {
			t.Errorf("a project viewer's cancel: status = %d, want 403 (body %q)", w.Code, w.Body.String())
		}
	})
}
