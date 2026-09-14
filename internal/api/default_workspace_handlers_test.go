package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// fakeDefaultOrgs: a handful of workspaces by id, each with a plan (which
// decides its channel), a type, and a member list.
type fakeDefaultOrgs struct {
	orgs.Service
	plans   map[string]string
	types   map[string]string
	members map[string][]string
}

func (f *fakeDefaultOrgs) Get(id string) (*orgs.Org, error) {
	plan, ok := f.plans[id]
	if !ok {
		return nil, orgs.ErrNotFound
	}
	o := &orgs.Org{ID: id, Plan: plan, OrgType: f.types[id]}
	o.ResolveReleaseChannel()
	return o, nil
}

func (f *fakeDefaultOrgs) IsMember(orgID, userID string) (bool, error) {
	for _, m := range f.members[orgID] {
		if m == userID {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeDefaultOrgs) MemberPreview(string, string) (bool, error) { return false, nil }

func (f *fakeDefaultOrgs) EnsurePersonalOrg(userID, _ string) (*orgs.Org, bool, error) {
	return &orgs.Org{ID: "personal-" + userID, OrgType: orgs.TypePersonal}, false, nil
}

func defaultOrgsFixture() *fakeDefaultOrgs {
	return &fakeDefaultOrgs{
		plans: map[string]string{
			"acme":     orgs.PlanSingle,   // nightly: the feature is on
			"bigco":    orgs.PlanBusiness, // stable with no release yet: gated
			"personal": orgs.PlanFree,
		},
		types:   map[string]string{"personal": orgs.TypePersonal},
		members: map[string][]string{"acme": {"u-1"}, "bigco": {"u-1"}, "personal": {"u-1"}},
	}
}

func defaultWorkspaceReq(method, body string, user *users.User) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, "/api/v1/me/default-workspace", nil)
	} else {
		r = httptest.NewRequest(method, "/api/v1/me/default-workspace", strings.NewReader(body))
	}
	if user != nil {
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, user))
	}
	return r
}

func TestDefaultWorkspaceRequiresUser(t *testing.T) {
	svc := &fakeUserPrefService{}
	h := &Handler{userService: svc, orgService: defaultOrgsFixture()}
	for _, tc := range []struct {
		name string
		do   func(http.ResponseWriter, *http.Request)
		req  *http.Request
	}{
		{"get", h.GetDefaultWorkspace, defaultWorkspaceReq(http.MethodGet, "", nil)},
		{"put", h.SetDefaultWorkspace, defaultWorkspaceReq(http.MethodPut, `{"org_id":"acme"}`, nil)},
	} {
		w := httptest.NewRecorder()
		tc.do(w, tc.req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", tc.name, w.Code)
		}
	}
	if svc.defaultOrg != "" {
		t.Fatal("the service was reached without a user")
	}
}

// A member picks a workspace they belong to; it is stored against the
// SESSION user, never an id from the body.
func TestSetDefaultWorkspaceStoresAMembersChoice(t *testing.T) {
	svc := &fakeUserPrefService{}
	h := &Handler{userService: svc, orgService: defaultOrgsFixture()}
	user := &users.User{ID: "u-1"}

	w := httptest.NewRecorder()
	h.SetDefaultWorkspace(w, defaultWorkspaceReq(http.MethodPut, `{"org_id":"acme"}`, user))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body %q)", w.Code, w.Body.String())
	}
	if svc.defaultOrg != "acme" {
		t.Fatalf("stored %q, want acme", svc.defaultOrg)
	}
	var resp defaultWorkspace
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.OrgID != "acme" {
		t.Fatalf("answer = %q (%v)", w.Body.String(), err)
	}

	// GET reads it back off the user.
	w = httptest.NewRecorder()
	h.GetDefaultWorkspace(w, defaultWorkspaceReq(http.MethodGet, "", &users.User{ID: "u-1", DefaultOrgID: "acme"}))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"acme"`) {
		t.Fatalf("get = %d %q", w.Code, w.Body.String())
	}
}

// A workspace the member does not belong to reads as not found — the same
// answer as one that does not exist, so the endpoint cannot be used to
// learn which workspaces there are.
func TestSetDefaultWorkspaceRefusesAWorkspaceTheMemberIsNotIn(t *testing.T) {
	svc := &fakeUserPrefService{defaultOrg: "keep"}
	h := &Handler{userService: svc, orgService: defaultOrgsFixture()}
	for _, id := range []string{"acme", "nowhere"} {
		w := httptest.NewRecorder()
		h.SetDefaultWorkspace(w, defaultWorkspaceReq(http.MethodPut, `{"org_id":"`+id+`"}`, &users.User{ID: "u-2"}))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", id, w.Code)
		}
	}
	if svc.defaultOrg != "keep" {
		t.Fatal("a refused choice was stored")
	}
}

// "" and the personal workspace both mean "land in my personal workspace",
// and both are stored as the empty choice rather than a personal id that
// could go stale.
func TestSetDefaultWorkspacePersonalMeansNone(t *testing.T) {
	for _, body := range []string{`{"org_id":""}`, `{"org_id":"personal"}`} {
		svc := &fakeUserPrefService{defaultOrg: "acme"}
		h := &Handler{userService: svc, orgService: defaultOrgsFixture()}
		w := httptest.NewRecorder()
		h.SetDefaultWorkspace(w, defaultWorkspaceReq(http.MethodPut, body, &users.User{ID: "u-1"}))
		if w.Code != http.StatusOK || svc.defaultOrg != "" {
			t.Errorf("%s: status = %d, stored %q", body, w.Code, svc.defaultOrg)
		}
	}
}

// A stable-channel workspace whose release predates the feature cannot be
// chosen yet; the refusal names the remedy rather than failing silently.
func TestSetDefaultWorkspaceIsGatedByTheWorkspacesChannel(t *testing.T) {
	svc := &fakeUserPrefService{}
	h := &Handler{userService: svc, orgService: defaultOrgsFixture()}
	w := httptest.NewRecorder()
	h.SetDefaultWorkspace(w, defaultWorkspaceReq(http.MethodPut, `{"org_id":"bigco"}`, &users.User{ID: "u-1"}))
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "next stable release") {
		t.Fatalf("status = %d (body %q)", w.Code, w.Body.String())
	}
	if svc.defaultOrg != "" {
		t.Fatal("a gated choice was stored")
	}
}

// --- the resolver ---

type fakeSessionUsers struct {
	users.Service
	activeOrg string
}

func (f *fakeSessionUsers) SessionByToken(string) (*users.Session, error) {
	return &users.Session{ActiveOrgID: f.activeOrg}, nil
}

// Where a sign-in lands: the session's own switch first, then the member's
// chosen default, then the personal workspace — and a default the member
// has since left is skipped rather than failing every request.
func TestResolveActiveOrgHonoursTheDefaultWorkspace(t *testing.T) {
	orgsFake := defaultOrgsFixture()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)

	resolve := func(sessionOrg, defaultOrg string) string {
		m := &AuthMiddleware{userService: &fakeSessionUsers{activeOrg: sessionOrg}, orgService: orgsFake}
		return m.resolveActiveOrg(r, "tok", &users.User{ID: "u-1", DefaultOrgID: defaultOrg})
	}
	if got := resolve("", ""); got != "personal-u-1" {
		t.Errorf("no choice: %q", got)
	}
	if got := resolve("", "acme"); got != "acme" {
		t.Errorf("fresh sign-in with a default: %q, want acme", got)
	}
	if got := resolve("bigco", "acme"); got != "bigco" {
		t.Errorf("the session's own switch must win: %q", got)
	}
	// Left the workspace since choosing it.
	orgsFake.members["acme"] = nil
	if got := resolve("", "acme"); got != "personal-u-1" {
		t.Errorf("a default the member left: %q, want personal", got)
	}
}
