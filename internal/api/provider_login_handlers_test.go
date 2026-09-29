package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// fakeLoginBroker serves fixed sign-in requests and records the writes that
// reach it.
type fakeLoginBroker struct {
	providers.LoginService
	byID   map[string]*providers.LoginRequest
	writes []string // "cancel <id>" or "code <id>"
}

func (f *fakeLoginBroker) Get(id string) (*providers.LoginRequest, error) {
	if l, ok := f.byID[id]; ok {
		return l, nil
	}
	return nil, errors.New("login request not found")
}

func (f *fakeLoginBroker) Cancel(id string) (*providers.LoginRequest, error) {
	f.writes = append(f.writes, "cancel "+id)
	return f.byID[id], nil
}

func (f *fakeLoginBroker) SubmitCode(id, code string) (*providers.LoginRequest, error) {
	f.writes = append(f.writes, "code "+id)
	return f.byID[id], nil
}

func providerLoginReq(userID, loginID, path, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/provider-logins/"+loginID+path, strings.NewReader(body))
	ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: userID})
	ctx = context.WithValue(ctx, ctxActiveOrg, "org-w")
	return mux.SetURLVars(r.WithContext(ctx), map[string]string{"id": loginID})
}

// TestProviderLoginWritesTakeTheStartsRole pins that cancelling a sign-in,
// or pasting its code, takes the role that starts it: a workspace admin for
// a sign-in of the workspace's shared workers, which signs them in to an
// account for every run they take; the requester (or a workspace admin) for
// a sign-in on the requester's own runner. A plain member used to be able to
// cancel an admin's workspace sign-in, and to paste a code into it.
func TestProviderLoginWritesTakeTheStartsRole(t *testing.T) {
	member := "member"
	cases := []struct {
		name, user, login, path string
		want                    int
	}{
		{"a plain member cancels a workspace sign-in", "member", "login-ws", "/cancel", http.StatusForbidden},
		{"a plain member pastes a code into a workspace sign-in", "member", "login-ws", "/code", http.StatusForbidden},
		{"the admin cancels a workspace sign-in", "admin", "login-ws", "/cancel", http.StatusOK},
		{"the admin pastes a code into a workspace sign-in", "admin", "login-ws", "/code", http.StatusOK},
		{"the member cancels its own sign-in", "member", "login-user", "/cancel", http.StatusOK},
		{"the member pastes a code into its own sign-in", "member", "login-user", "/code", http.StatusOK},
		{"another member cancels it", "member2", "login-user", "/cancel", http.StatusNotFound},
		{"the admin cancels a member's sign-in", "admin", "login-user", "/cancel", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			broker := &fakeLoginBroker{byID: map[string]*providers.LoginRequest{
				"login-ws": {ID: "login-ws", OrgID: "org-w", Provider: providers.ProviderGeminiCLI,
					Target: providers.LoginTargetWorkspace, Status: providers.LoginPending},
				"login-user": {ID: "login-user", OrgID: "org-w", Provider: providers.ProviderClaudeCode,
					Target: providers.LoginTargetUser, Status: providers.LoginClaimed, RequestedBy: &member},
			}}
			h := NewHandler(HandlerDeps{LoginService: broker, OrgService: &fakeOrgService{roles: map[string]map[string]string{
				"org-w": {"admin": orgs.RoleAdmin, "member": orgs.RoleMember, "member2": orgs.RoleMember},
			}}})
			w := httptest.NewRecorder()
			req := providerLoginReq(tc.user, tc.login, tc.path, `{"code":"tour-code"}`)
			if tc.path == "/cancel" {
				h.CancelProviderLogin(w, req)
			} else {
				h.SubmitProviderLoginCode(w, req)
			}
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.want, w.Body.String())
			}
			if tc.want != http.StatusOK && len(broker.writes) != 0 {
				t.Fatalf("a refused request reached the broker: %v", broker.writes)
			}
			if tc.want == http.StatusOK && len(broker.writes) != 1 {
				t.Fatalf("broker writes = %v, want one", broker.writes)
			}
		})
	}
}
