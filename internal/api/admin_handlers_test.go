package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// Platform administration (REQ-155): listings and admin promotion behind
// the platform-admin gate.

type adminOrgFake struct {
	fakeOrgService
	ids []string
}

func (f *adminOrgFake) ListAll() ([]string, error) { return f.ids, nil }
func (f *adminOrgFake) ListMembers(orgID string) ([]*orgs.Member, error) {
	return []*orgs.Member{{OrgID: orgID, UserID: "a"}, {OrgID: orgID, UserID: "b"}}, nil
}

type adminUserFake struct {
	users.Service
	byID map[string]*users.User
}

func (f *adminUserFake) ListUsers() ([]*users.User, error) {
	var out []*users.User
	for _, u := range f.byID {
		out = append(out, u)
	}
	return out, nil
}

func (f *adminUserFake) SetAdmin(id string, isAdmin bool) (*users.User, error) {
	u, ok := f.byID[id]
	if !ok {
		return nil, users.ErrUserNotFound
	}
	if !isAdmin && u.IsAdmin {
		admins := 0
		for _, other := range f.byID {
			if other.IsAdmin {
				admins++
			}
		}
		if admins <= 1 {
			return nil, users.ErrLastAdmin
		}
	}
	u.IsAdmin = isAdmin
	return u, nil
}

func adminReq(method, path, body string, user *users.User, vars map[string]string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if user != nil {
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, user))
	}
	if vars != nil {
		r = mux.SetURLVars(r, vars)
	}
	return r
}

func newAdminHandler() (*Handler, *adminUserFake) {
	uf := &adminUserFake{byID: map[string]*users.User{
		"root":  {ID: "root", Name: "Root", Email: "root@example.com", IsAdmin: true},
		"dave":  {ID: "dave", Name: "Dave", Email: "dave@example.com"},
		"other": {ID: "other", Name: "Other", Email: "other@example.com", IsAdmin: true},
	}}
	h := &Handler{
		orgService:  &adminOrgFake{fakeOrgService: fakeOrgService{plan: orgs.PlanBusiness}, ids: []string{"org-1", "org-2"}},
		userService: uf,
	}
	return h, uf
}

func TestAdminListingsAreGated(t *testing.T) {
	h, _ := newAdminHandler()
	for name, user := range map[string]*users.User{"visitor": nil, "member": {ID: "dave"}} {
		w := httptest.NewRecorder()
		h.AdminListWorkspaces(w, adminReq(http.MethodGet, "/api/v1/admin/workspaces", "", user, nil))
		want := http.StatusForbidden
		if user == nil {
			want = http.StatusUnauthorized
		}
		if w.Code != want {
			t.Errorf("%s workspaces: status = %d, want %d", name, w.Code, want)
		}
		w = httptest.NewRecorder()
		h.AdminListUsers(w, adminReq(http.MethodGet, "/api/v1/admin/users", "", user, nil))
		if w.Code != want {
			t.Errorf("%s users: status = %d, want %d", name, w.Code, want)
		}
	}
}

func TestAdminListWorkspacesAndUsers(t *testing.T) {
	h, _ := newAdminHandler()
	root := &users.User{ID: "root", IsAdmin: true}

	w := httptest.NewRecorder()
	h.AdminListWorkspaces(w, adminReq(http.MethodGet, "/api/v1/admin/workspaces", "", root, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("workspaces: status = %d", w.Code)
	}
	var wsList []adminWorkspace
	if err := json.Unmarshal(w.Body.Bytes(), &wsList); err != nil {
		t.Fatal(err)
	}
	if len(wsList) != 2 || wsList[0].Members != 2 || wsList[0].Plan != orgs.PlanBusiness {
		t.Errorf("workspaces = %+v", wsList)
	}

	w = httptest.NewRecorder()
	h.AdminListUsers(w, adminReq(http.MethodGet, "/api/v1/admin/users", "", root, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("users: status = %d", w.Code)
	}
	var people []adminUser
	if err := json.Unmarshal(w.Body.Bytes(), &people); err != nil {
		t.Fatal(err)
	}
	if len(people) != 3 || !people[0].IsAdmin || !people[1].IsAdmin || people[2].ID != "dave" {
		t.Errorf("users = %+v", people)
	}
	if strings.Contains(w.Body.String(), "password_hash") {
		t.Error("listing leaks password hashes")
	}
}

func TestAdminSetUserAdmin(t *testing.T) {
	h, uf := newAdminHandler()
	root := &users.User{ID: "root", IsAdmin: true}
	vars := func(id string) map[string]string { return map[string]string{"id": id} }

	w := httptest.NewRecorder()
	h.AdminSetUserAdmin(w, adminReq(http.MethodPut, "/x", `{"is_admin":true}`, root, vars("dave")))
	if w.Code != http.StatusOK || !uf.byID["dave"].IsAdmin {
		t.Fatalf("grant: status = %d, admin = %v", w.Code, uf.byID["dave"].IsAdmin)
	}

	w = httptest.NewRecorder()
	h.AdminSetUserAdmin(w, adminReq(http.MethodPut, "/x", `{"is_admin":false}`, root, vars("root")))
	if w.Code != http.StatusBadRequest || !uf.byID["root"].IsAdmin {
		t.Errorf("self-demotion: status = %d, admin = %v", w.Code, uf.byID["root"].IsAdmin)
	}

	w = httptest.NewRecorder()
	h.AdminSetUserAdmin(w, adminReq(http.MethodPut, "/x", `{"is_admin":false}`, root, vars("dave")))
	if w.Code != http.StatusOK || uf.byID["dave"].IsAdmin {
		t.Errorf("revoke: status = %d, admin = %v", w.Code, uf.byID["dave"].IsAdmin)
	}
	uf.byID["other"].IsAdmin = false
	w = httptest.NewRecorder()
	h.AdminSetUserAdmin(w, adminReq(http.MethodPut, "/x", `{"is_admin":false}`, &users.User{ID: "other", IsAdmin: true}, vars("root")))
	if w.Code != http.StatusBadRequest || !uf.byID["root"].IsAdmin {
		t.Errorf("last admin: status = %d, admin = %v", w.Code, uf.byID["root"].IsAdmin)
	}

	w = httptest.NewRecorder()
	h.AdminSetUserAdmin(w, adminReq(http.MethodPut, "/x", `{"is_admin":true}`, root, vars("nobody")))
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown user: status = %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.AdminSetUserAdmin(w, adminReq(http.MethodPut, "/x", `{"is_admin":true}`, &users.User{ID: "dave"}, vars("dave")))
	if w.Code != http.StatusForbidden {
		t.Errorf("non-admin: status = %d", w.Code)
	}
}
