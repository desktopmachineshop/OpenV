package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// The antigravity-cli provider ships behind its release feature (REQ-137,
// #379 bug 82): a stable-channel workspace whose stable release predates it
// is neither offered the provider nor able to choose it, and a nightly
// workspace, or one whose stable release carries it, is both.

// memProviderRepo keeps provider settings in memory, one per provider.
type memProviderRepo struct {
	stored map[string]*providers.ProviderSetting
}

func (m *memProviderRepo) Upsert(p *providers.ProviderSetting) error {
	if m.stored == nil {
		m.stored = map[string]*providers.ProviderSetting{}
	}
	m.stored[p.Provider] = p
	return nil
}

func (m *memProviderRepo) FindByProvider(_, provider string) (*providers.ProviderSetting, error) {
	return m.stored[provider], nil
}

func (m *memProviderRepo) List(string) ([]*providers.ProviderSetting, error) {
	out := []*providers.ProviderSetting{}
	for _, p := range m.stored {
		out = append(out, p)
	}
	return out, nil
}

// fakeAgentRawService adds the raw file save to fakeAgentDefService.
type fakeAgentRawService struct {
	*fakeAgentDefService
}

func (f fakeAgentRawService) SaveRawFile(orgID, slug, content string) (*agents.Agent, error) {
	def, err := agents.ParseFile(content)
	if err != nil {
		return nil, err
	}
	f.saved = append(f.saved, slug)
	return &agents.Agent{ID: "agent-1", OrgID: orgID, Slug: slug, Provider: def.Provider}, nil
}

// providerGateFixture: workspace org-1 on the given plan and turned-on
// stable release, with the provider's feature shipped in 0.3.0; the notes
// know 0.2.0 and 0.3.0 as stable releases. "admin" administers org-1.
func providerGateFixture(t *testing.T, plan, stableRelease string, existing map[string]*agents.Agent) (*Handler, *memProviderRepo, *fakeAgentDefService) {
	t.Helper()
	saved := release.Registry
	release.Registry = []release.Feature{{Key: release.FeatureAntigravity, ShippedIn: "0.3.0"}}
	t.Cleanup(func() { release.Registry = saved })
	notes, err := release.Parse("## 0.3.0 — 2026-10-20\n\nStable channel release since 2026-11-02.\n\n### New features\n\n- x\n\n## 0.2.0 — 2026-09-20\n\nStable channel release since 2026-10-01.\n\n### New features\n\n- y\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	repo := &memProviderRepo{}
	if existing == nil {
		existing = map[string]*agents.Agent{}
	}
	agentSvc := &fakeAgentDefService{bySlug: existing}
	h := newTestHandler(t, func(h *Handler) {
		h.OrgService = &fakeOrgService{
			plan:          plan,
			stableRelease: stableRelease,
			roles:         map[string]map[string]string{"org-1": {"admin": orgs.RoleAdmin}},
			previews:      map[string]bool{},
		}
		h.ReleaseService = staticRelease{notes: notes}
		h.ProviderService = providers.NewDefaultService(repo)
		h.AgentService = fakeAgentRawService{agentSvc}
	})
	return h, repo, agentSvc
}

func gateReq(method, path, body string, vars map[string]string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: "admin"})
	ctx = context.WithValue(ctx, ctxActiveOrg, "org-1")
	r = r.WithContext(ctx)
	if vars != nil {
		r = mux.SetURLVars(r, vars)
	}
	return r
}

// The workspaces the gate is closed for, and those it is open for.
var (
	gateClosed = [][2]string{
		{orgs.PlanBusiness, "0.2.0"}, // stable, on a release before the feature
		{orgs.PlanBusiness, ""},      // stable, before any stable release turned on
	}
	gateOpen = [][2]string{
		{orgs.PlanSingle, ""},        // nightly
		{orgs.PlanBusiness, "0.3.0"}, // stable, on the release that carries it
	}
)

func listedProviders(t *testing.T, h *Handler) []string {
	t.Helper()
	w := httptest.NewRecorder()
	h.ListProviderSettings(w, gateReq(http.MethodGet, "/api/v1/provider-settings", "", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list: status = %d (body %q)", w.Code, w.Body.String())
	}
	var list []providers.ProviderSetting
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	names := make([]string, 0, len(list))
	for _, p := range list {
		names = append(names, p.Provider)
	}
	return names
}

func TestProviderSettingsOfferAntigravityOnlyWhereItsFeatureIsOn(t *testing.T) {
	all := providers.KnownProviders()
	for _, c := range gateClosed {
		h, _, _ := providerGateFixture(t, c[0], c[1], nil)
		got := listedProviders(t, h)
		for _, p := range got {
			if p == providers.ProviderAntigravityCLI {
				t.Errorf("%s on %q: antigravity-cli offered: %v", c[0], c[1], got)
			}
		}
		if len(got) != len(all)-1 {
			t.Errorf("%s on %q: listed %v, want every provider but antigravity-cli", c[0], c[1], got)
		}
	}
	for _, c := range gateOpen {
		h, _, _ := providerGateFixture(t, c[0], c[1], nil)
		if got := listedProviders(t, h); strings.Join(got, ",") != strings.Join(all, ",") {
			t.Errorf("%s on %q: listed %v, want %v", c[0], c[1], got, all)
		}
	}
}

func TestAProviderSettingForAGatedProviderIsRefused(t *testing.T) {
	put := func(h *Handler, provider string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		body := `{"provider":"` + provider + `","auth_mode":"api-key"}`
		h.UpsertProviderSetting(w, gateReq(http.MethodPut, "/api/v1/provider-settings", body, nil))
		return w
	}
	for _, c := range gateClosed {
		h, repo, _ := providerGateFixture(t, c[0], c[1], nil)
		w := put(h, providers.ProviderAntigravityCLI)
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "next stable release") {
			t.Errorf("%s on %q: status = %d (body %q), want 403 with the gate message", c[0], c[1], w.Code, w.Body.String())
		}
		if repo.stored[providers.ProviderAntigravityCLI] != nil {
			t.Errorf("%s on %q: a gated provider setting was stored", c[0], c[1])
		}
		// An ungated provider is configured as before.
		if w := put(h, providers.ProviderGeminiCLI); w.Code != http.StatusOK {
			t.Errorf("%s on %q: gemini-cli: status = %d (body %q)", c[0], c[1], w.Code, w.Body.String())
		}
	}
	for _, c := range gateOpen {
		h, repo, _ := providerGateFixture(t, c[0], c[1], nil)
		if w := put(h, providers.ProviderAntigravityCLI); w.Code != http.StatusOK || repo.stored[providers.ProviderAntigravityCLI] == nil {
			t.Errorf("%s on %q: status = %d (body %q), want it stored", c[0], c[1], w.Code, w.Body.String())
		}
	}
}

func agentBody(provider string) string {
	return `{"slug":"scout","name":"Scout","provider":"` + provider + `","allowed_tools":["mcp__openv__*"]}`
}

func agentFile(provider string) string {
	return "---\nslug: scout\nname: Scout\nprovider: " + provider + "\nallowed_tools:\n  - mcp__openv__*\n---\nYou scout.\n"
}

func TestAnAgentCannotBeMovedOntoAGatedProvider(t *testing.T) {
	vars := map[string]string{"slug": "scout"}
	create := func(h *Handler, provider string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.CreateAgent(w, gateReq(http.MethodPost, "/api/v1/agents", agentBody(provider), nil))
		return w
	}
	update := func(h *Handler, provider string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.UpdateAgent(w, gateReq(http.MethodPut, "/api/v1/agents/scout", agentBody(provider), vars))
		return w
	}
	raw := func(h *Handler, provider string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"content": agentFile(provider)})
		w := httptest.NewRecorder()
		h.SaveAgentRaw(w, gateReq(http.MethodPut, "/api/v1/agents/scout/raw", string(body), vars))
		return w
	}
	refused := func(w *httptest.ResponseRecorder) bool {
		return w.Code == http.StatusForbidden && strings.Contains(w.Body.String(), "next stable release")
	}
	onClaude := func() map[string]*agents.Agent {
		return map[string]*agents.Agent{"scout": {Slug: "scout", Provider: providers.ProviderClaudeCode}}
	}
	onAntigravity := func() map[string]*agents.Agent {
		return map[string]*agents.Agent{"scout": {Slug: "scout", Provider: providers.ProviderAntigravityCLI}}
	}

	for _, c := range gateClosed {
		h, _, svc := providerGateFixture(t, c[0], c[1], nil)
		if w := create(h, providers.ProviderAntigravityCLI); !refused(w) {
			t.Errorf("%s on %q: create: status = %d (body %q), want the gate's 403", c[0], c[1], w.Code, w.Body.String())
		}
		if len(svc.saved) != 0 {
			t.Errorf("%s on %q: create: a gated agent reached the store", c[0], c[1])
		}
		if w := create(h, providers.ProviderClaudeCode); w.Code != http.StatusCreated {
			t.Errorf("%s on %q: create on claude-code: status = %d (body %q)", c[0], c[1], w.Code, w.Body.String())
		}

		h, _, _ = providerGateFixture(t, c[0], c[1], onClaude())
		if w := update(h, providers.ProviderAntigravityCLI); !refused(w) {
			t.Errorf("%s on %q: update onto it: status = %d (body %q), want the gate's 403", c[0], c[1], w.Code, w.Body.String())
		}
		if w := raw(h, providers.ProviderAntigravityCLI); !refused(w) {
			t.Errorf("%s on %q: raw save onto it: status = %d (body %q), want the gate's 403", c[0], c[1], w.Code, w.Body.String())
		}

		// An agent already on the provider stays editable.
		h, _, _ = providerGateFixture(t, c[0], c[1], onAntigravity())
		if w := update(h, providers.ProviderAntigravityCLI); w.Code != http.StatusOK {
			t.Errorf("%s on %q: update of an agent already on it: status = %d (body %q)", c[0], c[1], w.Code, w.Body.String())
		}
		if w := raw(h, providers.ProviderAntigravityCLI); w.Code != http.StatusOK {
			t.Errorf("%s on %q: raw save of an agent already on it: status = %d (body %q)", c[0], c[1], w.Code, w.Body.String())
		}
	}

	for _, c := range gateOpen {
		h, _, _ := providerGateFixture(t, c[0], c[1], nil)
		if w := create(h, providers.ProviderAntigravityCLI); w.Code != http.StatusCreated {
			t.Errorf("%s on %q: create: status = %d (body %q)", c[0], c[1], w.Code, w.Body.String())
		}
		h, _, _ = providerGateFixture(t, c[0], c[1], onClaude())
		if w := update(h, providers.ProviderAntigravityCLI); w.Code != http.StatusOK {
			t.Errorf("%s on %q: update: status = %d (body %q)", c[0], c[1], w.Code, w.Body.String())
		}
		if w := raw(h, providers.ProviderAntigravityCLI); w.Code != http.StatusOK {
			t.Errorf("%s on %q: raw save: status = %d (body %q)", c[0], c[1], w.Code, w.Body.String())
		}
	}
}

// The registry's own entry: the key the gate reads is the registered one.
func TestTheAntigravityGateReadsItsRegisteredFeature(t *testing.T) {
	if key := gatedProviders[providers.ProviderAntigravityCLI]; key != release.FeatureAntigravity {
		t.Fatalf("antigravity-cli is gated on %q, want %q", key, release.FeatureAntigravity)
	}
	for _, f := range release.Registry {
		if f.Key == release.FeatureAntigravity {
			return
		}
	}
	t.Fatalf("%q is not in release.Registry", release.FeatureAntigravity)
}
