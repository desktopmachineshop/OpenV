package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// TestListProjectsFailsClosedOnEmptyActiveOrg locks in the issue-#180 fix: an
// admin whose active workspace can't be resolved (empty) sees no projects,
// rather than every tenant's. It also confirms a resolved workspace is scoped
// in the repository, so foreign-org projects never reach the handler.
func TestListProjectsFailsClosedOnEmptyActiveOrg(t *testing.T) {
	const orgID = "org-1"
	newHandler := func() *Handler {
		return &Handler{
			projectService: &fakeProjectService{byID: map[string]*projects.Project{
				"proj-1":     {ID: "proj-1", OrgID: orgID, Name: "Alpha"},
				"proj-2":     {ID: "proj-2", OrgID: orgID, Name: "Beta"},
				"proj-other": {ID: "proj-other", OrgID: "org-2", Name: "Elsewhere"},
			}},
			orgService: &fakeOrgService{roles: map[string]map[string]string{
				orgID: {"admin": orgs.RoleAdmin},
			}},
			memberService: &fakeMemberService{roles: map[string]map[string]string{}},
		}
	}

	list := func(t *testing.T, h *Handler, user *users.User, activeOrg string) []projects.Project {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
		ctx := context.WithValue(r.Context(), ctxUser, user)
		if activeOrg != "" {
			ctx = context.WithValue(ctx, ctxActiveOrg, activeOrg)
		}
		w := httptest.NewRecorder()
		h.ListProjects(w, r.WithContext(ctx))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		var out []projects.Project
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("decoding response: %v (body %q)", err, w.Body.String())
		}
		return out
	}

	t.Run("platform admin with no active org sees nothing", func(t *testing.T) {
		got := list(t, newHandler(), &users.User{ID: "admin", IsAdmin: true}, "")
		if len(got) != 0 {
			t.Fatalf("empty active org returned %d projects, want 0 (fail closed)", len(got))
		}
	})

	t.Run("org admin with no active org sees nothing", func(t *testing.T) {
		got := list(t, newHandler(), &users.User{ID: "admin"}, "")
		if len(got) != 0 {
			t.Fatalf("empty active org returned %d projects, want 0 (fail closed)", len(got))
		}
	})

	t.Run("resolved workspace is scoped and excludes other orgs", func(t *testing.T) {
		got := list(t, newHandler(), &users.User{ID: "admin", IsAdmin: true}, orgID)
		if len(got) != 2 {
			t.Fatalf("admin saw %d projects, want 2 in-workspace", len(got))
		}
		for _, p := range got {
			if p.OrgID != orgID {
				t.Fatalf("project from another workspace leaked: %+v", p)
			}
		}
	})
}

// TestGlobalSearchFailsClosedOnEmptyActiveOrg locks in that global search
// returns nothing (and never touches the artifact index) when the active
// workspace can't be resolved.
func TestGlobalSearchFailsClosedOnEmptyActiveOrg(t *testing.T) {
	artifactSvc := &searchArtifactService{hits: []*artifacts.SearchHit{
		{ArtifactID: "art-1", ProjectID: "proj-1", Title: "Login"},
	}}
	h := &Handler{
		artifactService: artifactSvc,
		projectService: &searchProjectService{list: []*projects.Project{
			{ID: "proj-1", OrgID: "org-1", Name: "Alpha"},
			{ID: "proj-other", OrgID: "org-2", Name: "Elsewhere"},
		}},
		orgService:    &fakeOrgService{roles: map[string]map[string]string{}},
		memberService: &fakeMemberService{roles: map[string]map[string]string{}},
	}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=login", nil)
	// Authenticated, but no active org resolved.
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "admin", IsAdmin: true}))
	w := httptest.NewRecorder()
	h.GlobalSearch(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	var hits []artifacts.SearchHit
	if err := json.Unmarshal(w.Body.Bytes(), &hits); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("empty active org returned %d hits, want 0 (fail closed)", len(hits))
	}
	if artifactSvc.calls != 0 {
		t.Fatalf("artifact search called %d times with no active org, want 0", artifactSvc.calls)
	}
}

// TestListAgentRunsFailsClosedOnEmptyActiveOrg locks in that the run listing
// hands the repository an empty OrgID (which fails closed in SQL) rather than a
// filter that would page across every tenant, even for a platform admin.
func TestListAgentRunsFailsClosedOnEmptyActiveOrg(t *testing.T) {
	runSvc := &fakeRunService{}
	h := &Handler{
		runService: runSvc,
		orgService: &fakeOrgService{roles: map[string]map[string]string{}},
		memberService: &fakeMemberService{roles: map[string]map[string]string{
			"proj-1": {"member": members.RoleViewer},
		}},
	}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/agent-runs", nil)
	// Platform admin, but no active org resolved.
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "admin", IsAdmin: true}))
	w := httptest.NewRecorder()
	h.ListAgentRuns(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	if len(runSvc.listFilters) != 1 {
		t.Fatalf("List called %d times, want 1", len(runSvc.listFilters))
	}
	if got := runSvc.listFilters[0].OrgID; got != "" {
		t.Fatalf("filter.OrgID = %q, want empty so the repo fails closed", got)
	}
}
