package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

// TestRegisterRoutesResolvesTheNewPaths is a smoke test over the router: the
// invitation, policy and password routes must resolve to their handlers, and
// registering them must not panic on a conflicting pattern.
func TestRegisterRoutesResolvesTheNewPaths(t *testing.T) {
	h := &Handler{}
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/auth/policy"},
		{http.MethodPost, "/api/v1/auth/invitations/preview"},
		{http.MethodPost, "/api/v1/auth/invitations/accept"},
		{http.MethodGet, "/api/v1/orgs/org-1/invitations"},
		{http.MethodPost, "/api/v1/orgs/org-1/invitations"},
		{http.MethodDelete, "/api/v1/orgs/org-1/invitations/inv-1"},
		{http.MethodPut, "/api/v1/me/password"},
	} {
		var match mux.RouteMatch
		if !router.Match(httptest.NewRequest(tc.method, tc.path, nil), &match) {
			t.Errorf("%s %s does not resolve to a handler", tc.method, tc.path)
		}
	}
}
