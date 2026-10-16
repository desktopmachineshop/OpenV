package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/connectedapps"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// REQ-44: a read-only grant's tool requests are refused anything but a
// read, before they reach the router, whatever tool sent them; a read goes
// through as the member, in the grant's workspace, marked as a connected
// app's, with no credential of its own.
func TestMemberTransportHoldsAReadOnlyGrantToReads(t *testing.T) {
	var seen *http.Request
	h := newTestHandler(t, func(h *Handler) {
		h.SetMCPRouter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = r
			w.WriteHeader(http.StatusTeapot)
			_, _ = io.WriteString(w, "routed")
		}))
	})
	outer := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	member := &users.User{ID: "u1"}
	rt := &memberTransport{h: h, outer: outer, user: member,
		grant: &connectedapps.Grant{ID: "g1", UserID: "u1", OrgID: "org-1", Scope: connectedapps.ScopeRead}}

	post := httptest.NewRequest(http.MethodPost, "http://openv.internal/api/v1/artifacts", strings.NewReader(`{}`))
	res, err := rt.RoundTrip(post)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusForbidden || seen != nil {
		t.Fatalf("a write on a read-only grant: %d, routed %v", res.StatusCode, seen != nil)
	}

	get := httptest.NewRequest(http.MethodGet, "http://openv.internal/api/v1/projects", nil)
	get.Header.Set("Authorization", "Bearer something")
	res, err = rt.RoundTrip(get)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusTeapot || string(body) != "routed" {
		t.Fatalf("a read: %d %q", res.StatusCode, body)
	}
	if CurrentUser(seen) != member || ActiveOrg(seen) != "org-1" || ConnectedApp(seen) == nil {
		t.Fatalf("the routed request: user %v, org %q, app %v", CurrentUser(seen), ActiveOrg(seen), ConnectedApp(seen))
	}
	if seen.Header.Get("Authorization") != "" {
		t.Fatal("the routed request carried a credential")
	}

	rt.grant.Scope = connectedapps.ScopeRead + " " + connectedapps.ScopeWrite
	seen = nil
	if res, _ := rt.RoundTrip(httptest.NewRequest(http.MethodPost, "http://openv.internal/api/v1/artifacts", strings.NewReader(`{}`))); res.StatusCode != http.StatusTeapot || seen == nil {
		t.Fatalf("a write on a read-and-write grant was not routed: %d", res.StatusCode)
	}
}

// A project or workspace guard holds a connected app to its grant's
// workspace: another answers as one no row has, and the member's own
// session is not held to anything.
func TestScopedUserHoldsAConnectedAppToItsWorkspace(t *testing.T) {
	h := newTestHandler(t)
	member := &users.User{ID: "u1"}
	ctx := context.WithValue(context.Background(), ctxUser, member)
	app := context.WithValue(ctx, ctxConnectedApp, &connectedapps.Grant{OrgID: "org-1"})

	for name, c := range map[string]struct {
		ctx  context.Context
		org  string
		want bool
	}{
		"the member, anywhere":               {ctx, "org-2", true},
		"a connected app, in its workspace":  {app, "org-1", true},
		"a connected app, in another":        {app, "org-2", false},
		"a connected app, a project of none": {app, "", false},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(c.ctx)
		got := h.scopedUser(w, r, func() string { return c.org }, unknownProject)
		if (got != nil) != c.want {
			t.Errorf("%s: user %v, want %v", name, got, c.want)
		}
		if !c.want && w.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want the 404 of one no row has", name, w.Code)
		}
	}
}
