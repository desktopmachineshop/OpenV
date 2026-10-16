package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/ext"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// TestExtErrorCodesAreTheCatalogues: a module answers with the codes of
// this package's catalogue (httperr.go), which the contract publishes and a
// client branches on. ext spells them as its own constants, since it
// cannot import this package; this holds the two in step.
func TestExtErrorCodesAreTheCatalogues(t *testing.T) {
	for _, c := range []struct{ ext, api string }{
		{ext.ErrCodeBillingUnavailable, ErrCodeBillingUnavailable},
		{ext.ErrCodeBillingUpstream, ErrCodeBillingUpstream},
		{ext.ErrCodeUnknownPlan, ErrCodeUnknownPlan},
		{ext.ErrCodeAlreadySubscribed, ErrCodeAlreadySubscribed},
		{ext.ErrCodeNoSubscription, ErrCodeNoSubscription},
		{ext.ErrCodeGrantedPlan, ErrCodeGrantedPlan},
		{ext.ErrCodeNoCustomer, ErrCodeNoCustomer},
		{ext.ErrCodeCheckoutMismatch, ErrCodeCheckoutMismatch},
	} {
		if c.ext != c.api {
			t.Errorf("ext spells %q, the catalogue %q", c.ext, c.api)
		}
	}
}

// moduleProbe is a module with one guarded write route and one open read,
// recording what reached it and what it was told.
type moduleProbe struct {
	reached []string
	seats   []string
}

func (p *moduleProbe) Name() string { return "probe" }
func (p *moduleProbe) Routes(r ext.Router) {
	r.Handle("POST", "/api/v1/orgs/{id}/probe", func(w http.ResponseWriter, r *http.Request) {
		p.reached = append(p.reached, "write")
		w.WriteHeader(http.StatusNoContent)
	}, ext.RequireWorkspaceRole("id", ext.RoleAdmin))
	r.Handle("GET", "/api/v1/public/probe", func(w http.ResponseWriter, r *http.Request) {
		p.reached = append(p.reached, "read")
		w.WriteHeader(http.StatusNoContent)
	})
}
func (p *moduleProbe) SeatsChanged(orgID string)                         { p.seats = append(p.seats, orgID) }
func (p *moduleProbe) WorkspaceDeleted(context.Context, *ext.Workspace)  {}
func (p *moduleProbe) WorkspaceRestored(context.Context, *ext.Workspace) {}

// TestADeclaredGuardRunsBeforeTheModule: a module route's declared role
// guard answers exactly as the core's guard does — 401 without a session,
// 403 below the role — and the module's handler never runs then; an
// undeclared route is open. Observers hear seat changes.
func TestADeclaredGuardRunsBeforeTheModule(t *testing.T) {
	probe := &moduleProbe{}
	h := NewHandler(HandlerDeps{Modules: []ext.Module{probe}})
	h.OrgService = &fakeOrgService{roles: map[string]map[string]string{"org-1": {"admin": orgs.RoleAdmin, "member": orgs.RoleMember}}}
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	serve := func(method, path string, user *users.User) int {
		r := httptest.NewRequest(method, path, nil)
		if user != nil {
			r = r.WithContext(context.WithValue(r.Context(), ctxUser, user))
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w.Code
	}
	if got := serve("POST", "/api/v1/orgs/org-1/probe", nil); got != http.StatusUnauthorized {
		t.Errorf("no session: %d, want 401", got)
	}
	if got := serve("POST", "/api/v1/orgs/org-1/probe", &users.User{ID: "member"}); got != http.StatusForbidden {
		t.Errorf("member: %d, want 403", got)
	}
	if len(probe.reached) != 0 {
		t.Fatalf("the module ran behind a refused guard: %v", probe.reached)
	}
	if got := serve("POST", "/api/v1/orgs/org-1/probe", &users.User{ID: "admin"}); got != http.StatusNoContent {
		t.Errorf("admin: %d, want 204", got)
	}
	if got := serve("GET", "/api/v1/public/probe", nil); got != http.StatusNoContent {
		t.Errorf("open read: %d, want 204", got)
	}
	if strings.Join(probe.reached, ",") != "write,read" {
		t.Errorf("reached %v", probe.reached)
	}
	h.seatsChanged("org-1")
	if strings.Join(probe.seats, ",") != "org-1" {
		t.Errorf("seats heard %v", probe.seats)
	}
}
