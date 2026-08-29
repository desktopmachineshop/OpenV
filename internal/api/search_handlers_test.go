package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/embeddings"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// --- fakes for the embeddings service (provider + store) so the semantic /
// hybrid modes can be exercised without a real provider or database.

type fakeEmbedProvider struct{ enabled bool }

func (f fakeEmbedProvider) Enabled() bool { return f.enabled }
func (fakeEmbedProvider) Model() string   { return "test-model" }
func (fakeEmbedProvider) Embed(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{0.1, 0.2, 0.3}
	}
	return out, nil
}

type fakeEmbedStore struct {
	near          []embeddings.NearestHit
	gotProjectIDs []string
}

func (*fakeEmbedStore) Upsert(*embeddings.Embedding) error { return nil }
func (*fakeEmbedStore) GetByArtifact(string) (*embeddings.Embedding, error) {
	return nil, nil
}

func (f *fakeEmbedStore) NearestByEmbedding(projectIDs []string, _ []float32, limit int) ([]embeddings.NearestHit, error) {
	f.gotProjectIDs = projectIDs
	allowed := map[string]bool{}
	for _, id := range projectIDs {
		allowed[id] = true
	}
	out := []embeddings.NearestHit{}
	for _, n := range f.near {
		if allowed[n.ProjectID] {
			out = append(out, n)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (*fakeEmbedStore) DuplicateCandidates(string, float64, int) ([]embeddings.DuplicatePair, error) {
	return nil, nil
}

// TestGlobalSearchModes covers the mode dispatch and mode_used reporting:
// keyword (default), semantic when embeddings are enabled, semantic falling
// back to keyword when embeddings are disabled, and hybrid blending both.
func TestGlobalSearchModes(t *testing.T) {
	const orgID = "org-1"

	newHandler := func(embedSvc *embeddings.Service) (*Handler, *searchArtifactService) {
		artifactSvc := &searchArtifactService{hits: []*artifacts.SearchHit{
			{ArtifactID: "art-1", ProjectID: "proj-1", Type: "requirement", Title: "Login flow", Snippet: "the login flow shall"},
		}}
		h := &Handler{
			artifactService:  artifactSvc,
			embeddingService: embedSvc,
			projectService: &searchProjectService{list: []*projects.Project{
				{ID: "proj-1", OrgID: orgID, Name: "Alpha"},
				{ID: "proj-2", OrgID: orgID, Name: "Beta"},
				{ID: "proj-other-org", OrgID: "org-2", Name: "Elsewhere"},
			}},
			orgService: &fakeOrgService{roles: map[string]map[string]string{
				orgID: {"admin": orgs.RoleAdmin},
			}},
			memberService: &fakeMemberService{roles: map[string]map[string]string{}},
		}
		return h, artifactSvc
	}

	do := func(t *testing.T, h *Handler, urlSuffix string) (string, []artifacts.SearchHit) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/search"+urlSuffix, nil)
		ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: "admin"})
		ctx = context.WithValue(ctx, ctxActiveOrg, orgID)
		w := httptest.NewRecorder()
		h.GlobalSearch(w, r.WithContext(ctx))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		var resp struct {
			ModeUsed string                `json:"mode_used"`
			Hits     []artifacts.SearchHit `json:"hits"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v (body %q)", err, w.Body.String())
		}
		return resp.ModeUsed, resp.Hits
	}

	enabledStore := &fakeEmbedStore{near: []embeddings.NearestHit{
		{ArtifactID: "sem-1", ProjectID: "proj-2", Type: "requirement", Title: "Session expiry", Body: "sessions expire", Distance: 0.1},
		{ArtifactID: "sem-x", ProjectID: "proj-other-org", Type: "requirement", Title: "Leak", Body: "should not appear", Distance: 0.05},
	}}
	enabledSvc := embeddings.NewService(fakeEmbedProvider{enabled: true}, enabledStore, nil)
	disabledSvc := embeddings.NewService(fakeEmbedProvider{enabled: false}, enabledStore, nil)

	t.Run("default mode is keyword", func(t *testing.T) {
		h, _ := newHandler(enabledSvc)
		mode, hits := do(t, h, "?q=login")
		if mode != "keyword" {
			t.Fatalf("mode_used = %q, want keyword", mode)
		}
		if len(hits) != 1 || hits[0].ArtifactID != "art-1" {
			t.Fatalf("hits = %+v, want the keyword hit", hits)
		}
	})

	t.Run("semantic mode runs the vector path when enabled", func(t *testing.T) {
		h, _ := newHandler(enabledSvc)
		mode, hits := do(t, h, "?q=login&mode=semantic")
		if mode != "semantic" {
			t.Fatalf("mode_used = %q, want semantic", mode)
		}
		if len(hits) != 1 || hits[0].ArtifactID != "sem-1" {
			t.Fatalf("hits = %+v, want only the in-scope semantic hit (other-org excluded)", hits)
		}
		if hits[0].Score <= 0 {
			t.Errorf("semantic hit score = %v, want > 0", hits[0].Score)
		}
		if hits[0].ProjectName != "Beta" {
			t.Errorf("project_name = %q, want Beta", hits[0].ProjectName)
		}
	})

	t.Run("semantic falls back to keyword when embeddings disabled", func(t *testing.T) {
		h, _ := newHandler(disabledSvc)
		mode, hits := do(t, h, "?q=login&mode=semantic")
		if mode != "keyword" {
			t.Fatalf("mode_used = %q, want keyword (fallback)", mode)
		}
		if len(hits) != 1 || hits[0].ArtifactID != "art-1" {
			t.Fatalf("hits = %+v, want the keyword hit on fallback", hits)
		}
	})

	t.Run("semantic falls back when embedding service is nil", func(t *testing.T) {
		h, _ := newHandler(nil)
		mode, hits := do(t, h, "?q=login&mode=semantic")
		if mode != "keyword" {
			t.Fatalf("mode_used = %q, want keyword (nil service)", mode)
		}
		if len(hits) != 1 {
			t.Fatalf("hits = %+v, want the keyword hit", hits)
		}
	})

	t.Run("hybrid blends keyword and semantic", func(t *testing.T) {
		h, _ := newHandler(enabledSvc)
		mode, hits := do(t, h, "?q=login&mode=hybrid")
		if mode != "hybrid" {
			t.Fatalf("mode_used = %q, want hybrid", mode)
		}
		ids := map[string]bool{}
		for _, hit := range hits {
			ids[hit.ArtifactID] = true
		}
		if !ids["art-1"] || !ids["sem-1"] {
			t.Fatalf("hybrid hits = %+v, want both the keyword (art-1) and semantic (sem-1) hits", hits)
		}
		if ids["sem-x"] {
			t.Fatalf("hybrid leaked an out-of-scope semantic hit: %+v", hits)
		}
	})
}

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
		var resp struct {
			ModeUsed string                `json:"mode_used"`
			Hits     []artifacts.SearchHit `json:"hits"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decoding response: %v (body %q)", err, w.Body.String())
		}
		return w, resp.Hits
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
