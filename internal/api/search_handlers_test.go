package api

import (
	"context"
	"encoding/json"
	"errors"
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

	t.Run("a ref query takes the keyword path whatever mode was asked for", func(t *testing.T) {
		// "REQ-30" is an address, not a phrase. Nearest-neighbour ranking has
		// nothing to say about an identifier, so semantic and hybrid both hand
		// it to the keyword path, which is the one that matches refs.
		for _, suffix := range []string{"?q=REQ-30&mode=semantic", "?q=req-30&mode=hybrid", "?q=REQ-30"} {
			h, artifactSvc := newHandler(enabledSvc)
			mode, hits := do(t, h, suffix)
			if mode != "keyword" {
				t.Errorf("%s: mode_used = %q, want keyword", suffix, mode)
			}
			if artifactSvc.calls != 1 {
				t.Errorf("%s: keyword service called %d times, want 1", suffix, artifactSvc.calls)
			}
			for _, hit := range hits {
				if hit.ArtifactID == "sem-1" {
					t.Errorf("%s: semantic hit in results, want the keyword path only", suffix)
				}
			}
		}
	})

	t.Run("a workspace without the feature keeps the mode it asked for", func(t *testing.T) {
		// A stable-channel workspace still on 0.9.2 has not received
		// search-by-ref, so "REQ-30" is just a phrase: semantic mode stays
		// semantic and the keyword path is told not to match refs.
		onOldStable := &fakeOrgService{
			roles:         map[string]map[string]string{orgID: {"admin": orgs.RoleAdmin}},
			plan:          orgs.PlanTeam,
			stableRelease: "0.9.2",
		}

		h, _ := newHandler(enabledSvc)
		h.orgService = onOldStable
		if mode, _ := do(t, h, "?q=REQ-30&mode=semantic"); mode != "semantic" {
			t.Errorf("mode_used = %q, want semantic — the ref routing is gated", mode)
		}

		h, artifactSvc := newHandler(enabledSvc)
		h.orgService = onOldStable
		do(t, h, "?q=REQ-30")
		if artifactSvc.gotOpts.MatchRefs {
			t.Error("keyword search asked to match refs for a workspace without the feature")
		}
	})

	t.Run("the feature asks the keyword path to match refs", func(t *testing.T) {
		h, artifactSvc := newHandler(enabledSvc)
		do(t, h, "?q=REQ-30")
		if !artifactSvc.gotOpts.MatchRefs {
			t.Error("keyword search not asked to match refs for a workspace that has the feature")
		}
	})

	t.Run("a ref-shaped phrase still searches text", func(t *testing.T) {
		// "ISO-9001" parses as a ref even though no artifact has that ref, so it
		// routes to keyword too. That has to stay lossless: keyword matches
		// titles and bodies as well as refs, so a phrase search still works.
		h, artifactSvc := newHandler(enabledSvc)
		mode, hits := do(t, h, "?q=ISO-9001")
		if mode != "keyword" {
			t.Fatalf("mode_used = %q, want keyword", mode)
		}
		if artifactSvc.gotQuery != "ISO-9001" {
			t.Errorf("keyword query = %q, want the phrase passed through unchanged", artifactSvc.gotQuery)
		}
		if len(hits) != 1 || hits[0].ArtifactID != "art-1" {
			t.Errorf("hits = %+v, want the text match", hits)
		}
	})
}

// errEmbedProvider is an enabled provider whose Embed always fails with a
// non-sentinel error — the "provider is down / query failed" case for #243.
type errEmbedProvider struct{}

func (errEmbedProvider) Enabled() bool { return true }
func (errEmbedProvider) Model() string { return "test-model" }
func (errEmbedProvider) Embed([]string) ([][]float32, error) {
	return nil, errors.New("embedding provider unavailable")
}

// TestGlobalSearchProviderErrorFallsBack covers issue #243: when the embedding
// query fails with a plain provider/query error (not ErrDisabled /
// ErrVectorUnavailable), both semantic and hybrid degrade to keyword with
// mode_used=keyword instead of 500ing.
func TestGlobalSearchProviderErrorFallsBack(t *testing.T) {
	const orgID = "org-1"

	newHandler := func() *Handler {
		artifactSvc := &searchArtifactService{hits: []*artifacts.SearchHit{
			{ArtifactID: "art-1", ProjectID: "proj-1", Type: "requirement", Title: "Login flow", Snippet: "the login flow shall"},
		}}
		// Enabled service, but the provider errors on Embed and the store also
		// satisfies Searcher (so the failure is the provider's, not "no vector
		// path").
		svc := embeddings.NewService(errEmbedProvider{}, &fakeEmbedStore{}, nil)
		return &Handler{
			artifactService:  artifactSvc,
			embeddingService: svc,
			projectService: &searchProjectService{list: []*projects.Project{
				{ID: "proj-1", OrgID: orgID, Name: "Alpha"},
			}},
			orgService: &fakeOrgService{roles: map[string]map[string]string{
				orgID: {"admin": orgs.RoleAdmin},
			}},
			memberService: &fakeMemberService{roles: map[string]map[string]string{}},
		}
	}

	do := func(t *testing.T, urlSuffix string) (int, string, []artifacts.SearchHit) {
		t.Helper()
		h := newHandler()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/search"+urlSuffix, nil)
		ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: "admin"})
		ctx = context.WithValue(ctx, ctxActiveOrg, orgID)
		w := httptest.NewRecorder()
		h.GlobalSearch(w, r.WithContext(ctx))
		var resp struct {
			ModeUsed string                `json:"mode_used"`
			Hits     []artifacts.SearchHit `json:"hits"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return w.Code, resp.ModeUsed, resp.Hits
	}

	t.Run("semantic provider error falls back to keyword", func(t *testing.T) {
		code, mode, hits := do(t, "?q=login&mode=semantic")
		if code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (no 500 on provider error)", code)
		}
		if mode != "keyword" {
			t.Fatalf("mode_used = %q, want keyword (fallback)", mode)
		}
		if len(hits) != 1 || hits[0].ArtifactID != "art-1" {
			t.Fatalf("hits = %+v, want the keyword hit", hits)
		}
	})

	t.Run("hybrid provider error returns keyword hits", func(t *testing.T) {
		code, mode, hits := do(t, "?q=login&mode=hybrid")
		if code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (no 500 on provider error)", code)
		}
		if mode != "keyword" {
			t.Fatalf("mode_used = %q, want keyword (hybrid degrades)", mode)
		}
		if len(hits) != 1 || hits[0].ArtifactID != "art-1" {
			t.Fatalf("hits = %+v, want the keyword hit", hits)
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
	gotOpts       artifacts.SearchOptions
	calls         int
}

func (f *searchArtifactService) SearchArtifacts(projectIDs []string, query string, limit int, opts artifacts.SearchOptions) ([]*artifacts.SearchHit, error) {
	f.calls++
	f.gotOpts = opts
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
