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
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/runnersessions"
	"github.com/openv/requirements-platform/internal/domain/sharelinks"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/workerkeys"
)

// The writes a workspace over its plan still takes beyond the ones that
// bring it back under plan (OpenV REQ-177, with REQ-176's seat cap making it
// read-only; issue #379's questions 6 to 8 as the maintainer decided them):
// three revocations (a worker key, one's own runner key, a share link),
// which only take access away; three writes that touch only the caller's
// own session or lease; and a run's cancel. The exemption is the
// alwaysWritable wrapper a route is registered with, so each request goes
// through the router RegisterRoutes builds, not to the handler directly,
// and beside each group a write of the same guard that is not exempt shows
// the workspace read-only.

// lapsedOrgs is workspace org-1 after its Business subscription lapsed: it
// is entitled to the free tier's two seats while three people hold one
// (admin, m1 and m2), so it is read-only, and its billed plan still lets a
// member preview the next stable release.
type lapsedOrgs struct{ *fakeOrgService }

func (s lapsedOrgs) Get(id string) (*orgs.Org, error) {
	o, err := s.fakeOrgService.Get(id)
	if err == nil {
		o.OrgType, o.Billing.Status = orgs.TypeCompany, orgs.PlanStatusCanceled
	}
	return o, err
}

// revocableKeys holds m1's personal runner key and records each revocation
// as "orgID/keyID".
type revocableKeys struct {
	workerkeys.Service
	revoked []string
}

func (f *revocableKeys) PersonalKey(orgID, userID string) (*workerkeys.Key, error) {
	if userID != "m1" {
		return nil, nil
	}
	return &workerkeys.Key{ID: "rk-m1", OrgID: orgID, UserID: &userID}, nil
}

func (f *revocableKeys) Revoke(orgID, keyID string) error {
	f.revoked = append(f.revoked, orgID+"/"+keyID)
	return nil
}

// revocableLinks answers every id with a public link to p1 and records each
// revocation.
type revocableLinks struct {
	sharelinks.Service
	revoked []string
}

func (f *revocableLinks) Get(id string) (*sharelinks.Link, error) {
	return &sharelinks.Link{ID: id, ProjectID: "p1", Role: sharelinks.RolePublic}, nil
}

func (f *revocableLinks) Revoke(id string) error {
	f.revoked = append(f.revoked, id)
	return nil
}

// endableLease is m1's cloud runner lease, recording each end as
// "sessionID reason".
type endableLease struct {
	fakeRunnerSessions
	ended []string
}

func (f *endableLease) End(sessionID, reason string) (*runnersessions.Session, error) {
	f.ended = append(f.ended, sessionID+" "+reason)
	return f.session, nil
}

// deletableArtifacts holds artifact a-1 of p1 and records each delete, so a
// delete the gate fails to refuse answers 204 rather than panicking.
type deletableArtifacts struct {
	fakeArtifactService
	deleted []string
}

func (f *deletableArtifacts) DeleteArtifact(id string) error {
	f.deleted = append(f.deleted, id)
	return nil
}

// cancellableRuns records each cancel and answers the run cancelled.
type cancellableRuns struct {
	fakeRunService
	cancelled []string
}

func (f *cancellableRuns) RequestCancel(id string) (*agentruns.Run, error) {
	f.cancelled = append(f.cancelled, id)
	run := f.byID[id]
	run.Status = agentruns.StatusCancelled
	return run, nil
}

// readOnlyWorkspace is org-1 read-only over its seats, with project p1 (m1
// its editor, m2 holding no role in it) and its artifact a-1, m1's personal
// runner key and cloud runner lease, and two runs m2 launched: run-1 in p1
// and run-2 in no project.
type readOnlyWorkspace struct {
	router    *mux.Router
	orgs      *fakeOrgService
	keys      *revocableKeys
	links     *revocableLinks
	artifacts *deletableArtifacts
	sessions  *fakeActiveOrgUsers
	lease     *endableLease
	runs      *cancellableRuns
}

func newReadOnlyWorkspace(t *testing.T) *readOnlyWorkspace {
	t.Helper()
	enforceTiers(t)
	m2, p1 := "m2", "p1"
	x := &readOnlyWorkspace{
		orgs: &fakeOrgService{plan: orgs.PlanBusiness, roles: map[string]map[string]string{
			"org-1": {"admin": orgs.RoleAdmin, "m1": orgs.RoleMember, "m2": orgs.RoleMember},
		}},
		keys:  &revocableKeys{},
		links: &revocableLinks{},
		artifacts: &deletableArtifacts{fakeArtifactService: fakeArtifactService{byID: map[string]*artifacts.Artifact{
			"a-1": {ID: "a-1", ProjectID: "p1"},
		}}},
		sessions: &fakeActiveOrgUsers{},
		lease:    &endableLease{fakeRunnerSessions: fakeRunnerSessions{session: &runnersessions.Session{ID: "lease-m1", OrgID: "org-1", UserID: "m1"}}},
		runs: &cancellableRuns{fakeRunService: fakeRunService{byID: map[string]*agentruns.Run{
			"run-1": {ID: "run-1", OrgID: "org-1", ProjectID: &p1, LaunchedBy: &m2, Status: agentruns.StatusQueued},
			"run-2": {ID: "run-2", OrgID: "org-1", LaunchedBy: &m2, Status: agentruns.StatusQueued},
		}}},
	}
	h := NewHandler(HandlerDeps{
		OrgService:           lapsedOrgs{x.orgs},
		ProjectService:       &fakeProjectService{byID: map[string]*projects.Project{"p1": {ID: "p1", OrgID: "org-1"}}},
		MemberService:        &fakeMemberService{roles: map[string]map[string]string{"p1": {"m1": members.RoleEditor}}},
		WorkerKeyService:     x.keys,
		ShareLinkService:     x.links,
		ArtifactService:      x.artifacts,
		UserService:          x.sessions,
		RunnerSessionService: x.lease,
		RunService:           x.runs,
	})
	x.router = mux.NewRouter()
	h.RegisterRoutes(x.router)
	if over := h.overPlan(h.orgForLimits("org-1")); len(over) != 1 || over[0] != orgs.LimitMaxMembers {
		t.Fatalf("the fixture's workspace is over %v, not max_members alone", over)
	}
	return x
}

// send is one request through the router, as userID with a session cookie
// (the auth middleware's part).
func (x *readOnlyWorkspace) send(userID, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-" + userID})
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: userID}))
	w := httptest.NewRecorder()
	x.router.ServeHTTP(w, r)
	return w
}

// planReadOnly reports whether an answer is the read-only gate's refusal.
func planReadOnly(w *httptest.ResponseRecorder) bool {
	var body errorBody
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return w.Code == http.StatusForbidden && body.Code == ErrCodePlanReadOnly
}

// Revoking a worker key, one's own runner key or a share link is never
// refused for being over plan (#379 question 6, which names those routes):
// an admin revokes a workspace worker key and a public share link, a member
// its own runner key. Minting a key or a link there is still refused, and so
// is a delete that revokes nothing, on the workspace's guard (its logo) and
// on a project's (an artifact of p1): the gate asks a DELETE as it asks a
// POST or a PUT, which no step of the S5 tour pins any more, since every
// DELETE its over-plan passes refused is one of the routes #379 exempted.
func TestAReadOnlyWorkspaceStillRevokesAccess(t *testing.T) {
	x := newReadOnlyWorkspace(t)
	for _, c := range []struct{ who, method, path string }{
		{"admin", http.MethodDelete, "/api/v1/orgs/org-1/worker-keys/k-1"},
		{"m1", http.MethodDelete, "/api/v1/orgs/org-1/my-runner-key"},
		{"admin", http.MethodDelete, "/api/v1/share-links/sl-1"},
	} {
		if w := x.send(c.who, c.method, c.path, ""); w.Code != http.StatusNoContent {
			t.Errorf("%s %s as %s on a read-only workspace: %d %s, want 204", c.method, c.path, c.who, w.Code, w.Body.String())
		}
	}
	if got := strings.Join(x.keys.revoked, ", "); got != "org-1/k-1, org-1/rk-m1" {
		t.Errorf("worker keys revoked: %q, want org-1/k-1, org-1/rk-m1", got)
	}
	if got := strings.Join(x.links.revoked, ", "); got != "sl-1" {
		t.Errorf("share links revoked: %q, want sl-1", got)
	}

	for _, c := range []struct{ who, path, body string }{
		{"admin", "/api/v1/orgs/org-1/worker-keys", `{"name":"another"}`},
		{"m1", "/api/v1/orgs/org-1/my-runner-key", ""},
		{"admin", "/api/v1/projects/p1/share-links", `{"role":"public","label":"another"}`},
	} {
		if w := x.send(c.who, http.MethodPost, c.path, c.body); !planReadOnly(w) {
			t.Errorf("POST %s as %s on a read-only workspace: %d %s, want 403 plan_read_only", c.path, c.who, w.Code, w.Body.String())
		}
	}
	for _, c := range []struct{ who, path string }{
		{"admin", "/api/v1/orgs/org-1/logo"}, // org:admin
		{"m1", "/api/v1/artifacts/a-1"},      // project:editor
	} {
		if w := x.send(c.who, http.MethodDelete, c.path, ""); !planReadOnly(w) {
			t.Errorf("DELETE %s as %s on a read-only workspace: %d %s, want 403 plan_read_only", c.path, c.who, w.Code, w.Body.String())
		}
	}
	if len(x.artifacts.deleted) != 0 {
		t.Errorf("artifacts deleted on a read-only workspace: %v, want none", x.artifacts.deleted)
	}
}

// A write that touches only the caller's own session or lease is never
// refused for being over plan (#379 question 7): a member makes the
// workspace its session's active one, turns its own stable preview on and
// ends its cloud runner lease. Starting or extending a lease there is still
// refused.
func TestAReadOnlyWorkspaceStillTakesTheCallersOwnSessionAndLease(t *testing.T) {
	x := newReadOnlyWorkspace(t)
	if w := x.send("m1", http.MethodPost, "/api/v1/orgs/org-1/activate", ""); w.Code != http.StatusNoContent {
		t.Errorf("activate on a read-only workspace: %d %s, want 204", w.Code, w.Body.String())
	}
	if got := strings.Join(x.sessions.activated, ", "); got != "session-m1 -> org-1" {
		t.Errorf("sessions activated: %q, want session-m1 -> org-1", got)
	}
	if w := x.send("m1", http.MethodPut, "/api/v1/orgs/org-1/members/me/preview", `{"enabled":true}`); w.Code != http.StatusOK {
		t.Errorf("the stable preview on a read-only workspace: %d %s, want 200", w.Code, w.Body.String())
	}
	if !x.orgs.previews["org-1/m1"] {
		t.Errorf("m1's stable preview was not turned on: %v", x.orgs.previews)
	}
	if w := x.send("m1", http.MethodDelete, "/api/v1/orgs/org-1/runner-session", ""); w.Code != http.StatusOK {
		t.Errorf("ending the lease on a read-only workspace: %d %s, want 200", w.Code, w.Body.String())
	}
	if got := strings.Join(x.lease.ended, ", "); got != "lease-m1 "+runnersessions.EndReasonUser {
		t.Errorf("leases ended: %q, want lease-m1 %s", got, runnersessions.EndReasonUser)
	}

	for _, path := range []string{"/api/v1/orgs/org-1/runner-session", "/api/v1/orgs/org-1/runner-session/extend"} {
		if w := x.send("m1", http.MethodPost, path, ""); !planReadOnly(w) {
			t.Errorf("POST %s on a read-only workspace: %d %s, want 403 plan_read_only", path, w.Code, w.Body.String())
		}
	}
}

// Cancelling a run is never refused for being over plan (#379 question 8),
// for whoever may cancel it: a project editor who did not launch it, its
// launcher, who needs no role in the project (requireRunAccess lets a
// launcher by ahead of the project's ladder and so of its gate), and a
// workspace admin, who launched neither, for a run in no project. Each case
// passes on its own path alone. A retry by anyone but the run's launcher is
// still refused there, on either ladder; the launcher's own retry passes,
// since nothing on its path asks the gate, as its cancel passed before the
// fix. That is pinned as it behaves, question 8's retry part being left to
// the maintainer: refusing it would mean gating requireRunAccess's launcher
// branch, and this test turns red.
func TestAReadOnlyWorkspaceStillCancelsARun(t *testing.T) {
	x := newReadOnlyWorkspace(t)
	for _, c := range []struct{ who, run string }{
		{"m1", "run-1"},    // P's editor: the project's ladder
		{"m2", "run-1"},    // its launcher, with no role in P
		{"admin", "run-2"}, // a workspace admin, for a run in no project
	} {
		if w := x.send(c.who, http.MethodPost, "/api/v1/agent-runs/"+c.run+"/cancel", ""); w.Code != http.StatusOK {
			t.Errorf("%s cancels %s on a read-only workspace: %d %s, want 200", c.who, c.run, w.Code, w.Body.String())
		}
	}
	if got := strings.Join(x.runs.cancelled, ", "); got != "run-1, run-1, run-2" {
		t.Errorf("runs cancelled: %q, want run-1, run-1, run-2", got)
	}

	for _, c := range []struct{ who, run string }{
		{"m1", "run-1"},    // P's editor
		{"admin", "run-2"}, // a workspace admin, for a run in no project
	} {
		if w := x.send(c.who, http.MethodPost, "/api/v1/agent-runs/"+c.run+"/retry", ""); !planReadOnly(w) {
			t.Errorf("%s retries %s on a read-only workspace: %d %s, want 403 plan_read_only", c.who, c.run, w.Code, w.Body.String())
		}
	}
	if w := x.send("m2", http.MethodPost, "/api/v1/agent-runs/run-1/retry", ""); w.Code != http.StatusCreated {
		t.Errorf("m2 retries run-1, which it launched, on a read-only workspace: %d %s, want 201 (as it behaves)", w.Code, w.Body.String())
	}
	if got := strings.Join(x.runs.retryCalls, ", "); got != "run-1" {
		t.Errorf("runs retried: %q, want run-1, by its launcher alone", got)
	}
}
