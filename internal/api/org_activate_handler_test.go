package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// fakeActiveOrgUsers records the session writes ActivateOrg makes.
type fakeActiveOrgUsers struct {
	users.Service
	activated []string // "token -> orgID"
}

func (f *fakeActiveOrgUsers) SetActiveOrg(token, orgID string) error {
	f.activated = append(f.activated, token+" -> "+orgID)
	return nil
}

// activateReq is POST /api/v1/orgs/{id}/activate by a signed-in account
// with a session cookie.
func activateReq(user *users.User, orgID string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/orgs/"+orgID+"/activate", nil)
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-token"})
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, user))
	return mux.SetURLVars(r, map[string]string{"id": orgID})
}

// TestActivateOrgUnknownWorkspace: the guard lets a platform admin by for
// any id, so ActivateOrg looks the workspace up before it stores it as the
// session's active one. It used to store an id with no workspace behind it
// and answer 204.
func TestActivateOrgUnknownWorkspace(t *testing.T) {
	sessions := &fakeActiveOrgUsers{}
	h := NewHandler(HandlerDeps{
		OrgService: &fakeOrgService{
			roles:   map[string]map[string]string{"org-1": {"member": orgs.RoleMember}},
			missing: map[string]bool{"phantom": true},
		},
		UserService: sessions,
	})
	admin := &users.User{ID: "root", IsAdmin: true}

	w := httptest.NewRecorder()
	h.ActivateOrg(w, activateReq(admin, "phantom"))
	if w.Code != http.StatusNotFound || w.Body.String() != "{\"error\":\"workspace not found\"}\n" {
		t.Fatalf("status = %d (body %q), want 404 workspace not found", w.Code, w.Body.String())
	}
	if len(sessions.activated) != 0 {
		t.Fatalf("a workspace that does not exist was stored as the session's: %v", sessions.activated)
	}

	// A workspace that exists is stored, for a member and a platform admin.
	for _, user := range []*users.User{{ID: "member"}, admin} {
		w = httptest.NewRecorder()
		h.ActivateOrg(w, activateReq(user, "org-1"))
		if w.Code != http.StatusNoContent {
			t.Fatalf("%s: status = %d, want 204 (body %q)", user.ID, w.Code, w.Body.String())
		}
	}
	if len(sessions.activated) != 2 || sessions.activated[0] != "session-token -> org-1" {
		t.Fatalf("stored %v, want org-1 twice", sessions.activated)
	}
}
