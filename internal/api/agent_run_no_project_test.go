package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
