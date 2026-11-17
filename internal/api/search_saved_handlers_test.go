package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/savedsearches"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// fakeSavedSearchRepo is the in-memory repository behind the real service,
// so the handlers are tested with the service's own rules (replace by name,
// the per-scope cap).
type fakeSavedSearchRepo struct {
	rows []*savedsearches.SavedSearch
}

func (f *fakeSavedSearchRepo) Upsert(s *savedsearches.SavedSearch) error {
	for i, r := range f.rows {
		if r.UserID == s.UserID && r.OrgID == s.OrgID && r.Scope == s.Scope && r.Name == s.Name {
			s.ID, s.CreatedAt = r.ID, r.CreatedAt
			f.rows[i] = s
			return nil
		}
	}
	f.rows = append(f.rows, s)
	return nil
}

func (f *fakeSavedSearchRepo) ListVisible(userID, orgID, scope string) ([]*savedsearches.SavedSearch, error) {
	out := []*savedsearches.SavedSearch{}
	for _, r := range f.rows {
		if r.OrgID == orgID && r.Scope == scope && (r.UserID == userID || r.Shared) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeSavedSearchRepo) CountForUser(userID, orgID, scope string) (int, error) {
	n := 0
	for _, r := range f.rows {
		if r.UserID == userID && r.OrgID == orgID && r.Scope == scope {
			n++
		}
	}
	return n, nil
}

func (f *fakeSavedSearchRepo) DeleteForUser(userID, orgID, id string) (int64, error) {
	for i, r := range f.rows {
		if r.ID == id && r.UserID == userID && r.OrgID == orgID {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

func savedSearchReq(method, target, body, userID, org string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	ctx := r.Context()
	if userID != "" {
		ctx = context.WithValue(ctx, ctxUser, &users.User{ID: userID})
	}
	if org != "" {
		ctx = context.WithValue(ctx, ctxActiveOrg, org)
	}
	return r.WithContext(ctx)
}

func TestSavedSearchesRoundTrip(t *testing.T) {
	repo := &fakeSavedSearchRepo{}
	h := newTestHandler(t, func(h *Handler) {
		h.SavedSearchService = savedsearches.NewDefaultService(repo)
		h.OrgService = &fakeOrgService{}
	})
	router := mux.NewRouter()
	h.registerSavedSearchRoutes(router)
	serve := func(r *http.Request) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}

	// Signed out: every endpoint answers 401.
	for _, r := range []*http.Request{
		savedSearchReq("GET", "/api/v1/me/saved-searches?scope=requirements", "", "", "org-1"),
		savedSearchReq("POST", "/api/v1/me/saved-searches", `{"scope":"requirements","name":"x","value":{}}`, "", "org-1"),
		savedSearchReq("DELETE", "/api/v1/me/saved-searches/abc", "", "", "org-1"),
	} {
		if w := serve(r); w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s signed out = %d, want 401", r.Method, r.URL.Path, w.Code)
		}
	}

	w := serve(savedSearchReq("POST", "/api/v1/me/saved-searches",
		`{"scope":"requirements","name":"Release 1.4","value":{"chips":[{"kind":"filter","field":"status","values":["in_review"]}],"text":""},"shared":true}`,
		"dana", "org-1"))
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	var created savedsearches.SavedSearch
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created.ID == "" || created.UserID != "dana" || created.OrgID != "org-1" || !created.Shared {
		t.Fatalf("created %+v", created)
	}

	for _, tc := range []struct{ name, body string }{
		{"bad scope", `{"scope":"has space","name":"x","value":{}}`},
		{"no name", `{"scope":"requirements","name":"","value":{}}`},
		{"value not an object", `{"scope":"requirements","name":"x","value":"chips"}`},
	} {
		if w := serve(savedSearchReq("POST", "/api/v1/me/saved-searches", tc.body, "dana", "org-1")); w.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", tc.name, w.Code)
		}
	}
	if w := serve(savedSearchReq("POST", "/api/v1/me/saved-searches", `{"scope":"requirements","name":"x","value":{}}`, "dana", "")); w.Code != http.StatusBadRequest {
		t.Errorf("no active workspace = %d, want 400", w.Code)
	}
	if w := serve(savedSearchReq("GET", "/api/v1/me/saved-searches", "", "dana", "org-1")); w.Code != http.StatusBadRequest {
		t.Errorf("list without a scope = %d, want 400", w.Code)
	}

	list := func(user, org, scope string) []savedsearches.SavedSearch {
		t.Helper()
		w := serve(savedSearchReq("GET", "/api/v1/me/saved-searches?scope="+scope, "", user, org))
		if w.Code != http.StatusOK {
			t.Fatalf("list = %d", w.Code)
		}
		var body struct {
			SavedSearches []savedsearches.SavedSearch `json:"saved_searches"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body.SavedSearches == nil {
			t.Fatalf("list body %s: want an array", w.Body.String())
		}
		return body.SavedSearches
	}
	if got := list("dana", "org-1", "requirements"); len(got) != 1 || got[0].Name != "Release 1.4" {
		t.Fatalf("owner's list = %+v", got)
	}
	if got := list("sam", "org-1", "requirements"); len(got) != 1 {
		t.Fatalf("a shared search is listed for another member: %+v", got)
	}
	if got := list("sam", "org-2", "requirements"); len(got) != 0 {
		t.Fatalf("another workspace sees nothing: %+v", got)
	}
	if got := list("dana", "org-1", "global-search"); len(got) != 0 {
		t.Fatalf("another scope sees nothing: %+v", got)
	}

	// Another member cannot delete it, and is told it does not exist.
	if w := serve(savedSearchReq("DELETE", "/api/v1/me/saved-searches/"+created.ID, "", "sam", "org-1")); w.Code != http.StatusNotFound {
		t.Fatalf("delete by another member = %d, want 404", w.Code)
	}
	if w := serve(savedSearchReq("DELETE", "/api/v1/me/saved-searches/"+created.ID, "", "dana", "org-1")); w.Code != http.StatusNoContent {
		t.Fatalf("delete by owner = %d, want 204", w.Code)
	}
	if got := list("dana", "org-1", "requirements"); len(got) != 0 {
		t.Fatalf("deleted search still listed: %+v", got)
	}
}

func TestSavingASearchIsGatedOnTheFeature(t *testing.T) {
	repo := &fakeSavedSearchRepo{}
	h := newTestHandler(t, func(h *Handler) {
		h.SavedSearchService = savedsearches.NewDefaultService(repo)
		h.OrgService = &fakeOrgService{plan: orgs.PlanTeam, stableRelease: "0.9.2"}
	})
	w := httptest.NewRecorder()
	h.CreateSavedSearch(w, savedSearchReq("POST", "/api/v1/me/saved-searches", `{"scope":"requirements","name":"x","value":{}}`, "dana", "org-1"))
	if w.Code != http.StatusForbidden || len(repo.rows) != 0 {
		t.Fatalf("save on a stable channel without the feature = %d (%d rows), want 403", w.Code, len(repo.rows))
	}
	w = httptest.NewRecorder()
	h.ListSavedSearches(w, savedSearchReq("GET", "/api/v1/me/saved-searches?scope=requirements", "", "dana", "org-1"))
	if w.Code != http.StatusOK {
		t.Fatalf("listing is never gated: %d", w.Code)
	}
}

// TestGlobalSearchFacets: the chip search bar's project, type, status and
// owner chips reach the keyword search, a project chip only narrows the
// projects the caller can already search, and a workspace without the
// feature searches exactly as before.
func TestGlobalSearchFacets(t *testing.T) {
	const orgID = "org-1"
	newHandler := func(org *fakeOrgService) (*Handler, *searchArtifactService) {
		svc := &searchArtifactService{hits: []*artifacts.SearchHit{
			{ArtifactID: "a1", ProjectID: "proj-1", Type: "requirement", Title: "Login"},
			{ArtifactID: "a2", ProjectID: "proj-2", Type: "test_case", Title: "Login test"},
		}}
		h := newTestHandler(t, func(h *Handler) {
			h.ArtifactService = svc
			h.ProjectService = &searchProjectService{list: []*projects.Project{
				{ID: "proj-1", OrgID: orgID, Name: "Alpha"},
				{ID: "proj-2", OrgID: orgID, Name: "Beta"},
			}}
			h.OrgService = org
			h.MemberService = &fakeMemberService{roles: map[string]map[string]string{}}
		})
		return h, svc
	}
	admin := map[string]map[string]string{orgID: {"admin": orgs.RoleAdmin}}
	do := func(h *Handler, suffix string) []artifacts.SearchHit {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/search"+suffix, nil)
		ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: "admin"})
		ctx = context.WithValue(ctx, ctxActiveOrg, orgID)
		w := httptest.NewRecorder()
		h.GlobalSearch(w, r.WithContext(ctx))
		if w.Code != http.StatusOK {
			t.Fatalf("status %d", w.Code)
		}
		var resp struct {
			Hits []artifacts.SearchHit `json:"hits"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return resp.Hits
	}

	h, svc := newHandler(&fakeOrgService{roles: admin})
	do(h, "?q=login&project=proj-2,proj-elsewhere&type=test_case&status=approved&status=in_review&owner=Dana&facets=1&mode=semantic")
	if !reflect.DeepEqual(svc.gotProjectIDs, []string{"proj-2"}) {
		t.Errorf("projects = %v, want only the searchable one named", svc.gotProjectIDs)
	}
	if !reflect.DeepEqual(svc.gotOpts.Types, []string{"test_case"}) ||
		!reflect.DeepEqual(svc.gotOpts.Statuses, []string{"approved", "in_review"}) ||
		!reflect.DeepEqual(svc.gotOpts.Owners, []string{"Dana"}) || !svc.gotOpts.Facets {
		t.Errorf("opts = %+v", svc.gotOpts)
	}

	// Facets alone are a search.
	h, svc = newHandler(&fakeOrgService{roles: admin})
	if hits := do(h, "?type=requirement"); svc.calls != 1 || len(hits) == 0 {
		t.Errorf("a search of facets alone: %d calls, %d hits", svc.calls, len(hits))
	}

	// Without the feature, the facets are ignored and an empty query finds nothing.
	old := &fakeOrgService{roles: admin, plan: orgs.PlanTeam, stableRelease: "0.9.2"}
	h, svc = newHandler(old)
	do(h, "?q=login&project=proj-2&type=test_case")
	if len(svc.gotProjectIDs) != 2 || svc.gotOpts.Filtered() {
		t.Errorf("without the feature: projects %v, opts %+v", svc.gotProjectIDs, svc.gotOpts)
	}
	h, svc = newHandler(old)
	do(h, "?type=requirement")
	if svc.calls != 0 {
		t.Error("without the feature an empty query still searched")
	}
}
