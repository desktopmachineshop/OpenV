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
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/runnersessions"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// The tiers as sold (Phase 4). Every test here throws the switch the
// operator throws, and puts it back.

func enforceTiers(t *testing.T) {
	t.Helper()
	orgs.SetTiersEnforced(true)
	t.Cleanup(func() { orgs.SetTiersEnforced(false) })
}

func platformAdmin(method, path string, orgID string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(`{"name":"QA"}`))
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "root", IsAdmin: true}))
	return mux.SetURLVars(r, map[string]string{"id": orgID})
}

func seats(n int) []*orgs.Member {
	out := make([]*orgs.Member, n)
	for i := range out {
		out[i] = &orgs.Member{OrgID: "org-1", UserID: "u" + string(rune('1'+i)), Role: orgs.RoleMember}
	}
	return out
}

// The funnel: a shared workspace on the free tier seats two, so the second
// invitation is the one refused, and the refusal names the Billing tab.
func TestTheSecondInviteIsRefusedOnAFreeSharedWorkspace(t *testing.T) {
	enforceTiers(t)
	h := NewHandler(HandlerDeps{})
	h.orgService = &seatedOrgService{
		org:     &orgs.Org{ID: "org-1", OrgType: orgs.TypeCompany, BilledPlan: orgs.PlanSingle},
		members: seats(1),
	}
	if err := h.checkOrgSeats("org-1", 1); err != nil {
		t.Fatalf("the first invite was refused: %v", err)
	}
	h.orgService.(*seatedOrgService).members = seats(2)
	err := h.checkOrgSeats("org-1", 1)
	if err == nil {
		t.Fatal("the second invite was allowed on a two-seat tier")
	}
	if !strings.Contains(err.Error(), "Billing tab") {
		t.Fatalf("the refusal does not name the Billing tab: %v", err)
	}
	// Business bills per seat and caps nothing.
	h.orgService.(*seatedOrgService).org.BilledPlan = orgs.PlanBusiness
	h.orgService.(*seatedOrgService).members = seats(40)
	if err := h.checkOrgSeats("org-1", 1); err != nil {
		t.Fatalf("Business refused a seat: %v", err)
	}
}

// Unattended hosted compute is Lite's gate, enforced at the hosted claim:
// the same run claimed by the member's own machine is never gated.
func TestTheHostedClaimIsRefusedForAFreeWorkspaceWhileAConnectorClaimSucceeds(t *testing.T) {
	enforceTiers(t)
	run := &agentruns.Run{ID: "run-1", OrgID: "org-1", AgentID: "agent-1", Status: agentruns.StatusClaimed, WorkerID: "w-1"}
	h := &Handler{
		runService:   &fakeRunService{claimRun: run},
		agentService: &fakeAgentService{byID: map[string]*agents.Agent{"agent-1": {ID: "agent-1", Name: "Agent", Provider: "claude"}}},
		orgService:   &seatedOrgService{org: &orgs.Org{ID: "org-1", OrgType: orgs.TypeCompany, BilledPlan: orgs.PlanSingle}},
	}
	claim := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/agent-runs/claim", strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), ctxWorkerOrg, "org-1"))
		w := httptest.NewRecorder()
		h.ClaimAgentRun(w, r)
		return w
	}
	if w := claim(`{"worker_id":"w-1","hosted":true}`); w.Code != http.StatusForbidden {
		t.Fatalf("hosted claim on free: %d %s", w.Code, w.Body.String())
	} else {
		var body errorBody
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body.Code != ErrCodeLimitReached || !strings.Contains(body.Error, "Always-on hosted agents") {
			t.Fatalf("hosted refusal body: %s", w.Body.String())
		}
	}
	if w := claim(`{"worker_id":"w-1","hosted":false}`); w.Code != http.StatusOK {
		t.Fatalf("connector claim on free: %d %s", w.Code, w.Body.String())
	}
	// Lite includes it.
	h.orgService.(*seatedOrgService).org.BilledPlan = orgs.PlanBusinessLite
	if w := claim(`{"worker_id":"w-1","hosted":true}`); w.Code != http.StatusOK {
		t.Fatalf("hosted claim on Lite: %d %s", w.Code, w.Body.String())
	}
}

func TestCreatingATeamIsRefusedBelowBusiness(t *testing.T) {
	enforceTiers(t)
	h := NewHandler(HandlerDeps{})
	svc := &seatedOrgService{org: &orgs.Org{ID: "org-1", OrgType: orgs.TypeCompany, BilledPlan: orgs.PlanBusinessLite}, members: seats(1)}
	h.orgService = svc

	w := httptest.NewRecorder()
	h.CreateOrgTeam(w, platformAdmin(http.MethodPost, "/api/v1/orgs/org-1/teams", "org-1"))
	var body errorBody
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusForbidden || body.Code != ErrCodeLimitReached || !strings.Contains(body.Error, "Teams and per-project access") {
		t.Fatalf("team on Lite: %d %s", w.Code, w.Body.String())
	}
	// Grandfathered: the alpha terms in its own limits open the gate.
	svc.org.Limits = orgs.AlphaTerms()
	if err := h.checkFlag("org-1", orgs.LimitTeams); err != nil {
		t.Fatalf("a grandfathered workspace was refused a team: %v", err)
	}
	// And Business includes it.
	svc.org.Limits, svc.org.BilledPlan = nil, orgs.PlanBusiness
	if err := h.checkFlag("org-1", orgs.LimitTeams); err != nil {
		t.Fatalf("Business was refused a team: %v", err)
	}
}

// Over plan is read-only, not deleted: an edit is refused with the code and
// the remedy, removing a member is allowed (it is how the workspace gets
// back under plan), and a read — export — is never refused.
func TestAnOverPlanWorkspaceIsReadOnlyButTrimsAndExports(t *testing.T) {
	enforceTiers(t)
	h := NewHandler(HandlerDeps{})
	h.orgService = &seatedOrgService{
		org:     &orgs.Org{ID: "org-1", OrgType: orgs.TypeCompany, BilledPlan: orgs.PlanSingle},
		members: seats(3), // one more than the free tier seats
	}
	h.projectService = &fakeProjectService{byID: map[string]*projects.Project{"p1": {ID: "p1", OrgID: "org-1"}}}

	// An artifact edit: a write scoped to the project.
	w := httptest.NewRecorder()
	if h.requireProjectRole(w, platformAdmin(http.MethodPost, "/api/v1/projects/p1/artifacts", "p1"), "p1", members.RoleEditor) {
		t.Fatal("a write on an over-plan workspace was allowed")
	}
	var body struct {
		errorBody
		Over   []string `json:"over"`
		Remedy string   `json:"remedy"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusForbidden || body.Code != ErrCodePlanReadOnly || len(body.Over) != 1 || body.Over[0] != orgs.LimitMaxMembers {
		t.Fatalf("read-only refusal: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(body.Remedy, "exportable") || !strings.Contains(body.Remedy, "Billing tab") {
		t.Fatalf("remedy: %q", body.Remedy)
	}
	// A workspace-scoped write, the same.
	w = httptest.NewRecorder()
	if h.requireOrgRole(w, platformAdmin(http.MethodPut, "/api/v1/orgs/org-1", "org-1"), "org-1", orgs.RoleAdmin) {
		t.Fatal("a workspace write on an over-plan workspace was allowed")
	}

	// Export is a read: never refused.
	w = httptest.NewRecorder()
	if !h.requireProjectRole(w, platformAdmin(http.MethodGet, "/api/v1/projects/p1/export", "p1"), "p1", members.RoleViewer) {
		t.Fatalf("export refused on a read-only workspace: %s", w.Body.String())
	}

	// Removing a member is one of the writes that is always allowed.
	allowed := false
	h.alwaysWritable(func(w http.ResponseWriter, r *http.Request) {
		allowed = h.requireOrgRole(w, r, "org-1", orgs.RoleAdmin)
	})(httptest.NewRecorder(), platformAdmin(http.MethodDelete, "/api/v1/orgs/org-1/members/u3", "org-1"))
	if !allowed {
		t.Fatal("removing a member was refused on a read-only workspace")
	}

	// Trimmed back under plan: writable again, and the panel says so.
	h.orgService.(*seatedOrgService).members = seats(2)
	w = httptest.NewRecorder()
	if !h.requireProjectRole(w, platformAdmin(http.MethodPost, "/api/v1/projects/p1/artifacts", "p1"), "p1", members.RoleEditor) {
		t.Fatalf("still read-only after trimming: %s", w.Body.String())
	}
	resp, err := h.buildLimitsResponse("org-1")
	if err != nil || resp.ReadOnly || len(resp.OverPlan) != 0 {
		t.Fatalf("limits after trimming: %+v %v", resp, err)
	}
	h.orgService.(*seatedOrgService).members = seats(3)
	resp, _ = h.buildLimitsResponse("org-1")
	if !resp.ReadOnly || len(resp.OverPlan) != 1 {
		t.Fatalf("limits while over plan: read_only=%v over=%v", resp.ReadOnly, resp.OverPlan)
	}
	// Flags are in the panel now, with their reading.
	found := false
	for _, l := range resp.Limits {
		if l.Key == orgs.LimitTeams {
			found = true
			if l.Kind != orgs.KindFlag || l.Included == nil || *l.Included {
				t.Fatalf("teams flag on free: %+v", l)
			}
		}
	}
	if !found {
		t.Fatal("flags are missing from the limits panel")
	}
}

// The month's cloud-runner allowance is hard: a lease is cut to what is left
// and refused once nothing is. Nothing changes for a plan with no ceiling.
func TestALeaseIsCutToTheMonthsAllowanceAndRefusedAtIt(t *testing.T) {
	t.Cleanup(func() { orgs.SetDeploymentLimits(nil) })
	orgs.SetDeploymentLimits(map[string]interface{}{orgs.LimitHostedRunnerMinutesMonth: 100})
	svc := &fakeRunnerSessions{minutesUsed: 90, session: &runnersessions.Session{ID: "s1", OrgID: "org-1", UserID: "user-1", Status: runnersessions.StatusActive}}
	h := &Handler{runnerSessionService: svc, orgService: memberOrgService()}

	w := httptest.NewRecorder()
	h.StartRunnerSession(w, memberRequest(http.MethodPost, "/api/v1/orgs/org-1/runner-session"))
	if w.Code != http.StatusCreated || svc.startedWith != 10 {
		t.Fatalf("lease with 10 minutes left: %d, asked for %d minutes; %s", w.Code, svc.startedWith, w.Body.String())
	}

	svc.minutesUsed = 100
	w = httptest.NewRecorder()
	h.StartRunnerSession(w, memberRequest(http.MethodPost, "/api/v1/orgs/org-1/runner-session"))
	var body errorBody
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusForbidden || body.Code != ErrCodeLimitReached || !strings.Contains(body.Error, "Agent Connector") {
		t.Fatalf("lease at the allowance: %d %s", w.Code, w.Body.String())
	}

	orgs.SetDeploymentLimits(nil)
	svc.startedWith = 0
	w = httptest.NewRecorder()
	h.StartRunnerSession(w, memberRequest(http.MethodPost, "/api/v1/orgs/org-1/runner-session"))
	if w.Code != http.StatusCreated || svc.startedWith != runnersessions.DefaultSessionMinutes {
		t.Fatalf("uncapped lease: %d, %d minutes", w.Code, svc.startedWith)
	}
}
