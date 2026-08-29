package orchestration

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	domainevents "github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/guided"
	"github.com/openv/requirements-platform/internal/domain/interviews"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/teams"
	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// The fakes embed their service interface so only the methods the hooks
// actually touch need implementations; any unexpected call panics loudly,
// which doubles as an assertion that the hooks stay narrow.

type fakeRunService struct {
	agentruns.Service
	launches  []agentruns.LaunchRequest
	launchErr error
	listRuns  []*agentruns.Run
	listErr   error
	listCalls int
	attached  map[string]string // runID -> workItemID
}

func (f *fakeRunService) Launch(req agentruns.LaunchRequest) (*agentruns.Run, string, error) {
	if f.launchErr != nil {
		return nil, "", f.launchErr
	}
	f.launches = append(f.launches, req)
	return &agentruns.Run{ID: fmt.Sprintf("launched-%d", len(f.launches)), Status: agentruns.StatusQueued}, "token", nil
}

func (f *fakeRunService) List(filter agentruns.ListFilter) ([]*agentruns.Run, error) {
	f.listCalls++
	return f.listRuns, f.listErr
}

func (f *fakeRunService) AttachWorkItem(runID, workItemID string) error {
	if f.attached == nil {
		f.attached = map[string]string{}
	}
	f.attached[runID] = workItemID
	return nil
}

type moveCall struct {
	id    string
	req   workitems.MoveRequest
	actor string
}

type activityCall struct {
	workItemID string
	kind       string
	content    string
	actor      string
	payload    map[string]interface{}
}

type createCall struct {
	req   workitems.CreateWorkItemRequest
	actor string
}

type fakeWorkItemService struct {
	workitems.Service
	creates    []createCall
	createErr  error
	moves      []moveCall
	moveErr    error
	activities []activityCall
	items      map[string]*workitems.WorkItem
	getErr     error
	getCalls   int
}

func (f *fakeWorkItemService) Create(req workitems.CreateWorkItemRequest, createdBy *string, actor string) (*workitems.WorkItem, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.creates = append(f.creates, createCall{req: req, actor: actor})
	return &workitems.WorkItem{
		ID:           fmt.Sprintf("created-%d", len(f.creates)),
		ProjectID:    req.ProjectID,
		Title:        req.Title,
		Description:  req.Description,
		Column:       req.Column,
		AssigneeType: req.AssigneeType,
		AssigneeID:   req.AssigneeID,
	}, nil
}

func (f *fakeWorkItemService) Get(id string) (*workitems.WorkItem, error) {
	f.getCalls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.items[id], nil
}

func (f *fakeWorkItemService) Move(id string, req workitems.MoveRequest, actor string) (*workitems.WorkItem, error) {
	if f.moveErr != nil {
		return nil, f.moveErr
	}
	f.moves = append(f.moves, moveCall{id: id, req: req, actor: actor})
	return &workitems.WorkItem{ID: id, Column: req.Column}, nil
}

func (f *fakeWorkItemService) RecordRunActivity(workItemID, kind, content, actor string, payload map[string]interface{}) error {
	f.activities = append(f.activities, activityCall{workItemID: workItemID, kind: kind, content: content, actor: actor, payload: payload})
	return nil
}

type fakeTeamService struct {
	teams.Service
	edges    map[string][]*teams.Edge // "<nodeID>|<edgeType>"
	edgesErr error
	graph    *teams.TeamGraph
	graphErr error
}

func (f *fakeTeamService) SuccessorEdges(nodeID string, edgeType string) ([]*teams.Edge, error) {
	if f.edgesErr != nil {
		return nil, f.edgesErr
	}
	return f.edges[nodeID+"|"+edgeType], nil
}

func (f *fakeTeamService) GetTeam(id string) (*teams.TeamGraph, error) {
	if f.graphErr != nil {
		return nil, f.graphErr
	}
	return f.graph, nil
}

type appendCall struct {
	sessionID string
	role      string
	content   string
}

type fakeInterviewService struct {
	interviews.Service
	appends   []appendCall
	appendErr error
}

func (f *fakeInterviewService) AppendMessage(sessionID, role, content string) (*interviews.Message, error) {
	if f.appendErr != nil {
		return nil, f.appendErr
	}
	f.appends = append(f.appends, appendCall{sessionID, role, content})
	return &interviews.Message{ID: "m1", SessionID: sessionID, Role: role, Content: content}, nil
}

type fakeGuidedService struct {
	guided.Service
	appends   []appendCall
	appendErr error
}

func (f *fakeGuidedService) AppendChatMessage(sessionID, role, content string) (*guided.ChatMessage, error) {
	if f.appendErr != nil {
		return nil, f.appendErr
	}
	f.appends = append(f.appends, appendCall{sessionID, role, content})
	return &guided.ChatMessage{ID: "m1", SessionID: sessionID, Role: role, Content: content}, nil
}

type fakeProjectService struct {
	projects.Service
	projects map[string]*projects.Project
	getErr   error
}

func (f *fakeProjectService) GetProject(id string) (*projects.Project, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.projects[id], nil
}

type broadcastCall struct {
	key   string
	event string
	data  interface{}
}

type fakeBroadcaster struct {
	calls []broadcastCall
}

func (f *fakeBroadcaster) BroadcastSession(key string, event string, data interface{}) {
	f.calls = append(f.calls, broadcastCall{key, event, data})
}

type fixture struct {
	hooks       *Hooks
	runs        *fakeRunService
	teams       *fakeTeamService
	workItems   *fakeWorkItemService
	interviews  *fakeInterviewService
	guided      *fakeGuidedService
	projects    *fakeProjectService
	broadcaster *fakeBroadcaster
}

func newFixture() *fixture {
	f := &fixture{
		runs:        &fakeRunService{},
		teams:       &fakeTeamService{},
		workItems:   &fakeWorkItemService{items: map[string]*workitems.WorkItem{}},
		interviews:  &fakeInterviewService{},
		guided:      &fakeGuidedService{},
		projects:    &fakeProjectService{projects: map[string]*projects.Project{}},
		broadcaster: &fakeBroadcaster{},
	}
	f.hooks = NewHooks(f.runs, f.teams, f.workItems, f.interviews, f.guided, f.projects, f.broadcaster)
	return f
}

func strptr(s string) *string { return &s }

// --- Kanban column sync ---

func TestStatusChangeMovesTrackedCard(t *testing.T) {
	cases := []struct {
		status       string
		wantColumn   string
		wantActivity string // "" = no run activity recorded
	}{
		{agentruns.StatusQueued, workitems.ColumnTodo, ""},
		{agentruns.StatusClaimed, workitems.ColumnInProgress, ""},
		{agentruns.StatusRunning, workitems.ColumnInProgress, workitems.KindRunStarted},
		{agentruns.StatusAwaitingApproval, workitems.ColumnReview, workitems.KindRunFinished},
		{agentruns.StatusSucceeded, workitems.ColumnDone, workitems.KindRunFinished},
		{agentruns.StatusFailed, workitems.ColumnTodo, workitems.KindRunFailed},
		{agentruns.StatusCancelled, workitems.ColumnTodo, workitems.KindRunFailed},
		{agentruns.StatusTimedOut, workitems.ColumnTodo, workitems.KindRunFailed},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			f := newFixture()
			run := &agentruns.Run{ID: "r1", Status: tc.status, WorkItemID: strptr("wi-9")}

			f.hooks.RunStatusChanged(run)

			if len(f.workItems.moves) != 1 {
				t.Fatalf("moves = %d, want 1", len(f.workItems.moves))
			}
			move := f.workItems.moves[0]
			if move.id != "wi-9" || move.req.Column != tc.wantColumn {
				t.Errorf("moved %s to %q, want wi-9 to %q", move.id, move.req.Column, tc.wantColumn)
			}
			if move.actor != "agent:r1" {
				t.Errorf("move actor = %q, want agent:r1 (the loop guard depends on this prefix)", move.actor)
			}
			if tc.wantActivity == "" {
				if len(f.workItems.activities) != 0 {
					t.Errorf("unexpected activity %+v", f.workItems.activities)
				}
				return
			}
			if len(f.workItems.activities) != 1 {
				t.Fatalf("activities = %d, want 1", len(f.workItems.activities))
			}
			act := f.workItems.activities[0]
			if act.kind != tc.wantActivity || act.workItemID != "wi-9" || act.actor != "agent:r1" {
				t.Errorf("activity = %+v, want kind %q on wi-9 by agent:r1", act, tc.wantActivity)
			}
			if act.payload["run_id"] != "r1" {
				t.Errorf("activity payload run_id = %v, want r1", act.payload["run_id"])
			}
		})
	}
}

func TestStatusChangeUnknownStatusDoesNothing(t *testing.T) {
	f := newFixture()
	f.hooks.RunStatusChanged(&agentruns.Run{ID: "r1", Status: "weird", WorkItemID: strptr("wi-9")})
	if len(f.workItems.moves) != 0 || len(f.workItems.activities) != 0 {
		t.Errorf("unknown status must not touch the board: moves=%v activities=%v", f.workItems.moves, f.workItems.activities)
	}
}

func TestQueuedRootRunAutoCreatesTrackingCard(t *testing.T) {
	f := newFixture()
	longPrompt := strings.Repeat("p", 400)
	run := &agentruns.Run{
		ID:        "r1",
		AgentID:   "agent-1",
		AgentName: "Builder",
		Status:    agentruns.StatusQueued,
		ProjectID: strptr("p1"),
		Prompt:    longPrompt,
	}

	f.hooks.RunStatusChanged(run)

	if len(f.workItems.creates) != 1 {
		t.Fatalf("creates = %d, want 1", len(f.workItems.creates))
	}
	c := f.workItems.creates[0]
	if c.req.Title != "Agent run: Builder" {
		t.Errorf("title = %q", c.req.Title)
	}
	if c.req.ProjectID != "p1" || c.req.Column != workitems.ColumnTodo {
		t.Errorf("card = %+v, want project p1 in todo", c.req)
	}
	if c.req.AssigneeType != workitems.AssigneeAgent || c.req.AssigneeID == nil || *c.req.AssigneeID != "agent-1" {
		t.Errorf("assignee = %s/%v, want agent/agent-1", c.req.AssigneeType, c.req.AssigneeID)
	}
	if want := agentruns.Truncate(longPrompt, 300); c.req.Description != want {
		t.Errorf("description not truncated to 300: len=%d", len(c.req.Description))
	}
	if c.actor != "agent:r1" {
		t.Errorf("create actor = %q, want agent:r1", c.actor)
	}
	if got := f.runs.attached["r1"]; got != "created-1" {
		t.Errorf("AttachWorkItem = %q, want created-1", got)
	}
	if run.WorkItemID == nil || *run.WorkItemID != "created-1" {
		t.Errorf("run.WorkItemID = %v, want created-1", run.WorkItemID)
	}
	if len(f.workItems.activities) != 1 || f.workItems.activities[0].kind != workitems.KindRunStarted || f.workItems.activities[0].content != "Run queued" {
		t.Errorf("activities = %+v, want one run-started 'Run queued'", f.workItems.activities)
	}
	if len(f.workItems.moves) != 0 {
		t.Errorf("fresh card must not also be moved: %+v", f.workItems.moves)
	}
}

func TestQueuedRunWithoutNameGetsGenericCardTitle(t *testing.T) {
	f := newFixture()
	f.hooks.RunStatusChanged(&agentruns.Run{ID: "r1", AgentID: "a1", Status: agentruns.StatusQueued, ProjectID: strptr("p1")})
	if len(f.workItems.creates) != 1 || f.workItems.creates[0].req.Title != "Agent run" {
		t.Fatalf("creates = %+v, want generic 'Agent run' title", f.workItems.creates)
	}
}

func TestNoTrackingCardForNonRootOrSessionRuns(t *testing.T) {
	base := func() *agentruns.Run {
		return &agentruns.Run{ID: "r1", AgentID: "a1", Status: agentruns.StatusQueued, ProjectID: strptr("p1")}
	}
	cases := []struct {
		name   string
		mutate func(*agentruns.Run)
	}{
		{"child run", func(r *agentruns.Run) { r.ParentRunID = strptr("parent") }},
		{"interview run", func(r *agentruns.Run) { r.InterviewSessionID = strptr("iv") }},
		{"guided run", func(r *agentruns.Run) { r.GuidedSessionID = strptr("gs") }},
		{"no project", func(r *agentruns.Run) { r.ProjectID = nil }},
		{"not queued", func(r *agentruns.Run) { r.Status = agentruns.StatusRunning }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture()
			run := base()
			tc.mutate(run)
			f.hooks.RunStatusChanged(run)
			if len(f.workItems.creates) != 0 {
				t.Errorf("card created for %s: %+v", tc.name, f.workItems.creates)
			}
			if run.WorkItemID != nil {
				t.Errorf("run gained a work item: %v", *run.WorkItemID)
			}
		})
	}
}

func TestCardCreateFailureSkipsAttach(t *testing.T) {
	f := newFixture()
	f.workItems.createErr = errors.New("db down")
	f.hooks.RunStatusChanged(&agentruns.Run{ID: "r1", AgentID: "a1", Status: agentruns.StatusQueued, ProjectID: strptr("p1")})
	if len(f.runs.attached) != 0 {
		t.Errorf("attach must not run after create failure: %v", f.runs.attached)
	}
	if len(f.workItems.activities) != 0 {
		t.Errorf("no activity should be recorded after create failure: %+v", f.workItems.activities)
	}
}

// --- Successor / handoff launches ---

func agentNode(id, agentID, label string) *teams.Node {
	return &teams.Node{ID: id, NodeType: teams.NodeAgent, AgentID: agentID, Label: label}
}

func humanNode(id, userID, label string) *teams.Node {
	n := &teams.Node{ID: id, NodeType: teams.NodeHuman, Label: label}
	if userID != "" {
		n.UserID = &userID
	}
	return n
}

func teamRun(status string) *agentruns.Run {
	return &agentruns.Run{
		ID:         "r1",
		OrgID:      "org-1",
		AgentName:  "Planner",
		Status:     status,
		TeamID:     strptr("t1"),
		TeamNodeID: strptr("n1"),
		ProjectID:  strptr("p1"),
		WorkItemID: strptr("wi-1"),
		FinalText:  "the plan is done",
	}
}

func TestSucceededRunLaunchesHandsOffAndReviewSuccessors(t *testing.T) {
	f := newFixture()
	f.teams.graph = &teams.TeamGraph{Nodes: []*teams.Node{
		agentNode("n1", "agent-1", "Planner"),
		agentNode("n2", "agent-2", "Builder"),
		agentNode("n3", "agent-3", "Reviewer"),
	}}
	f.teams.edges = map[string][]*teams.Edge{
		"n1|" + teams.EdgeHandsOff: {{ID: "e1", TeamID: "t1", FromNodeID: "n1", ToNodeID: "n2", EdgeType: teams.EdgeHandsOff, Config: map[string]interface{}{}}},
		"n1|" + teams.EdgeReviews:  {{ID: "e2", TeamID: "t1", FromNodeID: "n1", ToNodeID: "n3", EdgeType: teams.EdgeReviews, Config: map[string]interface{}{}}},
	}

	f.hooks.RunStatusChanged(teamRun(agentruns.StatusSucceeded))

	if len(f.runs.launches) != 2 {
		t.Fatalf("launches = %d, want 2 (hands-off + review)", len(f.runs.launches))
	}

	handoff := f.runs.launches[0]
	if handoff.AgentID != "agent-2" {
		t.Errorf("hands-off target agent = %q, want agent-2", handoff.AgentID)
	}
	if handoff.OrgID != "org-1" {
		t.Errorf("successor OrgID = %q, want the parent run's org", handoff.OrgID)
	}
	if handoff.ParentRunID == nil || *handoff.ParentRunID != "r1" {
		t.Errorf("ParentRunID = %v, want r1", handoff.ParentRunID)
	}
	if handoff.TeamNodeID == nil || *handoff.TeamNodeID != "n2" {
		t.Errorf("TeamNodeID = %v, want n2", handoff.TeamNodeID)
	}
	if handoff.Priority != agentruns.PriorityChild {
		t.Errorf("Priority = %d, want PriorityChild", handoff.Priority)
	}
	if handoff.WorkItemID == nil || *handoff.WorkItemID != "wi-1" {
		t.Errorf("WorkItemID = %v, want carried over", handoff.WorkItemID)
	}
	if !strings.Contains(handoff.Prompt, "the plan is done") || !strings.Contains(handoff.Prompt, "r1") {
		t.Errorf("hands-off prompt missing output or run id: %q", handoff.Prompt)
	}
	if !strings.Contains(handoff.Prompt, "teammate finished") {
		t.Errorf("hands-off prompt should use the continuation wording: %q", handoff.Prompt)
	}

	review := f.runs.launches[1]
	if review.AgentID != "agent-3" {
		t.Errorf("review target agent = %q, want agent-3", review.AgentID)
	}
	if !strings.Contains(review.Prompt, "reviewing the output") {
		t.Errorf("review prompt should use the review wording: %q", review.Prompt)
	}
}

// TestAwaitingApprovalHoldsSuccessors locks in the approval gate: a run whose
// writes are still pending review must NOT launch crew successors/handoffs —
// a teammate would otherwise build on writes that may yet be rejected. The
// successors fire later, when the resolved run transitions to succeeded.
func TestAwaitingApprovalHoldsSuccessors(t *testing.T) {
	f := newFixture()
	f.teams.graph = &teams.TeamGraph{Nodes: []*teams.Node{agentNode("n1", "agent-1", "Planner"), agentNode("n2", "agent-2", "Builder")}}
	f.teams.edges = map[string][]*teams.Edge{
		"n1|" + teams.EdgeHandsOff: {{ID: "e1", TeamID: "t1", FromNodeID: "n1", ToNodeID: "n2", EdgeType: teams.EdgeHandsOff, Config: map[string]interface{}{}}},
	}
	f.hooks.RunStatusChanged(teamRun(agentruns.StatusAwaitingApproval))
	if len(f.runs.launches) != 0 {
		t.Fatalf("launches = %d, want 0 while proposals are unapproved", len(f.runs.launches))
	}

	// Once approval resolves the run to succeeded, the successor launches.
	f.hooks.RunStatusChanged(teamRun(agentruns.StatusSucceeded))
	if len(f.runs.launches) != 1 {
		t.Fatalf("launches after approval = %d, want 1", len(f.runs.launches))
	}
}

// TestAwaitingApprovalStillDeliversConversationalReply keeps interview/guided
// sessions unblocked: the reply is delivered when the answer is ready even
// though the run's writes are still awaiting review.
func TestAwaitingApprovalStillDeliversConversationalReply(t *testing.T) {
	f := newFixture()
	run := &agentruns.Run{ID: "r1", Status: agentruns.StatusAwaitingApproval, GuidedSessionID: strptr("g1"), FinalText: "here is the draft"}
	f.hooks.RunStatusChanged(run)
	if len(f.guided.appends) != 1 || f.guided.appends[0].content != "here is the draft" {
		t.Fatalf("guided reply not delivered on awaiting_approval: %+v", f.guided.appends)
	}
}

func TestFailedRunLaunchesNoSuccessors(t *testing.T) {
	f := newFixture()
	f.teams.graph = &teams.TeamGraph{Nodes: []*teams.Node{agentNode("n1", "agent-1", "Planner"), agentNode("n2", "agent-2", "Builder")}}
	f.teams.edges = map[string][]*teams.Edge{
		"n1|" + teams.EdgeHandsOff: {{ID: "e1", TeamID: "t1", FromNodeID: "n1", ToNodeID: "n2", EdgeType: teams.EdgeHandsOff, Config: map[string]interface{}{}}},
	}
	for _, status := range []string{agentruns.StatusFailed, agentruns.StatusTimedOut, agentruns.StatusCancelled} {
		f.hooks.RunStatusChanged(teamRun(status))
	}
	if len(f.runs.launches) != 0 {
		t.Fatalf("launches = %d, want 0 for failed/timed-out/cancelled runs", len(f.runs.launches))
	}
}

func TestRunWithoutTeamNodeLaunchesNothing(t *testing.T) {
	f := newFixture()
	run := teamRun(agentruns.StatusSucceeded)
	run.TeamNodeID = nil
	f.hooks.RunStatusChanged(run)
	if len(f.runs.launches) != 0 {
		t.Fatalf("launches = %d, want 0 without a team node", len(f.runs.launches))
	}
}

func TestSuccessorPromptTemplateOverridesDefault(t *testing.T) {
	f := newFixture()
	f.teams.graph = &teams.TeamGraph{Nodes: []*teams.Node{agentNode("n1", "agent-1", "Planner"), agentNode("n2", "agent-2", "Builder")}}
	f.teams.edges = map[string][]*teams.Edge{
		"n1|" + teams.EdgeHandsOff: {{
			ID: "e1", TeamID: "t1", FromNodeID: "n1", ToNodeID: "n2", EdgeType: teams.EdgeHandsOff,
			Config: map[string]interface{}{"prompt_template": "Ship {{handoff.output}} from {{handoff.run_id}}"},
		}},
	}
	f.hooks.RunStatusChanged(teamRun(agentruns.StatusSucceeded))
	if len(f.runs.launches) != 1 {
		t.Fatalf("launches = %d, want 1", len(f.runs.launches))
	}
	if got := f.runs.launches[0].Prompt; got != "Ship the plan is done from r1" {
		t.Errorf("templated prompt = %q", got)
	}
}

func TestSuccessorEdgeToMissingNodeIsSkipped(t *testing.T) {
	f := newFixture()
	f.teams.graph = &teams.TeamGraph{Nodes: []*teams.Node{agentNode("n1", "agent-1", "Planner")}}
	f.teams.edges = map[string][]*teams.Edge{
		"n1|" + teams.EdgeHandsOff: {{ID: "e1", TeamID: "t1", FromNodeID: "n1", ToNodeID: "gone", EdgeType: teams.EdgeHandsOff, Config: map[string]interface{}{}}},
	}
	f.hooks.RunStatusChanged(teamRun(agentruns.StatusSucceeded))
	if len(f.runs.launches) != 0 {
		t.Fatalf("launches = %d, want 0 when the target node is gone", len(f.runs.launches))
	}
}

// --- Human handoff cards ---

func TestHandoffToHumanCreatesCardInsteadOfRun(t *testing.T) {
	f := newFixture()
	f.teams.graph = &teams.TeamGraph{Nodes: []*teams.Node{
		agentNode("n1", "agent-1", "Planner"),
		humanNode("n2", "user-7", "Dana"),
	}}
	f.teams.edges = map[string][]*teams.Edge{
		"n1|" + teams.EdgeHandsOff: {{ID: "e1", TeamID: "t1", FromNodeID: "n1", ToNodeID: "n2", EdgeType: teams.EdgeHandsOff, Config: map[string]interface{}{}}},
	}

	f.hooks.RunStatusChanged(teamRun(agentruns.StatusSucceeded))

	if len(f.runs.launches) != 0 {
		t.Fatalf("human targets must never get agent runs, got %d launches", len(f.runs.launches))
	}
	if len(f.workItems.creates) != 1 {
		t.Fatalf("creates = %d, want 1 handoff card", len(f.workItems.creates))
	}
	c := f.workItems.creates[0]
	if c.req.Title != "Handoff from Planner" {
		t.Errorf("title = %q, want 'Handoff from Planner' (source node label)", c.req.Title)
	}
	if c.req.ProjectID != "p1" || c.req.Column != workitems.ColumnTodo {
		t.Errorf("card = %+v, want p1/todo", c.req)
	}
	if c.req.AssigneeType != workitems.AssigneeUser || c.req.AssigneeID == nil || *c.req.AssigneeID != "user-7" {
		t.Errorf("assignee = %s/%v, want user/user-7", c.req.AssigneeType, c.req.AssigneeID)
	}
	if !strings.Contains(c.req.Description, "the plan is done") || !strings.Contains(c.req.Description, "(from agent run r1)") {
		t.Errorf("description = %q, want run output plus provenance", c.req.Description)
	}
	// The source run's own card gets a finished note pointing at the new card
	// (alongside the regular status-sync activity).
	var handoffNotes []activityCall
	for _, act := range f.workItems.activities {
		if act.payload["work_item_id"] != nil {
			handoffNotes = append(handoffNotes, act)
		}
	}
	if len(handoffNotes) != 1 {
		t.Fatalf("handoff notes = %d, want 1; all activities: %+v", len(handoffNotes), f.workItems.activities)
	}
	act := handoffNotes[0]
	if act.workItemID != "wi-1" || act.kind != workitems.KindRunFinished || !strings.Contains(act.content, "Dana") {
		t.Errorf("activity = %+v, want run-finished 'Handed off to Dana' on wi-1", act)
	}
	if act.payload["work_item_id"] != "created-1" {
		t.Errorf("activity payload should link the handoff card: %+v", act.payload)
	}
}

func TestReviewHandoffToHumanTitlesAsReviewRequest(t *testing.T) {
	f := newFixture()
	f.teams.graph = &teams.TeamGraph{Nodes: []*teams.Node{
		agentNode("n1", "agent-1", "Planner"),
		humanNode("n2", "user-7", "Dana"),
	}}
	f.teams.edges = map[string][]*teams.Edge{
		"n1|" + teams.EdgeReviews: {{ID: "e1", TeamID: "t1", FromNodeID: "n1", ToNodeID: "n2", EdgeType: teams.EdgeReviews, Config: map[string]interface{}{}}},
	}
	f.hooks.RunStatusChanged(teamRun(agentruns.StatusSucceeded))
	if len(f.workItems.creates) != 1 || f.workItems.creates[0].req.Title != "Review request: Planner" {
		t.Fatalf("creates = %+v, want one 'Review request: Planner' card", f.workItems.creates)
	}
}

func TestHandoffToHumanSkippedWithoutProjectOrUser(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*fixture, *agentruns.Run)
	}{
		{"run without project", func(f *fixture, r *agentruns.Run) { r.ProjectID = nil }},
		{"human node without user", func(f *fixture, r *agentruns.Run) {
			f.teams.graph.Nodes[1] = humanNode("n2", "", "Dana")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture()
			f.teams.graph = &teams.TeamGraph{Nodes: []*teams.Node{
				agentNode("n1", "agent-1", "Planner"),
				humanNode("n2", "user-7", "Dana"),
			}}
			f.teams.edges = map[string][]*teams.Edge{
				"n1|" + teams.EdgeHandsOff: {{ID: "e1", TeamID: "t1", FromNodeID: "n1", ToNodeID: "n2", EdgeType: teams.EdgeHandsOff, Config: map[string]interface{}{}}},
			}
			run := teamRun(agentruns.StatusSucceeded)
			tc.mutate(f, run)
			f.hooks.RunStatusChanged(run)
			if len(f.workItems.creates) != 0 || len(f.runs.launches) != 0 {
				t.Errorf("nothing should happen: creates=%+v launches=%+v", f.workItems.creates, f.runs.launches)
			}
		})
	}
}

// --- Interview / guided reply delivery ---

func TestInterviewReplyDelivery(t *testing.T) {
	f := newFixture()
	run := &agentruns.Run{ID: "r1", Status: agentruns.StatusSucceeded, InterviewSessionID: strptr("iv-1"), FinalText: "  Tell me more.  "}

	f.hooks.RunStatusChanged(run)

	if len(f.interviews.appends) != 1 {
		t.Fatalf("appends = %d, want 1", len(f.interviews.appends))
	}
	msg := f.interviews.appends[0]
	if msg.sessionID != "iv-1" || msg.role != interviews.RoleAssistant || msg.content != "Tell me more." {
		t.Errorf("append = %+v, want trimmed assistant reply on iv-1", msg)
	}
	if len(f.broadcaster.calls) != 1 {
		t.Fatalf("broadcasts = %d, want 1", len(f.broadcaster.calls))
	}
	b := f.broadcaster.calls[0]
	if b.key != "interview:iv-1" || b.event != "message" {
		t.Errorf("broadcast = %+v, want interview:iv-1 message", b)
	}
}

func TestInterviewReplyEmptyFinalTextGetsPlaceholder(t *testing.T) {
	f := newFixture()
	f.hooks.RunStatusChanged(&agentruns.Run{ID: "r1", Status: agentruns.StatusSucceeded, InterviewSessionID: strptr("iv-1"), FinalText: "   "})
	if len(f.interviews.appends) != 1 || !strings.Contains(f.interviews.appends[0].content, "nothing further to add") {
		t.Fatalf("appends = %+v, want placeholder reply", f.interviews.appends)
	}
}

func TestInterviewFailureDeliversSystemMessage(t *testing.T) {
	for _, status := range []string{agentruns.StatusFailed, agentruns.StatusTimedOut} {
		f := newFixture()
		f.hooks.RunStatusChanged(&agentruns.Run{ID: "r1", Status: status, InterviewSessionID: strptr("iv-1")})
		if len(f.interviews.appends) != 1 {
			t.Fatalf("%s: appends = %d, want 1", status, len(f.interviews.appends))
		}
		msg := f.interviews.appends[0]
		if msg.role != interviews.RoleSystem || !strings.Contains(msg.content, "technical problem") {
			t.Errorf("%s: append = %+v, want system apology", status, msg)
		}
		if len(f.broadcaster.calls) != 1 || f.broadcaster.calls[0].key != "interview:iv-1" {
			t.Errorf("%s: broadcasts = %+v", status, f.broadcaster.calls)
		}
	}
}

func TestInterviewAppendFailureSkipsBroadcast(t *testing.T) {
	f := newFixture()
	f.interviews.appendErr = errors.New("session gone")
	f.hooks.RunStatusChanged(&agentruns.Run{ID: "r1", Status: agentruns.StatusSucceeded, InterviewSessionID: strptr("iv-1"), FinalText: "hi"})
	if len(f.broadcaster.calls) != 0 {
		t.Errorf("broadcast after failed append: %+v", f.broadcaster.calls)
	}
}

func TestGuidedReplyDelivery(t *testing.T) {
	f := newFixture()
	f.hooks.RunStatusChanged(&agentruns.Run{ID: "r1", Status: agentruns.StatusSucceeded, GuidedSessionID: strptr("gs-1"), FinalText: "Here is a suggestion."})
	if len(f.guided.appends) != 1 {
		t.Fatalf("appends = %d, want 1", len(f.guided.appends))
	}
	msg := f.guided.appends[0]
	if msg.sessionID != "gs-1" || msg.role != guided.ChatRoleAssistant || msg.content != "Here is a suggestion." {
		t.Errorf("append = %+v", msg)
	}
	if len(f.broadcaster.calls) != 1 || f.broadcaster.calls[0].key != "guided:gs-1" || f.broadcaster.calls[0].event != "message" {
		t.Errorf("broadcast = %+v, want guided:gs-1 message", f.broadcaster.calls)
	}
	if len(f.interviews.appends) != 0 {
		t.Errorf("guided runs must not touch interview sessions")
	}
}

func TestGuidedFailureDeliversSystemMessage(t *testing.T) {
	f := newFixture()
	f.hooks.RunStatusChanged(&agentruns.Run{ID: "r1", Status: agentruns.StatusFailed, GuidedSessionID: strptr("gs-1")})
	if len(f.guided.appends) != 1 || f.guided.appends[0].role != guided.ChatRoleSystem {
		t.Fatalf("appends = %+v, want one system message", f.guided.appends)
	}
}

func TestNilBroadcasterDoesNotPanic(t *testing.T) {
	f := newFixture()
	f.hooks = NewHooks(f.runs, f.teams, f.workItems, f.interviews, f.guided, f.projects, nil)
	f.hooks.RunStatusChanged(&agentruns.Run{ID: "r1", Status: agentruns.StatusSucceeded, InterviewSessionID: strptr("iv-1"), FinalText: "x"})
	f.hooks.RunStatusChanged(&agentruns.Run{ID: "r2", Status: agentruns.StatusFailed, GuidedSessionID: strptr("gs-1")})
	if len(f.interviews.appends) != 1 || len(f.guided.appends) != 1 {
		t.Errorf("messages should still be persisted without a broadcaster")
	}
}

// --- Board-drives-AI trigger and its loop guard ---

func boardMoveEvent(actor string) domainevents.Event {
	return domainevents.Event{
		EventType: domainevents.WorkItemMoved,
		EntityID:  "wi-1",
		Actor:     actor,
		Payload: map[string]interface{}{
			"column":        workitems.ColumnTodo,
			"assignee_type": workitems.AssigneeAgent,
			"assignee_id":   "agent-1",
		},
	}
}

func triggerFixture() *fixture {
	f := newFixture()
	f.workItems.items["wi-1"] = &workitems.WorkItem{
		ID:           "wi-1",
		ProjectID:    "p1",
		Title:        "Implement widget",
		Description:  "Make the widget spin",
		Column:       workitems.ColumnTodo,
		AssigneeType: workitems.AssigneeAgent,
		AssigneeID:   strptr("agent-1"),
		ArtifactIDs:  []string{"art-1", "art-2"},
	}
	f.projects.projects["p1"] = &projects.Project{ID: "p1", OrgID: "org-1"}
	return f
}

func TestBoardMoveByUserLaunchesRun(t *testing.T) {
	f := triggerFixture()

	f.hooks.onEvent(boardMoveEvent("user:u1"))

	if len(f.runs.launches) != 1 {
		t.Fatalf("launches = %d, want 1", len(f.runs.launches))
	}
	launch := f.runs.launches[0]
	if launch.OrgID != "org-1" {
		t.Errorf("OrgID = %q, want org-1 (resolved from the card's project)", launch.OrgID)
	}
	if launch.AgentID != "agent-1" {
		t.Errorf("AgentID = %q, want agent-1", launch.AgentID)
	}
	if launch.ProjectID == nil || *launch.ProjectID != "p1" {
		t.Errorf("ProjectID = %v, want p1", launch.ProjectID)
	}
	if launch.WorkItemID == nil || *launch.WorkItemID != "wi-1" {
		t.Errorf("WorkItemID = %v, want wi-1", launch.WorkItemID)
	}
	for _, want := range []string{"Implement widget", "Make the widget spin", "wi-1", "art-1, art-2", "get_work_item"} {
		if !strings.Contains(launch.Prompt, want) {
			t.Errorf("prompt missing %q: %q", want, launch.Prompt)
		}
	}
	if len(f.workItems.activities) != 1 {
		t.Fatalf("activities = %d, want 1", len(f.workItems.activities))
	}
	act := f.workItems.activities[0]
	if act.kind != workitems.KindRunStarted || act.actor != "system" {
		t.Errorf("activity = %+v, want system run-started", act)
	}
	if act.payload["run_id"] != "launched-1" {
		t.Errorf("activity payload = %+v, want the launched run id", act.payload)
	}
}

// TestBoardMoveByAgentDoesNotRelaunch is the loop-guard regression test: the
// hooks themselves move cards as "agent:<run_id>" when a run's status
// changes, and that move publishes a WorkItemMoved event back onto the bus.
// If the actor-prefix guard ever breaks, agent moves relaunch runs, whose
// status changes move the card again — an infinite loop. Agent and system
// moves must be ignored before ANY lookup or launch happens.
func TestBoardMoveByAgentDoesNotRelaunch(t *testing.T) {
	for _, actor := range []string{"agent:r1", "system", "", "automation:a1", "user"} {
		t.Run("actor="+actor, func(t *testing.T) {
			f := triggerFixture()
			f.hooks.onEvent(boardMoveEvent(actor))
			if len(f.runs.launches) != 0 {
				t.Fatalf("actor %q launched a run — infinite loop guard broken", actor)
			}
			if f.workItems.getCalls != 0 {
				t.Errorf("actor %q should be rejected before any card lookup", actor)
			}
			if f.runs.listCalls != 0 {
				t.Errorf("actor %q should be rejected before any run listing", actor)
			}
		})
	}
}

// TestAgentMoveEventRoundTrip drives the guard end to end: the exact event a
// hook-initiated card move would publish (actor = the same "agent:"+run.ID
// prefix syncWorkItem uses) must not relaunch.
func TestAgentMoveEventRoundTrip(t *testing.T) {
	f := triggerFixture()
	run := &agentruns.Run{ID: "r-loop", Status: agentruns.StatusFailed, WorkItemID: strptr("wi-1")}
	f.hooks.RunStatusChanged(run) // moves the card back to todo as agent:r-loop
	if len(f.workItems.moves) != 1 {
		t.Fatalf("expected the failed run to move its card")
	}
	// The board service would publish WorkItemMoved with the move's actor.
	f.hooks.onEvent(domainevents.Event{
		EventType: domainevents.WorkItemMoved,
		EntityID:  "wi-1",
		Actor:     f.workItems.moves[0].actor,
		Payload:   map[string]interface{}{"column": workitems.ColumnTodo, "assignee_type": workitems.AssigneeAgent},
	})
	if len(f.runs.launches) != 0 {
		t.Fatal("agent-initiated card move relaunched a run: infinite loop")
	}
}

func TestBoardMoveIgnoresOtherEventTypes(t *testing.T) {
	f := triggerFixture()
	e := boardMoveEvent("user:u1")
	e.EventType = domainevents.WorkItemCreated
	f.hooks.onEvent(e)
	if len(f.runs.launches) != 0 || f.workItems.getCalls != 0 {
		t.Errorf("non-move events must be ignored")
	}
}

func TestBoardMoveIgnoresWrongColumnOrAssignee(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]interface{}
	}{
		{"non-todo column", map[string]interface{}{"column": workitems.ColumnDone, "assignee_type": workitems.AssigneeAgent}},
		{"user assignee", map[string]interface{}{"column": workitems.ColumnTodo, "assignee_type": workitems.AssigneeUser}},
		{"missing payload", map[string]interface{}{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := triggerFixture()
			e := boardMoveEvent("user:u1")
			e.Payload = tc.payload
			f.hooks.onEvent(e)
			if len(f.runs.launches) != 0 {
				t.Errorf("launched for %s", tc.name)
			}
		})
	}
}

func TestBoardMoveSkipsCardWithLiveRun(t *testing.T) {
	for _, status := range []string{agentruns.StatusQueued, agentruns.StatusClaimed, agentruns.StatusRunning} {
		t.Run(status, func(t *testing.T) {
			f := triggerFixture()
			f.runs.listRuns = []*agentruns.Run{{ID: "live", Status: status}}
			f.hooks.onEvent(boardMoveEvent("user:u1"))
			if len(f.runs.launches) != 0 {
				t.Errorf("launched a duplicate run while a %s run is attached", status)
			}
		})
	}
}

func TestBoardMoveLaunchesWhenAttachedRunsAreTerminal(t *testing.T) {
	f := triggerFixture()
	f.runs.listRuns = []*agentruns.Run{
		{ID: "old-1", Status: agentruns.StatusFailed},
		{ID: "old-2", Status: agentruns.StatusSucceeded},
	}
	f.hooks.onEvent(boardMoveEvent("user:u1"))
	if len(f.runs.launches) != 1 {
		t.Fatalf("launches = %d, want 1 (terminal history must not block relaunch)", len(f.runs.launches))
	}
}

func TestBoardMoveSkipsWhenCardOrAssigneeMissing(t *testing.T) {
	t.Run("card missing", func(t *testing.T) {
		f := triggerFixture()
		delete(f.workItems.items, "wi-1")
		f.hooks.onEvent(boardMoveEvent("user:u1"))
		if len(f.runs.launches) != 0 {
			t.Error("launched for a missing card")
		}
	})
	t.Run("no assignee id", func(t *testing.T) {
		f := triggerFixture()
		f.workItems.items["wi-1"].AssigneeID = nil
		f.hooks.onEvent(boardMoveEvent("user:u1"))
		if len(f.runs.launches) != 0 {
			t.Error("launched with no assignee")
		}
	})
}

func TestBoardMoveSkipsWhenOrgUnresolvable(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*fixture)
	}{
		{"project lookup error", func(f *fixture) { f.projects.getErr = errors.New("boom") }},
		{"project missing", func(f *fixture) { delete(f.projects.projects, "p1") }},
		{"project without org", func(f *fixture) { f.projects.projects["p1"].OrgID = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := triggerFixture()
			tc.mutate(f)
			f.hooks.onEvent(boardMoveEvent("user:u1"))
			if len(f.runs.launches) != 0 {
				t.Errorf("launched without a resolvable org (%s)", tc.name)
			}
		})
	}
}

func TestBoardMoveLaunchFailureRecordsCardActivity(t *testing.T) {
	f := triggerFixture()
	f.runs.launchErr = errors.New("no such agent")
	f.hooks.onEvent(boardMoveEvent("user:u1"))
	if len(f.workItems.activities) != 1 {
		t.Fatalf("activities = %d, want 1 failure note", len(f.workItems.activities))
	}
	act := f.workItems.activities[0]
	if act.kind != workitems.KindRunFailed || act.actor != "system" || !strings.Contains(act.content, "no such agent") {
		t.Errorf("activity = %+v, want system run-failed with the error", act)
	}
}
