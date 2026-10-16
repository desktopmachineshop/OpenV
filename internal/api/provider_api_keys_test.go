package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// A workspace admin pastes a provider API key in the app (REQ-197): the API
// stores it sealed, never answers its value, and hands it to a runner only
// with the claim of a run of a project on api-key auth.

// memAPIKeyRepo keeps pasted keys in memory.
type memAPIKeyRepo struct {
	rows map[string]*providers.StoredKey
}

func (m *memAPIKeyRepo) UpsertKey(k *providers.StoredKey) error {
	cp := *k
	m.rows[k.OrgID+"/"+k.KeyEnv] = &cp
	return nil
}

func (m *memAPIKeyRepo) FindKey(orgID, keyEnv string) (*providers.StoredKey, error) {
	return m.rows[orgID+"/"+keyEnv], nil
}

func (m *memAPIKeyRepo) ListKeys(orgID string) ([]*providers.StoredKey, error) {
	out := []*providers.StoredKey{}
	for _, k := range m.rows {
		if k.OrgID == orgID {
			out = append(out, k)
		}
	}
	return out, nil
}

func (m *memAPIKeyRepo) DeleteKey(orgID, keyEnv string) (bool, error) {
	_, ok := m.rows[orgID+"/"+keyEnv]
	delete(m.rows, orgID+"/"+keyEnv)
	return ok, nil
}

const pastedKey = "fake-test-key-pasted-WXYZ"

// apiKeyFixture: workspace org-1, where "admin" is an admin and "member" a
// member, on the given plan and stable release, with workspace-api-keys
// shipped in 0.3.0. withSealer false leaves the server without
// OPENV_SECRETS_KEY.
func apiKeyFixture(t *testing.T, plan, stableRelease string, withSealer bool) (*Handler, *memAPIKeyRepo) {
	t.Helper()
	saved := release.Registry
	release.Registry = []release.Feature{{Key: release.FeatureWorkspaceAPIKeys, ShippedIn: "0.3.0"}}
	t.Cleanup(func() { release.Registry = saved })
	notes, err := release.Parse("## 0.3.0 — 2026-10-20\n\nStable channel release since 2026-11-02.\n\n### New features\n\n- x\n\n## 0.2.0 — 2026-09-20\n\nStable channel release since 2026-10-01.\n\n### New features\n\n- y\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var sealer *providers.Sealer
	if withSealer {
		if sealer, err = providers.NewSealer(strings.Repeat("s", providers.MinSecretsKeyLength)); err != nil {
			t.Fatal(err)
		}
	}
	repo := &memAPIKeyRepo{rows: map[string]*providers.StoredKey{}}
	h := newTestHandler(t, func(h *Handler) {
		h.OrgService = &fakeOrgService{
			plan:          plan,
			stableRelease: stableRelease,
			roles:         map[string]map[string]string{"org-1": {"admin": orgs.RoleAdmin, "member": orgs.RoleMember}},
			previews:      map[string]bool{},
		}
		h.ReleaseService = staticRelease{notes: notes}
		h.ProviderKeyService = providers.NewKeyService(repo, sealer)
	})
	return h, repo
}

func keyReq(user, method, body, env string) *http.Request {
	r := httptest.NewRequest(method, "/api/v1/provider-api-keys/"+env, strings.NewReader(body))
	ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: user})
	ctx = context.WithValue(ctx, ctxActiveOrg, "org-1")
	return mux.SetURLVars(r.WithContext(ctx), map[string]string{"env": env})
}

func putKey(h *Handler, user, env, key string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.SetProviderAPIKey(w, keyReq(user, http.MethodPut, `{"key":"`+key+`"}`, env))
	return w
}

func TestAnAdminPastesAKeyAndOnlyItsLastFourComeBack(t *testing.T) {
	h, repo := apiKeyFixture(t, orgs.PlanSingle, "", true)
	w := putKey(h, "admin", "ANTHROPIC_API_KEY", pastedKey)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: status = %d (body %q)", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "fake-test-key") || !strings.Contains(w.Body.String(), `"last4":"WXYZ"`) {
		t.Errorf("PUT answered %s; want the last four characters and nothing more of the key", w.Body.String())
	}
	if repo.rows["org-1/ANTHROPIC_API_KEY"] == nil {
		t.Fatal("the key was not stored")
	}

	list := httptest.NewRecorder()
	h.ListProviderAPIKeys(list, keyReq("admin", http.MethodGet, "", ""))
	var got providerAPIKeysResponse
	if err := json.Unmarshal(list.Body.Bytes(), &got); err != nil || list.Code != http.StatusOK {
		t.Fatalf("GET: status = %d, body %q, %v", list.Code, list.Body.String(), err)
	}
	if !got.Enabled || len(got.Keys) != 1 || got.Keys[0].KeyEnv != "ANTHROPIC_API_KEY" || got.Keys[0].UpdatedBy != "admin" {
		t.Errorf("GET = %+v, want the one key, enabled", got)
	}
	if strings.Contains(list.Body.String(), "fake-test-key") || strings.Contains(list.Body.String(), "sealed") {
		t.Errorf("GET leaks the key: %s", list.Body.String())
	}

	del := httptest.NewRecorder()
	h.DeleteProviderAPIKey(del, keyReq("admin", http.MethodDelete, "", "ANTHROPIC_API_KEY"))
	if del.Code != http.StatusNoContent || len(repo.rows) != 0 {
		t.Errorf("DELETE: status = %d, rows left %d", del.Code, len(repo.rows))
	}
	again := httptest.NewRecorder()
	h.DeleteProviderAPIKey(again, keyReq("admin", http.MethodDelete, "", "ANTHROPIC_API_KEY"))
	if again.Code != http.StatusNotFound {
		t.Errorf("DELETE of no key: status = %d, want 404", again.Code)
	}
}

func TestOnlyAWorkspaceAdminManagesPastedKeys(t *testing.T) {
	h, repo := apiKeyFixture(t, orgs.PlanSingle, "", true)
	if w := putKey(h, "member", "ANTHROPIC_API_KEY", pastedKey); w.Code != http.StatusForbidden {
		t.Errorf("a member's PUT: status = %d, want 403", w.Code)
	}
	list := httptest.NewRecorder()
	h.ListProviderAPIKeys(list, keyReq("member", http.MethodGet, "", ""))
	if list.Code != http.StatusForbidden {
		t.Errorf("a member's GET: status = %d, want 403", list.Code)
	}
	if len(repo.rows) != 0 {
		t.Error("a member stored a key")
	}
}

func TestAPastedKeyIsRefusedWhenItCannotBeStored(t *testing.T) {
	h, _ := apiKeyFixture(t, orgs.PlanSingle, "", true)
	for _, tc := range []struct{ env, key string }{
		{"DATABASE_URL", pastedKey},
		{"ANTHROPIC_API_KEY", "   "},
		{"ANTHROPIC_API_KEY", "two words"},
	} {
		if w := putKey(h, "admin", tc.env, tc.key); w.Code != http.StatusBadRequest {
			t.Errorf("PUT %s %q: status = %d (body %q), want 400", tc.env, tc.key, w.Code, w.Body.String())
		}
	}
	off, _ := apiKeyFixture(t, orgs.PlanSingle, "", false)
	w := putKey(off, "admin", "ANTHROPIC_API_KEY", pastedKey)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "OPENV_SECRETS_KEY") {
		t.Errorf("PUT with no secrets key: status = %d (body %q), want 503 naming OPENV_SECRETS_KEY", w.Code, w.Body.String())
	}
}

func TestPastingAKeyWaitsForTheWorkspacesStableRelease(t *testing.T) {
	for _, c := range gateClosed {
		h, repo := apiKeyFixture(t, c[0], c[1], true)
		w := putKey(h, "admin", "ANTHROPIC_API_KEY", pastedKey)
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "next stable release") || len(repo.rows) != 0 {
			t.Errorf("%s on %q: status = %d (body %q), want 403 with the gate message and nothing stored", c[0], c[1], w.Code, w.Body.String())
		}
	}
	for _, c := range gateOpen {
		h, _ := apiKeyFixture(t, c[0], c[1], true)
		if w := putKey(h, "admin", "ANTHROPIC_API_KEY", pastedKey); w.Code != http.StatusOK {
			t.Errorf("%s on %q: status = %d (body %q), want it stored", c[0], c[1], w.Code, w.Body.String())
		}
	}
}

func TestAClaimCarriesTheWorkspacesPastedKeyOnlyForAnAPIKeyProject(t *testing.T) {
	h, _ := apiKeyFixture(t, orgs.PlanSingle, "", true)
	if w := putKey(h, "admin", "ANTHROPIC_API_KEY", pastedKey); w.Code != http.StatusOK {
		t.Fatalf("PUT: status = %d", w.Code)
	}
	apiKeyProject, accountProject := "p-key", "p-account"
	h.ProjectService = &fakeProjectService{byID: map[string]*projects.Project{
		apiKeyProject:  {ID: apiKeyProject, OrgID: "org-1", AgentAuth: projects.AgentAuthAPIKey},
		accountProject: {ID: accountProject, OrgID: "org-1", AgentAuth: projects.AgentAuthUserAccount},
	}}
	agent := &agents.Agent{Provider: providers.ProviderClaudeCode}

	auth := h.resolveRunAuth(httptest.NewRequest("POST", "/api/v1/agent-runs/claim", nil), &agentruns.Run{OrgID: "org-1", ProjectID: &apiKeyProject}, agent)
	if auth["mode"] != projects.AgentAuthAPIKey || auth["api_key_env"] != "ANTHROPIC_API_KEY" || auth["api_key"] != pastedKey {
		t.Errorf("api-key project: auth = %v, want the pasted key for ANTHROPIC_API_KEY", auth)
	}
	if auth := h.resolveRunAuth(httptest.NewRequest("POST", "/api/v1/agent-runs/claim", nil), &agentruns.Run{OrgID: "org-1", ProjectID: &accountProject}, agent); auth["api_key"] != "" {
		t.Errorf("user-account project: auth = %v, want no key", auth)
	}
	if auth := h.resolveRunAuth(httptest.NewRequest("POST", "/api/v1/agent-runs/claim", nil), &agentruns.Run{OrgID: "org-1"}, agent); auth["api_key"] != "" {
		t.Errorf("run with no project: auth = %v, want no key", auth)
	}
	// Another workspace's run of an api-key project gets nothing of org-1's.
	h.ProjectService.(*fakeProjectService).byID[apiKeyProject].OrgID = "org-2"
	if auth := h.resolveRunAuth(httptest.NewRequest("POST", "/api/v1/agent-runs/claim", nil), &agentruns.Run{OrgID: "org-2", ProjectID: &apiKeyProject}, agent); auth["api_key"] != "" {
		t.Errorf("org-2's run: auth = %v, want no key", auth)
	}
}
