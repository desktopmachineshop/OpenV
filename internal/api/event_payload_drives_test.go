package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/guided"
	"github.com/openv/requirements-platform/internal/domain/interviews"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/proposals"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/vv"
	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// The drives behind TestEventPayloadTypes: one per path that publishes a
// domain event. An HTTP drive's label is the route it serves (checked against
// the router), so the golden names publishers by route, not by Go function,
// and a pure move leaves it unchanged. Domain services that publish through
// their own bus (work items, test results, agent runs) run for real on
// in-memory repositories, sharing the recording bus, so their payloads are
// the ones production builds.

const (
	payloadOrg     = "org-1"
	payloadProject = "proj-1"
)

type payloadDrive struct {
	via string
	run func(t *testing.T, fx *payloadFixture)
}

type payloadFixture struct {
	h      *Handler
	router *mux.Router
	bus    *payloadBus
	admin  *users.User
}

// reqOpt sets who is calling. With none, the caller is a platform admin,
// which passes every role check, so a drive tests the publish, not authz.
type reqOpt func(*http.Request) *http.Request

func asWorkerOf(orgID string) reqOpt {
	return func(r *http.Request) *http.Request {
		return r.WithContext(context.WithValue(r.Context(), ctxWorkerOrg, orgID))
	}
}

func asAgentRun(run *agentruns.Run) reqOpt {
	return func(r *http.Request) *http.Request {
		return r.WithContext(context.WithValue(r.Context(), ctxRun, run))
	}
}

// anonymous is a caller with no session: the public interview link.
func anonymous(r *http.Request) *http.Request { return r }

func withSessionCookie(value string) reqOpt {
	return func(r *http.Request) *http.Request {
		r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: value})
		return r
	}
}

// httpDrive serves one request through the router. via is "METHOD
// /route/{template}", optionally followed by a note in parentheses; path is
// the concrete path.
func httpDrive(via, path, body string, opts ...reqOpt) payloadDrive {
	return payloadDrive{via: via, run: func(t *testing.T, fx *payloadFixture) {
		t.Helper()
		method, rest, _ := strings.Cut(via, " ")
		template, _, _ := strings.Cut(rest, " ")
		var r *http.Request
		if body == "" {
			r = httptest.NewRequest(method, path, nil)
		} else {
			r = httptest.NewRequest(method, path, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
		}
		r.RemoteAddr = "203.0.113.20:4000"
		if len(opts) == 0 {
			r = asUser(r, fx.admin)
		}
		for _, opt := range opts {
			r = opt(r)
		}
		var match mux.RouteMatch
		if !fx.router.Match(r, &match) || match.Route == nil {
			t.Fatalf("drive %q: %s %s matches no route", via, method, path)
		}
		if got, _ := match.Route.GetPathTemplate(); got != template {
			t.Fatalf("drive %q: %s %s is served by %s", via, method, path, got)
		}
		rec := httptest.NewRecorder()
		fx.router.ServeHTTP(rec, r)
		if rec.Code < 200 || rec.Code > 299 {
			t.Fatalf("drive %q: %s %s answered %d: %s", via, method, path, rec.Code, rec.Body.String())
		}
	}}
}

// callDrive runs an in-process publisher that no route reaches directly.
func callDrive(via string, run func(t *testing.T, fx *payloadFixture)) payloadDrive {
	return payloadDrive{via: via, run: run}
}

func payloadDrives() []payloadDrive {
	return []payloadDrive{
		// Artifacts, links, baselines, feed notes, review rounds.
		httpDrive("POST /api/v1/artifacts", "/api/v1/artifacts",
			`{"project_id":"proj-1","type":"requirement","title":"New requirement"}`),
		httpDrive("PUT /api/v1/artifacts/{id}", "/api/v1/artifacts/art-req", `{"title":"Renamed"}`),
		httpDrive("PUT /api/v1/artifacts/{id}/status", "/api/v1/artifacts/art-req/status", `{"status":"in_review"}`),
		httpDrive("POST /api/v1/artifacts/{id}/restore", "/api/v1/artifacts/art-req/restore", `{"version":1}`),
		httpDrive("DELETE /api/v1/artifacts/{id}", "/api/v1/artifacts/art-gone", ""),
		httpDrive("POST /api/v1/links", "/api/v1/links", `{"from_id":"art-tc","to_id":"art-req","type":"verifies"}`),
		httpDrive("DELETE /api/v1/links/{id}", "/api/v1/links/link-1", ""),
		httpDrive("POST /api/v1/projects/{id}/baselines", "/api/v1/projects/proj-1/baselines", `{"name":"Release 1"}`),
		httpDrive("DELETE /api/v1/baselines/{id}", "/api/v1/baselines/bl-old", ""),
		httpDrive("POST /api/v1/chatter", "/api/v1/chatter", `{"artifact_id":"art-req","message":"Looks good @sam"}`),
		httpDrive("POST /api/v1/projects/{id}/review-round", "/api/v1/projects/proj-1/review-round", `{"types":["requirement"]}`),
		// A guided commit publishes each approval it made.
		httpDrive("POST /api/v1/guided-sessions/{id}/commit", "/api/v1/guided-sessions/gs-1/commit", ""),
		// A proposal-mode agent's write is diverted into the review queue.
		httpDrive("POST /api/v1/artifacts (proposal-mode agent run)", "/api/v1/artifacts",
			`{"project_id":"proj-1","type":"requirement","title":"Proposed"}`, asAgentRun(proposalRun())),
		// Approved proposals apply through the appliers the proposal service calls.
		callDrive("proposal applier CreateArtifact", func(t *testing.T, fx *payloadFixture) {
			_, err := fx.h.ProposalAppliers().CreateArtifact(map[string]interface{}{
				"project_id": payloadProject, "type": "requirement", "title": "Applied"})
			mustApply(t, err)
		}),
		callDrive("proposal applier UpdateArtifact", func(t *testing.T, fx *payloadFixture) {
			_, err := fx.h.ProposalAppliers().UpdateArtifact("art-req", map[string]interface{}{"title": "Applied rename"})
			mustApply(t, err)
		}),
		callDrive("proposal applier DeleteArtifact", func(t *testing.T, fx *payloadFixture) {
			mustApply(t, fx.h.ProposalAppliers().DeleteArtifact("art-gone"))
		}),
		callDrive("proposal applier CreateLink", func(t *testing.T, fx *payloadFixture) {
			_, err := fx.h.ProposalAppliers().CreateLink(map[string]interface{}{
				"from_id": "art-tc", "to_id": "art-req", "type": "verifies"})
			mustApply(t, err)
		}),
		callDrive("proposal applier DeleteLink", func(t *testing.T, fx *payloadFixture) {
			mustApply(t, fx.h.ProposalAppliers().DeleteLink("link-1"))
		}),
		callDrive("proposal applier RecordTestResult", func(t *testing.T, fx *payloadFixture) {
			_, err := fx.h.ProposalAppliers().RecordTestResult(map[string]interface{}{
				"run_id": "tr-1", "test_case_id": "art-tc", "status": vv.ResultFail})
			mustApply(t, err)
		}),
		// Project membership.
		httpDrive("POST /api/v1/projects/{id}/members", "/api/v1/projects/proj-1/members",
			`{"email":"known@example.com","role":"editor"}`),
		httpDrive("PUT /api/v1/projects/{id}/members/{userId}", "/api/v1/projects/proj-1/members/u-known", `{"role":"viewer"}`),
		httpDrive("DELETE /api/v1/projects/{id}/members/{userId}", "/api/v1/projects/proj-1/members/u-known", ""),
		// Workspace membership and invitations.
		httpDrive("POST /api/v1/orgs/{id}/members (address with an account)", "/api/v1/orgs/org-1/members",
			`{"email":"known@example.com","role":"member"}`),
		httpDrive("POST /api/v1/orgs/{id}/members (address without an account)", "/api/v1/orgs/org-1/members",
			`{"email":"stranger@example.com","role":"member"}`),
		httpDrive("POST /api/v1/orgs/{id}/invitations", "/api/v1/orgs/org-1/invitations",
			`{"email":"newcomer@example.com","role":"admin"}`),
		httpDrive("PUT /api/v1/orgs/{id}/members/{userId}", "/api/v1/orgs/org-1/members/u-known", `{"role":"admin"}`),
		httpDrive("DELETE /api/v1/orgs/{id}/members/{userId}", "/api/v1/orgs/org-1/members/u-known", ""),
		httpDrive("POST /api/v1/auth/invitations/accept", "/api/v1/auth/invitations/accept",
			`{"token":"tok-invited@example.com"}`, withSessionCookie("cookie-invited")),
		callDrive("OIDC sign-in with a provider-verified address", func(t *testing.T, fx *payloadFixture) {
			fx.h.acceptInvitationsForProviderVerifiedEmail("u-sso", "sso@example.com")
		}),
		// Interviews: the public finish link.
		httpDrive("POST /api/v1/public/interviews/{token}/finish", "/api/v1/public/interviews/good-token/finish", "",
			anonymous),
		// Work items, through the real service.
		httpDrive("POST /api/v1/projects/{id}/work-items", "/api/v1/projects/proj-1/work-items",
			`{"title":"Write the test plan","column":"todo","assignee_type":"agent","assignee_id":"agent-1"}`),
		httpDrive("PUT /api/v1/work-items/{id}", "/api/v1/work-items/wi-assigned",
			`{"title":"Write the test plan (v2)","assignee_type":"agent","assignee_id":"agent-1"}`),
		httpDrive("POST /api/v1/work-items/{id}/move (assigned card)", "/api/v1/work-items/wi-assigned/move",
			`{"column":"todo","sort_order":3}`),
		httpDrive("POST /api/v1/work-items/{id}/move (unassigned card)", "/api/v1/work-items/wi-open/move",
			`{"column":"done","sort_order":1}`),
		// Test results, through the real V&V service.
		httpDrive("POST /api/v1/test-runs/{id}/results", "/api/v1/test-runs/tr-1/results",
			`{"test_case_id":"art-tc","status":"pass","notes":"ok"}`),
		// A worker finishing a run, through the real run service.
		httpDrive("POST /api/v1/agent-runs/{id}/finish", "/api/v1/agent-runs/run-live/finish",
			`{"status":"succeeded","final_text":"Done.","tokens_in":10,"tokens_out":20}`, asWorkerOf(payloadOrg)),
		// A crew run that finished over its workspace's budget: the
		// orchestration hooks record the agent successors the budget refused
		// through the run service, which no route calls directly.
		callDrive("crew run finished over budget (orchestration hooks)", func(t *testing.T, fx *payloadFixture) {
			project, team := payloadProject, "team-1"
			run := &agentruns.Run{ID: "run-live", OrgID: payloadOrg, AgentID: "agent-1", ProjectID: &project, TeamID: &team}
			refusal := fmt.Errorf("%w: this workspace has reached its $1.00 monthly budget ($5.00 spent)", agentruns.ErrBudgetExceeded)
			if err := runServiceOf(fx.h).SuccessorsSkipped(run, []agentruns.Successor{{NodeID: "node-2", Label: "Checker"}}, refusal); err != nil {
				t.Fatalf("SuccessorsSkipped: %v", err)
			}
		}),
	}
}

func mustApply(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("applier failed: %v", err)
	}
}

func proposalRun() *agentruns.Run {
	project := payloadProject
	return &agentruns.Run{ID: "run-proposal", OrgID: payloadOrg, AgentID: "agent-proposal", ProjectID: &project, Status: agentruns.StatusRunning}
}

// newPayloadFixture wires one handler for every drive, starting from the
// registration fixture (users, invitations and the limiters) and adding the
// services the other drives reach.
func newPayloadFixture(t *testing.T) *payloadFixture {
	t.Helper()
	h, logins, invites := newRegistrationHandler(t, "")
	// The services are set through HandlerDeps's exported names, which a
	// rename of Handler's private fields (plan M14) leaves alone.
	var deps HandlerDeps
	bus := &payloadBus{}
	deps.Bus = bus

	known := &users.User{ID: "u-known", Email: "known@example.com", Name: "Known", EmailVerified: true}
	logins.accounts = map[string]*users.User{"known@example.com": known}
	logins.sessions = map[string]*users.User{"cookie-invited": {ID: "u-invited", Email: "invited@example.com"}}
	invites.invite(payloadOrg, "invited@example.com", orgs.RoleMember)
	invites.invite(payloadOrg, "sso@example.com", orgs.RoleAdmin)

	arts := &payloadArtifacts{applierArtifactService: &applierArtifactService{byID: map[string]*artifacts.Artifact{
		"art-req":  {ID: "art-req", ProjectID: payloadProject, Type: "requirement", Title: "Requirement", Status: artifacts.StatusDraft, Version: 2},
		"art-tc":   {ID: "art-tc", ProjectID: payloadProject, Type: "test-case", Title: "Test case", Status: artifacts.StatusDraft, Version: 1},
		"art-gone": {ID: "art-gone", ProjectID: payloadProject, Type: "requirement", Title: "Gone", Version: 1},
	}}}
	chatterSvc := &fakeChatterService{}
	deps.ArtifactService = arts
	deps.LinkService = &applierLinkService{byID: map[string]*links.Link{
		"link-1": {ID: "link-1", FromID: "art-tc", ToID: "art-req", Type: "verifies"},
	}}
	deps.ProjectService = &fakeProjectService{byID: map[string]*projects.Project{
		payloadProject: {ID: payloadProject, OrgID: payloadOrg, Name: "Project"},
	}}
	deps.ChatterService = chatterSvc
	deps.ExportService = &fakeExportService{data: []byte(`{"artifacts":[]}`)}
	deps.BaselineService = &payloadBaselines{}
	deps.MemberService = &payloadMembers{}
	deps.OrgService = &payloadOrgs{fakeMemberOrgs: &fakeMemberOrgs{roles: map[string]string{}}}
	deps.AgentService = &fakeAgentService{byID: map[string]*agents.Agent{
		"agent-proposal": {ID: "agent-proposal", OrgID: payloadOrg, WriteMode: agents.WriteModeProposal},
	}}
	deps.ProposalService = &payloadProposals{}
	deps.GuidedService = &payloadGuided{arts: arts}
	project := payloadProject
	deps.InterviewService = &payloadInterviews{fakeInterviewService: &fakeInterviewService{
		interview: &interviews.Interview{ID: "iv-1", ProjectID: project},
		invite:    &interviews.Invite{ID: "invite-1", InterviewID: "iv-1"},
		session:   &interviews.Session{ID: "session-1", InviteID: "invite-1", InterviewID: "iv-1"},
	}}

	agentID := "agent-1"
	deps.WorkItemService = workitems.NewDefaultService(&payloadWorkItems{items: map[string]*workitems.WorkItem{
		"wi-assigned": {ID: "wi-assigned", ProjectID: payloadProject, Title: "Assigned", Column: workitems.ColumnBacklog,
			AssigneeType: workitems.AssigneeAgent, AssigneeID: &agentID, ArtifactIDs: []string{}},
		"wi-open": {ID: "wi-open", ProjectID: payloadProject, Title: "Open", Column: workitems.ColumnTodo,
			AssigneeType: workitems.AssigneeUser, ArtifactIDs: []string{}},
	}}, bus)
	deps.VVService = vv.NewDefaultService(&payloadTestRuns{run: &vv.TestRun{ID: "tr-1", ProjectID: payloadProject, Name: "Run 1",
		Status: vv.RunStatusInProgress}},
		arts, chatterSvc, bus)
	launchedBy := "u-launcher"
	deps.RunService = agentruns.NewDefaultService(&payloadRuns{run: &agentruns.Run{
		ID: "run-live", OrgID: payloadOrg, AgentID: "agent-1", ProjectID: &project, LaunchedBy: &launchedBy,
		Status: agentruns.StatusRunning,
	}}, nil, bus)

	setTestServices(h, deps)

	router := mux.NewRouter()
	h.RegisterRoutes(router)
	return &payloadFixture{
		h: h, router: router, bus: bus,
		admin: &users.User{ID: "u-admin", Email: "admin@example.com", Name: "Admin", IsAdmin: true},
	}
}

// ---- Fakes: each embeds its interface, so a method a drive newly reaches
// panics loudly instead of answering wrongly. ----

type payloadArtifacts struct {
	*applierArtifactService
}

func (f *payloadArtifacts) ChangeStatus(id, status string) (*artifacts.Artifact, error) {
	a, err := f.GetArtifact(id)
	if err != nil {
		return nil, err
	}
	changed := *a
	changed.Status = status
	return &changed, nil
}

// RestoreArtifactVersion answers the requirement at its next version, with
// the restored version's title.
func (f *payloadArtifacts) RestoreArtifactVersion(id string, version int) (*artifacts.Artifact, error) {
	a, err := f.GetArtifact(id)
	if err != nil {
		return nil, err
	}
	restored := *a
	restored.Title = "Requirement, as first written"
	restored.Version++
	return &restored, nil
}

func (f *payloadArtifacts) StartProjectReview(projectID string, req artifacts.ReviewRoundRequest) (*artifacts.ReviewRoundResult, error) {
	moved := *f.byID["art-req"]
	moved.Status = artifacts.StatusInReview
	return &artifacts.ReviewRoundResult{Moved: []*artifacts.Artifact{&moved}, AlreadyInReview: 1, Approved: 2, Types: req.Types}, nil
}

// payloadGuided serves one session of the project, whose commit approves
// the fixture's requirement.
type payloadGuided struct {
	guided.Service
	arts *payloadArtifacts
}

func (f *payloadGuided) GetSession(id string) (*guided.Session, error) {
	return &guided.Session{ID: id, ProjectID: payloadProject, Status: guided.StatusInProgress}, nil
}

func (f *payloadGuided) Commit(sessionID string) (*guided.CommitResult, error) {
	approved := *f.arts.byID["art-req"]
	approved.Status = artifacts.StatusApproved
	approved.Version++
	session, _ := f.GetSession(sessionID)
	session.Status = guided.StatusCommitted
	return &guided.CommitResult{Session: session, Approved: []*artifacts.Artifact{&approved}}, nil
}

type payloadBaselines struct{ baselines.Service }

func (f *payloadBaselines) CreateBaseline(projectID, name string, snapshot []byte, createdBy *string) (*baselines.Baseline, error) {
	return &baselines.Baseline{ID: "bl-1", ProjectID: projectID, Name: name}, nil
}

// GetBaseline answers bl-old, the baseline the delete drive removes.
func (f *payloadBaselines) GetBaseline(id string) (*baselines.Baseline, error) {
	if id != "bl-old" {
		return nil, baselines.ErrNotFound
	}
	return &baselines.Baseline{ID: id, ProjectID: payloadProject, Name: "Release 0"}, nil
}

func (f *payloadBaselines) DeleteBaseline(id string) error { return nil }

type payloadMembers struct{ members.Service }

func (f *payloadMembers) AddMember(projectID, userID, role string) error { return nil }
func (f *payloadMembers) SetRole(projectID, userID, role string) error   { return nil }
func (f *payloadMembers) RemoveMember(projectID, userID string) error    { return nil }
func (f *payloadMembers) RoleFor(projectID, userID string) (string, error) {
	return members.RoleEditor, nil
}

type payloadOrgs struct{ *fakeMemberOrgs }

func (f *payloadOrgs) SetMemberRole(orgID, userID, role string) error { return nil }
func (f *payloadOrgs) RemoveMember(orgID, userID string) error        { return nil }
func (f *payloadOrgs) ListMembers(orgID string) ([]*orgs.Member, error) {
	return nil, nil
}

type payloadProposals struct{ proposals.Service }

func (f *payloadProposals) Propose(runID, projectID, op string, targetID *string, payload map[string]interface{}) (*proposals.Proposal, error) {
	return &proposals.Proposal{ID: "prop-1", RunID: runID, ProjectID: projectID, Op: op, Status: proposals.StatusPending}, nil
}

type payloadInterviews struct{ *fakeInterviewService }

func (f *payloadInterviews) CompleteSession(sessionID, summary string) error { return nil }

// payloadWorkItems is an in-memory workitems.Repository.
type payloadWorkItems struct {
	workitems.Repository
	items map[string]*workitems.WorkItem
}

func (f *payloadWorkItems) Save(item *workitems.WorkItem) error {
	f.items[item.ID] = item
	return nil
}

func (f *payloadWorkItems) Update(item *workitems.WorkItem) error {
	f.items[item.ID] = item
	return nil
}

func (f *payloadWorkItems) FindByID(id string) (*workitems.WorkItem, error) {
	if item, ok := f.items[id]; ok {
		copied := *item
		return &copied, nil
	}
	return nil, errors.New("work item not found")
}

func (f *payloadWorkItems) SaveActivity(*workitems.Activity) error             { return nil }
func (f *payloadWorkItems) MaxSortOrder(projectID, column string) (int, error) { return 0, nil }

// payloadTestRuns is the slice of vv.Repository UpsertResult reaches.
type payloadTestRuns struct {
	vv.Repository
	run *vv.TestRun
}

func (f *payloadTestRuns) FindRunByID(id string) (*vv.TestRun, error) {
	if id != f.run.ID {
		return nil, vv.ErrRunNotFound
	}
	return f.run, nil
}

func (f *payloadTestRuns) FindResultByCase(runID, testCaseID string) (*vv.TestResult, error) {
	return nil, nil
}

func (f *payloadTestRuns) AddResult(*vv.TestResult) error { return nil }

// payloadRuns is the slice of agentruns.Repository Finish reaches.
type payloadRuns struct {
	agentruns.Repository
	run *agentruns.Run
}

func (f *payloadRuns) FindByID(id string) (*agentruns.Run, error) {
	if id != f.run.ID {
		return nil, nil
	}
	copied := *f.run
	return &copied, nil
}

func (f *payloadRuns) CountPendingProposals(runID string) (int, error) { return 0, nil }
func (f *payloadRuns) AppendNote(runID string, e agentruns.LogEntry) (agentruns.LogEntry, error) {
	e.RunID, e.Seq = runID, 1
	return e, nil
}
func (f *payloadRuns) UpdateTerminal(r *agentruns.Run) (bool, error) {
	f.run.Status = r.Status
	f.run.FinishedAt = &time.Time{}
	return true, nil
}
