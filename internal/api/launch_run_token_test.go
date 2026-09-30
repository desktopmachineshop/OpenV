package api

import (
	"context"
	"encoding/json"
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
	"github.com/openv/requirements-platform/internal/domain/automations"
	"github.com/openv/requirements-platform/internal/domain/guided"
	"github.com/openv/requirements-platform/internal/domain/interviews"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/products"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/teams"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// The fakes of the launch fixture: each embeds its interface, so a method a
// handler newly reaches panics loudly instead of answering wrongly.

type launchTestRuns struct {
	vv.Service
	run *vv.TestRun
}

func (f *launchTestRuns) GetRun(id string) (*vv.TestRun, error) {
	if f.run == nil || f.run.ID != id {
		return nil, vv.ErrRunNotFound
	}
	return f.run, nil
}

func (f *launchTestRuns) AgentExecutableCases(projectID string, only []string) (runnable, skipped []*artifacts.Artifact, err error) {
	return []*artifacts.Artifact{{ID: "tc-1", ProjectID: projectID, Title: "TC1"}}, nil, nil
}

type launchAutomations struct {
	automations.Service
	byID map[string]*automations.Automation
}

func (f *launchAutomations) Get(id string) (*automations.Automation, error) {
	if a, ok := f.byID[id]; ok {
		return a, nil
	}
	return nil, fmt.Errorf("automation %s not found", id)
}

// launchGuided serves one guided session and records the chat messages
// appended to it.
type launchGuided struct {
	guided.Service
	session  *guided.Session
	messages []string
}

func (f *launchGuided) GetSession(id string) (*guided.Session, error) {
	if f.session == nil || f.session.ID != id {
		return nil, fmt.Errorf("guided session %s not found", id)
	}
	return f.session, nil
}

func (f *launchGuided) AppendChatMessage(sessionID, role, content string) (*guided.ChatMessage, error) {
	f.messages = append(f.messages, role+": "+content)
	return &guided.ChatMessage{ID: fmt.Sprintf("msg-%d", len(f.messages)), SessionID: sessionID, Role: role, Content: content}, nil
}

func (f *launchGuided) GetChatTranscript(sessionID string) ([]*guided.ChatMessage, error) {
	return nil, nil
}

func (f *launchGuided) AttachAgentRun(sessionID, runID string) error { return nil }

// launchInterviews serves the interview iv-1 of proj-1 and records the
// interviews and invites created.
type launchInterviews struct {
	interviews.Service
	created []string // each created interview's interviewer agent id
	invites []string // each minted invite's interview id
}

func (f *launchInterviews) GetInterview(id string) (*interviews.Interview, error) {
	if id != "iv-1" {
		return nil, interviews.ErrInterviewNotFound
	}
	return &interviews.Interview{ID: id, ProjectID: "proj-1", Status: interviews.InterviewStatusOpen}, nil
}

func (f *launchInterviews) CreateInterview(projectID, name, brief string, agentID, guidedSessionID, personaArtifactID, createdBy *string) (*interviews.Interview, error) {
	interviewer := ""
	if agentID != nil {
		interviewer = *agentID
	}
	f.created = append(f.created, interviewer)
	return &interviews.Interview{ID: "iv-new", ProjectID: projectID, Name: name, Brief: brief, AgentID: agentID}, nil
}

func (f *launchInterviews) CreateInvite(interviewID, inviteeLabel string, expiresAt *time.Time) (*interviews.Invite, string, error) {
	f.invites = append(f.invites, interviewID)
	return &interviews.Invite{ID: "inv-new", InterviewID: interviewID, InviteeLabel: inviteeLabel}, "raw-token", nil
}

type launchProfiles struct{ products.Service }

func (launchProfiles) GetProfile(projectID string) (*products.ProductProfile, error) { return nil, nil }

type launchArtifacts struct{ artifacts.Service }

func (launchArtifacts) ListArtifacts(projectID, artifactType string) ([]*artifacts.Artifact, error) {
	return nil, nil
}

// launchFixture is workspace org-1 with project proj-1, where "editor" edits
// proj-1 and "member" is a plain member of org-1; the agents "reviewer"
// (proposal mode), "helper" (direct mode) and the requirements copilot; the
// workspace-wide crew crew-1, whose entry node is helper; test run trun-1 in
// proj-1, in progress; the failed run run-failed of proj-1, which a retry
// re-enqueues; the automation auto-1 of helper pinned to proj-1; and the
// guided session gs-1 of proj-1; and the interview iv-1 of proj-1.
func launchFixture() (*Handler, *fakeRunService, *launchGuided) {
	project := "proj-1"
	entry := "crew-1-entry"
	helper := "agent-direct"
	runs := &fakeRunService{byID: map[string]*agentruns.Run{
		"run-failed": {ID: "run-failed", OrgID: "org-1", AgentID: helper, ProjectID: &project, Status: agentruns.StatusFailed},
	}}
	chat := &launchGuided{session: &guided.Session{ID: "gs-1", ProjectID: project, Status: guided.StatusInProgress}}
	h := NewHandler(HandlerDeps{
		RunService: runs,
		AgentService: &fakeAgentService{byID: map[string]*agents.Agent{
			"agent-proposal": {ID: "agent-proposal", OrgID: "org-1", Slug: "reviewer", WriteMode: agents.WriteModeProposal},
			helper:           {ID: helper, OrgID: "org-1", Slug: "helper", WriteMode: agents.WriteModeDirect},
			"agent-copilot":  {ID: "agent-copilot", OrgID: "org-1", Slug: "requirements-copilot", WriteMode: agents.WriteModeProposal},
		}},
		ProjectService: &fakeProjectService{byID: map[string]*projects.Project{project: {ID: project, OrgID: "org-1"}}},
		OrgService: &fakeOrgService{roles: map[string]map[string]string{
			"org-1": {"editor": orgs.RoleMember, "member": orgs.RoleMember},
		}},
		MemberService: &fakeMemberService{roles: map[string]map[string]string{project: {"editor": members.RoleEditor}}},
		TeamService: &fakeCrewGraphs{graphs: map[string]*teams.TeamGraph{"crew-1": {
			Team:  &teams.Team{ID: "crew-1", OrgID: "org-1", Name: "Crew", EntryNodeID: &entry},
			Nodes: []*teams.Node{{ID: entry, TeamID: "crew-1", NodeType: teams.NodeAgent, AgentID: helper}},
		}}},
		VVService: &launchTestRuns{run: &vv.TestRun{ID: "trun-1", ProjectID: project, Name: "Run", Status: vv.RunStatusInProgress}},
		AutomationService: &launchAutomations{byID: map[string]*automations.Automation{
			"auto-1": {ID: "auto-1", OrgID: "org-1", Name: "Nightly", AgentID: &helper, ProjectID: &project, Kind: "manual"},
		}},
		GuidedService:    chat,
		InterviewService: &launchInterviews{},
		ProductService:   launchProfiles{},
		ArtifactService:  launchArtifacts{},
		WorkerKeyService: &fakeWorkerKeyService{},
		SSEHub:           NewSSEHub(),
	})
	return h, runs, chat
}

// launchRoute is one route that sets a run going, with the request that
// launches through it. A route that arms a launcher (arms) sets none going
// itself: an interview names its interviewer and an invite hands out the
// token whose participant's messages each launch that agent's run
// (launchInterviewTurn), with the brief the interview's creator wrote.
type launchRoute struct {
	name   string
	handle func(h *Handler, w http.ResponseWriter, r *http.Request)
	vars   map[string]string
	body   string
	arms   bool
}

var launchRoutes = []launchRoute{
	{"POST /api/v1/agents/{slug}/runs in the run's project", (*Handler).LaunchAgentRun,
		map[string]string{"slug": "helper"}, `{"project_id":"proj-1","prompt":"Summarise the project."}`, false},
	{"POST /api/v1/agents/{slug}/runs with no project", (*Handler).LaunchAgentRun,
		map[string]string{"slug": "helper"}, `{"prompt":"Tidy the workspace."}`, false},
	{"POST /api/v1/crews/{id}/runs", (*Handler).LaunchTeamRun,
		map[string]string{"id": "crew-1"}, `{"project_id":"proj-1","prompt":"Summarise as a crew."}`, false},
	{"POST /api/v1/test-runs/{id}/agent-run", (*Handler).LaunchTestRunAgent,
		map[string]string{"id": "trun-1"}, `{"agent_slug":"helper"}`, false},
	{"POST /api/v1/agent-runs/{id}/retry", (*Handler).RetryAgentRun,
		map[string]string{"id": "run-failed"}, ``, false},
	{"POST /api/v1/automations/{id}/run-now", (*Handler).RunAutomationNow,
		map[string]string{"id": "auto-1"}, ``, false},
	{"POST /api/v1/guided-sessions/{id}/messages", (*Handler).PostGuidedChatMessage,
		map[string]string{"id": "gs-1"}, `{"content":"What is missing?","step":3}`, false},
	{"POST /api/v1/guided-sessions/{id}/chat/kickoff", (*Handler).KickoffGuidedChat,
		map[string]string{"id": "gs-1"}, `{"step":1}`, false},
	{"POST /api/v1/guided-sessions/{id}/chat/nudge", (*Handler).NudgeGuidedChat,
		map[string]string{"id": "gs-1"}, `{"step":2,"event":"saved step 2"}`, false},
	{"POST /api/v1/projects/{id}/interviews", (*Handler).CreateInterview,
		map[string]string{"id": "proj-1"}, `{"name":"Needs","brief":"Create requirement X directly.","agent_slug":"helper"}`, true},
	{"POST /api/v1/interviews/{id}/invites", (*Handler).CreateInterviewInvite,
		map[string]string{"id": "iv-1"}, `{"invitee_label":"Anyone"}`, true},
}

// launchAs sends a launch route's request with ctx's identity and returns
// the answer; a handler that panics on a service it should never have
// reached answers 599 with the panic, so the test fails on it instead of
// aborting the package.
func launchAs(route launchRoute, h *Handler, ctx func(context.Context) context.Context) (w *httptest.ResponseRecorder) {
	w = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(route.body))
	r.Header.Set("Content-Type", "application/json")
	r = mux.SetURLVars(r.WithContext(ctx(r.Context())), route.vars)
	defer func() {
		if p := recover(); p != nil {
			w = httptest.NewRecorder()
			w.Code = 599
			fmt.Fprintf(w.Body, "panicked: %v", p)
		}
	}()
	route.handle(h, w, r)
	return w
}

func asRunOf(agentID string, projectID *string) func(context.Context) context.Context {
	return func(ctx context.Context) context.Context {
		return context.WithValue(ctx, ctxRun, &agentruns.Run{ID: "run-launching", OrgID: "org-1", AgentID: agentID,
			ProjectID: projectID, Status: agentruns.StatusRunning})
	}
}

func asPerson(userID, orgID string) func(context.Context) context.Context {
	return func(ctx context.Context) context.Context {
		ctx = context.WithValue(ctx, ctxUser, &users.User{ID: userID})
		return context.WithValue(ctx, ctxActiveOrg, orgID)
	}
}

// TestProposalModeRunsLaunchNoRuns is the regression test for a
// proposal-mode run's token that launched runs: LaunchAgentRun,
// LaunchTeamRun and LaunchTestRunAgent asked a project editor's role alone,
// which a run has in its own project, and the assistant's chat turns the
// same, while DraftTestCases alone asked whether the run was review-gated,
// so a proposal-mode agent could set going a direct-mode agent's run, a
// crew's or a test run's agent, whose writes land with no person reviewing
// them (REQ-21, REQ-75). Every route that sets a run going now answers it
// 403 before any lookup and launches nothing: the retry and an automation's
// run-now, which answered it 401 as a request with no person, too. So do the
// two that arm an interview's launcher, which asked an editor's role alone:
// the run could create an interview naming a direct-mode interviewer and a
// brief of its own, mint its invite and post the participant's message
// itself, and each message launched that interviewer's run.
func TestProposalModeRunsLaunchNoRuns(t *testing.T) {
	const refused = `{"error":"proposal-mode agent runs cannot launch agent runs"}`
	project := "proj-1"
	for _, route := range launchRoutes {
		for _, scope := range []struct {
			name    string
			project *string
		}{{"in the project", &project}, {"with no project", nil}} {
			t.Run(route.name+", a proposal-mode run "+scope.name, func(t *testing.T) {
				h, runs, chat := launchFixture()
				w := launchAs(route, h, asRunOf("agent-proposal", scope.project))
				if w.Code != http.StatusForbidden || strings.TrimSpace(w.Body.String()) != refused {
					t.Fatalf("status = %d, body %q: want 403 %s", w.Code, w.Body.String(), refused)
				}
				if len(runs.launchReqs) != 0 || len(runs.retryCalls) != 0 {
					t.Fatalf("a proposal-mode run launched %d runs and retried %d", len(runs.launchReqs), len(runs.retryCalls))
				}
				if len(chat.messages) != 0 {
					t.Fatalf("a proposal-mode run's chat message was kept: %q", chat.messages)
				}
				if iv := h.interviewService.(*launchInterviews); len(iv.created)+len(iv.invites) != 0 {
					t.Fatalf("a proposal-mode run created interviews %q and invites %q", iv.created, iv.invites)
				}
			})
		}
	}
}

// TestARunsLaunchRecordsItsParent is the regression test for a launch by a
// run's token that the run tree did not show: the new run carried no
// launcher and no parent, so nothing tied it to the run that set it going. A
// direct-mode run's launch, of an agent, a crew, a test run's agent, the
// draft of test cases or an assistant turn, now records the launching run as
// its parent (ParentRunID, as a delegation does); a person's launch records
// none. The retry and run-now take a person's session, and still answer a
// run 401; a direct-mode run still creates an interview and its invite,
// which launch nothing themselves.
func TestARunsLaunchRecordsItsParent(t *testing.T) {
	project := "proj-1"
	for _, route := range launchRoutes {
		t.Run(route.name, func(t *testing.T) {
			h, runs, _ := launchFixture()
			w := launchAs(route, h, asRunOf("agent-direct", &project))
			if route.arms {
				iv := h.interviewService.(*launchInterviews)
				if w.Code != http.StatusCreated || len(iv.created)+len(iv.invites) != 1 || len(runs.launchReqs) != 0 {
					t.Fatalf("status = %d, %d interviews, %d invites, %d launches: want 201, one made and no launch (body %q)",
						w.Code, len(iv.created), len(iv.invites), len(runs.launchReqs), w.Body.String())
				}
				return
			}
			if strings.HasSuffix(route.name, "/retry") || strings.HasSuffix(route.name, "/run-now") {
				if w.Code != http.StatusUnauthorized || len(runs.launchReqs)+len(runs.retryCalls) != 0 {
					t.Fatalf("status = %d, %d launches: want 401 and none (body %q)", w.Code, len(runs.launchReqs), w.Body.String())
				}
				return
			}
			if w.Code != http.StatusCreated && w.Code != http.StatusOK {
				t.Fatalf("status = %d, body %q: want the launch to pass", w.Code, w.Body.String())
			}
			if len(runs.launchReqs) != 1 {
				t.Fatalf("launches = %d, want 1 (body %q)", len(runs.launchReqs), w.Body.String())
			}
			if parent := runs.launchReqs[0].ParentRunID; parent == nil || *parent != "run-launching" {
				t.Fatalf("the launched run's parent = %v, want the launching run run-launching", parent)
			}
		})
	}

	t.Run("POST /api/v1/projects/{id}/draft-test-cases", func(t *testing.T) {
		h, runs := newDraftFixture()
		h.agentService.(*fakeAgentService).byID["agent-direct"] = &agents.Agent{ID: "agent-direct", OrgID: "org-1",
			Slug: "helper", WriteMode: agents.WriteModeDirect}
		pid := "proj-1"
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"requirement_ids":["`+draftReqUUID1+`"]}`))
		ctx := context.WithValue(r.Context(), ctxRun, &agentruns.Run{ID: "run-launching", OrgID: "org-1",
			AgentID: "agent-direct", ProjectID: &pid})
		w := httptest.NewRecorder()
		h.DraftTestCases(w, mux.SetURLVars(r.WithContext(ctx), map[string]string{"id": "proj-1"}))
		if w.Code != http.StatusCreated || len(runs.launchReqs) != 1 {
			t.Fatalf("status = %d, %d launches: want 201 and one (body %q)", w.Code, len(runs.launchReqs), w.Body.String())
		}
		if parent := runs.launchReqs[0].ParentRunID; parent == nil || *parent != "run-launching" {
			t.Fatalf("the drafting run's parent = %v, want run-launching", parent)
		}
	})

	t.Run("a person's launch records no parent", func(t *testing.T) {
		h, runs, _ := launchFixture()
		w := launchAs(launchRoutes[0], h, asPerson("editor", "org-1"))
		if w.Code != http.StatusCreated || len(runs.launchReqs) != 1 {
			t.Fatalf("status = %d, %d launches: want 201 and one (body %q)", w.Code, len(runs.launchReqs), w.Body.String())
		}
		if parent := runs.launchReqs[0].ParentRunID; parent != nil {
			t.Fatalf("a person's launch recorded the parent %q", *parent)
		}
	})
}

// TestALaunchWithNoProjectAsksTheWorkspace is the regression test for a
// launch that names no project, which asked nothing: LaunchAgentRun checked
// a project editor's role only when the body named a project, so a launch
// without one ran in the workspace with no membership asked, and passed a
// workspace read-only over its plan, where every other write is refused
// (REQ-176, REQ-177). A person must now be a member of the agent's
// workspace, and the read-only gate refuses the launch whoever sends it; a
// worker key and a direct-mode run's token launch there as before.
func TestALaunchWithNoProjectAsksTheWorkspace(t *testing.T) {
	noProject := launchRoutes[1]
	worker := func(ctx context.Context) context.Context { return context.WithValue(ctx, ctxWorkerOrg, "org-1") }

	// A workspace the person is no member of answers as one no row has (I3).
	t.Run("a person who is not a member of the workspace is refused", func(t *testing.T) {
		h, runs, _ := launchFixture()
		w := launchAs(noProject, h, asPerson("stranger", "org-1"))
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "workspace not found") {
			t.Fatalf("status = %d, body %q: want 404 workspace not found", w.Code, w.Body.String())
		}
		if len(runs.launchReqs) != 0 {
			t.Fatalf("a stranger launched %d runs", len(runs.launchReqs))
		}
	})

	for _, tc := range []struct {
		name string
		as   func(context.Context) context.Context
	}{
		{"a member", asPerson("member", "org-1")},
		{"W's worker key", worker},
		{"a direct-mode run's token", asRunOf("agent-direct", nil)},
	} {
		t.Run(tc.name+" launches in a writable workspace", func(t *testing.T) {
			h, runs, _ := launchFixture()
			w := launchAs(noProject, h, tc.as)
			if w.Code != http.StatusCreated || len(runs.launchReqs) != 1 || runs.launchReqs[0].OrgID != "org-1" {
				t.Fatalf("status = %d, %d launches: want 201 and one in org-1 (body %q)", w.Code, len(runs.launchReqs), w.Body.String())
			}
		})
		t.Run(tc.name+" is refused by a read-only workspace", func(t *testing.T) {
			enforceTiers(t)
			h, runs, _ := launchFixture()
			// Three members on the free tier's two seats: read-only.
			h.orgService.(*fakeOrgService).roles["org-1"]["third"] = orgs.RoleMember
			w := launchAs(noProject, h, tc.as)
			if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), `"code":"plan_read_only"`) {
				t.Fatalf("status = %d, body %q: want 403 plan_read_only", w.Code, w.Body.String())
			}
			if len(runs.launchReqs) != 0 {
				t.Fatalf("a read-only workspace took %d launches", len(runs.launchReqs))
			}
		})
	}
}

// treeRuns answers Tree with its runs, root first and each run after its
// parent, as agentruns.DefaultService's breadth-first walk does.
type treeRuns struct {
	*fakeRunService
	tree []*agentruns.Run
}

func (f treeRuns) Tree(rootID string) ([]*agentruns.Run, error) { return f.tree, nil }

// TestARunTreeShowsOnlyRunsItsReaderCanOpen is the regression test for a
// run tree that showed runs its reader could not open: since a launch by a
// run's token records the launching run as its parent, a project's run that
// launches with no project has an unscoped child, which only the
// workspace's admins may read (requireRunAccess), while GetAgentRunTree
// asked the root alone, so a viewer of the project read the child's prompt,
// answer and error in the tree and got 403 reading the child by itself
// (REQ-16). The tree now keeps a run below the root only when its reader
// could open it and its parent is kept, so no hidden run's id shows as a
// parent either. An unscoped run its reader launched stays, as its launcher
// opens it, beside a hidden one of the same workspace.
func TestARunTreeShowsOnlyRunsItsReaderCanOpen(t *testing.T) {
	project := "proj-1"
	parent := func(id string) *string { return &id }
	tree := []*agentruns.Run{
		{ID: "run-root", OrgID: "org-1", AgentID: "agent-direct", ProjectID: &project},
		{ID: "run-delegate", OrgID: "org-1", AgentID: "agent-direct", ProjectID: &project, ParentRunID: parent("run-root")},
		{ID: "run-unscoped", OrgID: "org-1", AgentID: "agent-direct", ParentRunID: parent("run-root"), Prompt: "Tidy the workspace."},
		{ID: "run-mine", OrgID: "org-1", AgentID: "agent-direct", ParentRunID: parent("run-root"), LaunchedBy: parent("viewer")},
		{ID: "run-beneath", OrgID: "org-1", AgentID: "agent-direct", ProjectID: &project, ParentRunID: parent("run-unscoped")},
	}
	for _, tc := range []struct {
		reader string
		want   string
	}{
		{"viewer", "run-root run-delegate run-mine"},
		{"wsadmin", "run-root run-delegate run-unscoped run-mine run-beneath"},
	} {
		t.Run(tc.reader, func(t *testing.T) {
			h, runs, _ := launchFixture()
			runs.byID["run-root"] = tree[0]
			h.runService = treeRuns{fakeRunService: runs, tree: tree}
			h.orgService.(*fakeOrgService).roles["org-1"]["viewer"] = orgs.RoleMember
			h.orgService.(*fakeOrgService).roles["org-1"]["wsadmin"] = orgs.RoleAdmin
			h.memberService.(*fakeMemberService).roles[project]["viewer"] = members.RoleViewer

			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r = mux.SetURLVars(r.WithContext(asPerson(tc.reader, "org-1")(r.Context())), map[string]string{"id": "run-root"})
			h.GetAgentRunTree(w, r)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, body %q: want 200", w.Code, w.Body.String())
			}
			var got []*agentruns.Run
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode %q: %v", w.Body.String(), err)
			}
			ids := make([]string, 0, len(got))
			for _, run := range got {
				ids = append(ids, run.ID)
			}
			if strings.Join(ids, " ") != tc.want {
				t.Fatalf("%s reads the tree %q, want %q", tc.reader, ids, tc.want)
			}
		})
	}
}
