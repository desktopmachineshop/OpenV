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

// --- fakes local to the search endpoint (authz_test.go style: embed the
// interface so only the touched methods need implementations).

type searchProjectService struct {
	projects.Service
	list []*projects.Project
}

// ListProjectsByOrg mirrors the SQL fail-closed contract: an empty orgID
// returns nothing, otherwise only the projects in that workspace.
func (f *searchProjectService) ListProjectsByOrg(orgID string) ([]*projects.Project, error) {
	if orgID == "" {
		return nil, nil
	}
	var out []*projects.Project
	for _, p := range f.list {
		if p.OrgID == orgID {
			out = append(out, p)
		}
	}
	return out, nil
}

type searchArtifactService struct {
	artifacts.Service
	hits []*artifacts.SearchHit

	gotProjectIDs []string
	gotQuery      string
	gotLimit      int
	calls         int
}

func (f *searchArtifactService) SearchArtifacts(projectIDs []string, query string, limit int) ([]*artifacts.SearchHit, error) {
	f.calls++
	f.gotProjectIDs = projectIDs
	f.gotQuery = query
	f.gotLimit = limit
	allowed := map[string]bool{}
	for _, id := range projectIDs {
		allowed[id] = true
	}
	out := []*artifacts.SearchHit{}
	for _, hit := range f.hits {
		copied := *hit
		if allowed[copied.ProjectID] {
			out = append(out, &copied)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// TestGlobalSearchScoping locks in that /api/v1/search only surfaces hits
// from projects the caller can access: org admins search the whole active
// workspace, plain members only their own projects, and projects from other
// workspaces never leak in.
func TestGlobalSearchScoping(t *testing.T) {
	const orgID = "org-1"

	newFixture := func() (*Handler, *searchArtifactService) {
		artifactSvc := &searchArtifactService{hits: []*artifacts.SearchHit{
			{ArtifactID: "art-1", ProjectID: "proj-1", Type: "requirement", Title: "Login flow", Snippet: "the login flow shall"},
			{ArtifactID: "art-2", ProjectID: "proj-2", Type: "requirement", Title: "Login audit", Snippet: "audit each login"},
			{ArtifactID: "art-3", ProjectID: "proj-other-org", Type: "requirement", Title: "Login theme", Snippet: ""},
		}}
		h := &Handler{
			artifactService: artifactSvc,
			projectService: &searchProjectService{list: []*projects.Project{
				{ID: "proj-1", OrgID: orgID, Name: "Alpha"},
				{ID: "proj-2", OrgID: orgID, Name: "Beta"},
				{ID: "proj-other-org", OrgID: "org-2", Name: "Elsewhere"},
			}},
			orgService: &fakeOrgService{roles: map[string]map[string]string{
				orgID: {"admin": orgs.RoleAdmin, "member": orgs.RoleMember},
			}},
			memberService: &fakeMemberService{roles: map[string]map[string]string{
				"proj-1": {"member": members.RoleViewer},
			}},
		}
		return h, artifactSvc
	}

	search := func(t *testing.T, h *Handler, userID, query string) (*httptest.ResponseRecorder, []artifacts.SearchHit) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/search"+query, nil)
		ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: userID})
		ctx = context.WithValue(ctx, ctxActiveOrg, orgID)
		w := httptest.NewRecorder()
		h.GlobalSearch(w, r.WithContext(ctx))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		var hits []artifacts.SearchHit
		if err := json.Unmarshal(w.Body.Bytes(), &hits); err != nil {
			t.Fatalf("decoding response: %v (body %q)", err, w.Body.String())
		}
		return w, hits
	}

	t.Run("unauthenticated gets 401", func(t *testing.T) {
		h, svc := newFixture()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=login", nil)
		w := httptest.NewRecorder()
		h.GlobalSearch(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (body %q)", w.Code, w.Body.String())
		}
		if svc.calls != 0 {
			t.Fatalf("search service called %d times on denied request, want 0", svc.calls)
		}
	})

	t.Run("member only searches their projects", func(t *testing.T) {
		h, svc := newFixture()
		_, hits := search(t, h, "member", "?q=login")
		if len(svc.gotProjectIDs) != 1 || svc.gotProjectIDs[0] != "proj-1" {
			t.Fatalf("searched projects = %v, want only proj-1", svc.gotProjectIDs)
		}
		if len(hits) != 1 || hits[0].ArtifactID != "art-1" {
			t.Fatalf("hits = %+v, want only the proj-1 hit", hits)
		}
		if hits[0].ProjectName != "Alpha" {
			t.Errorf("project_name = %q, want %q", hits[0].ProjectName, "Alpha")
		}
	})

	t.Run("org admin searches all workspace projects but not other orgs", func(t *testing.T) {
		h, svc := newFixture()
		_, hits := search(t, h, "admin", "?q=login")
		if len(svc.gotProjectIDs) != 2 {
			t.Fatalf("searched projects = %v, want proj-1 and proj-2", svc.gotProjectIDs)
		}
		if len(hits) != 2 {
			t.Fatalf("hits = %+v, want 2 in-workspace hits", hits)
		}
		for _, hit := range hits {
			if hit.ProjectID == "proj-other-org" {
				t.Fatalf("hit from another workspace leaked: %+v", hit)
			}
		}
	})

	t.Run("member with no accessible projects gets empty list without a search", func(t *testing.T) {
		h, svc := newFixture()
		_, hits := search(t, h, "stranger", "?q=login")
		if len(hits) != 0 {
			t.Fatalf("hits = %+v, want none", hits)
		}
		if svc.calls != 0 {
			t.Fatalf("search service called %d times with no accessible projects, want 0", svc.calls)
		}
	})

	t.Run("blank query short-circuits to empty list", func(t *testing.T) {
		h, svc := newFixture()
		_, hits := search(t, h, "admin", "?q=%20%20")
		if len(hits) != 0 {
			t.Fatalf("hits = %+v, want none for a blank query", hits)
		}
		if svc.calls != 0 {
			t.Fatalf("search service called %d times for a blank query, want 0", svc.calls)
		}
	})

	t.Run("limit defaults to 20 and caps at 50", func(t *testing.T) {
		h, svc := newFixture()
		search(t, h, "admin", "?q=login")
		if svc.gotLimit != 20 {
			t.Errorf("default limit = %d, want 20", svc.gotLimit)
		}
		search(t, h, "admin", "?q=login&limit=999")
		if svc.gotLimit != 50 {
			t.Errorf("capped limit = %d, want 50", svc.gotLimit)
		}
		search(t, h, "admin", "?q=login&limit=5")
		if svc.gotLimit != 5 {
			t.Errorf("explicit limit = %d, want 5", svc.gotLimit)
		}
	})
}
