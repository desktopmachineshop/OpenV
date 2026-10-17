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
		grant: &connectedapps.Grant{ID: "g1", UserID: "u1", OrgID: "org-1", Scope: connectedapps.ScopeRead,
			Workspaces: []connectedapps.Workspace{{ID: "org-1", Name: "Acme"}}}}

	post := httptest.NewRequest(http.MethodPost, "http://openv.internal/api/v1/artifacts", strings.NewReader(`{}`))
	res, err := rt.RoundTrip(post)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusForbidden || seen != nil {
		t.Fatalf("a write on a read-only grant: %d, routed %v", res.StatusCode, seen != nil)
	}

	get := httptest.NewRequest(http.MethodGet, "http://openv.internal/api/v1/artifacts?project_id=p1", nil)
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
	app := context.WithValue(ctx, ctxConnectedApp, &connectedapps.Grant{OrgID: "org-1",
		Workspaces: []connectedapps.Workspace{{ID: "org-1"}, {ID: "org-3"}}})

	for name, c := range map[string]struct {
		ctx  context.Context
		org  string
		want bool
	}{
		"the member, anywhere":               {ctx, "org-2", true},
		"a connected app, in its workspace":  {app, "org-1", true},
		"a connected app, in its second":     {app, "org-3", true},
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

// Listing projects through a grant of two workspaces lists both, each
// project naming its workspace, with each workspace active in turn; a
// workspace whose answer is not a list is answered as it is.
func TestMemberTransportListsProjectsInEveryWorkspace(t *testing.T) {
	failIn := ""
	h := newTestHandler(t, func(h *Handler) {
		h.SetMCPRouter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			org := ActiveOrg(r)
			if org == failIn {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, `{"error":"boom"}`)
				return
			}
			_, _ = io.WriteString(w, `[{"id":"p-`+org+`","name":"Project in `+org+`"}]`)
		}))
	})
	rt := &memberTransport{h: h, outer: httptest.NewRequest(http.MethodPost, "/mcp", nil), user: &users.User{ID: "u1"},
		grant: &connectedapps.Grant{ID: "g1", UserID: "u1", OrgID: "org-1", Scope: connectedapps.ScopeRead,
			Workspaces: []connectedapps.Workspace{{ID: "org-1", Name: "Acme"}, {ID: "org-2", Name: "Beta"}}}}

	list := func() (int, string) {
		res, err := rt.RoundTrip(httptest.NewRequest(http.MethodGet, "http://openv.internal/api/v1/projects", nil))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(body)
	}
	code, body := list()
	want := `[{"id":"p-org-1","name":"Project in org-1","workspace":"Acme"},{"id":"p-org-2","name":"Project in org-2","workspace":"Beta"}]`
	if code != http.StatusOK || body != want {
		t.Fatalf("projects across two workspaces: %d %s\nwant %s", code, body, want)
	}
	failIn = "org-2"
	if code, body := list(); code != http.StatusInternalServerError || !strings.Contains(body, "boom") {
		t.Fatalf("a workspace that fails: %d %s, want its own answer", code, body)
	}
}
