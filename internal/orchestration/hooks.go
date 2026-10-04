package orchestration

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/automations"
	domainevents "github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/guided"
	"github.com/openv/requirements-platform/internal/domain/interviews"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/teams"
	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// SessionBroadcaster pushes events onto an SSE stream (implemented by api.SSEHub).
type SessionBroadcaster interface {
	BroadcastSession(key string, event string, data interface{})
}

// GuidedNudgeLauncher launches one copilot turn for a wizard nudge that was
// parked while a run was in flight. Implemented by the API handler, which
// owns prompt building; wired after construction (main.go) because the
// handler is built from these hooks' own services.
type GuidedNudgeLauncher interface {
	LaunchGuidedNudge(sessionID string, nudge guided.PendingNudge, launchedBy *string) error
}

// ProjectReach reports whether a person may be handed a card on a project's
// board, and if not, which rule refused them (Reach): an admin of the
// project's workspace, or a member of it with a role in the project,
// directly or through a people team, is allowed. That is stricter than what
// a person's own session opens, the safer choice for a card made on their
// behalf: a platform admin who is neither, or someone who has left the
// workspace but keeps a role in the project, is refused too. With an error
// it answers a refusal, never ReachAllowed. main.go wires it from the
// project, org and member services (handoffReach).
type ProjectReach func(projectID, userID string) (Reach, error)

// Reach is what a ProjectReach found of a person and a project: allowed, or
// the rule that refused them, which the hand-off's refusal names.
type Reach string

const (
	// ReachAllowed: an admin of the project's workspace, or a member of it
	// with a role in the project.
	ReachAllowed Reach = "allowed"
	// ReachNotInWorkspace: not a member of the project's workspace, such as
	// someone who has left it, whatever role in the project they kept.
	ReachNotInWorkspace Reach = "not_in_workspace"
	// ReachNoRole: a member of the project's workspace, not its admin, with
	// no role in the project.
	ReachNoRole Reach = "no_role"
)

// partialBroadcastInterval is the floor between two assistant_partial events
// for one run. The worker already batches at 750ms, but a retry, a second
// worker, or a future faster pump must not turn a reply into an SSE flood.
const partialBroadcastInterval = 500 * time.Millisecond

// Hooks wires run lifecycle changes to the kanban board, team follow-ups,
// and interview sessions, and lets kanban card moves drive agent runs.
// It implements agentruns.Subscriber and subscribes to the event bus.
type Hooks struct {
	runService       agentruns.Service
	teamService      teams.Service
	workItemService  workitems.Service
	interviewService interviews.Service
	guidedService    guided.Service
	projectService   projects.Service
	broadcaster      SessionBroadcaster
	nudgeLauncher    GuidedNudgeLauncher
	// reach decides who may be handed a card (handOffToHuman); nil refuses
	// every hand-off to a person.
	reach ProjectReach

	// Last assistant_partial broadcast per run, for the rate limit. Entries
	// are dropped when the run reaches a terminal state.
	partialMu   sync.Mutex
	lastPartial map[string]time.Time
}

// NewHooks creates the orchestration hooks. reach decides who a crew's
// hand-off card may go to.
func NewHooks(runService agentruns.Service, teamService teams.Service, workItemService workitems.Service, interviewService interviews.Service, guidedService guided.Service, projectService projects.Service, broadcaster SessionBroadcaster, reach ProjectReach) *Hooks {
	return &Hooks{
		runService:       runService,
		teamService:      teamService,
		workItemService:  workItemService,
		interviewService: interviewService,
		guidedService:    guidedService,
		projectService:   projectService,
		broadcaster:      broadcaster,
		reach:            reach,
		lastPartial:      map[string]time.Time{},
	}
}

// SetGuidedNudgeLauncher closes the construction cycle: coalesced wizard
// nudges are launched through the API handler once it exists. Without one,
// a parked nudge simply waits for the next thing the user does.
func (h *Hooks) SetGuidedNudgeLauncher(l GuidedNudgeLauncher) { h.nudgeLauncher = l }

// SubscribeBus attaches the board-drives-AI trigger.
func (h *Hooks) SubscribeBus(bus domainevents.Bus) {
	bus.Subscribe(h.onEvent)
}

// RunLogsAppended implements agentruns.Subscriber (no-op; the SSE hub
// handles log fan-out).
func (h *Hooks) RunLogsAppended(run *agentruns.Run, entries []agentruns.LogEntry) {}

// RunPartialText implements agentruns.Subscriber: a conversational run's
// answer-so-far goes to its session's chat channel as `assistant_partial`,
// so the panel can render the reply while it is being written. The final
// `message` event replaces whatever the last partial left on screen.
func (h *Hooks) RunPartialText(run *agentruns.Run, text string) {
	if h.broadcaster == nil || strings.TrimSpace(text) == "" {
		return
	}
	key := ""
	switch {
	case run.GuidedSessionID != nil:
		key = "guided:" + *run.GuidedSessionID
	case run.InterviewSessionID != nil:
		key = "interview:" + *run.InterviewSessionID
	default:
		// Not a conversation: the run's own log stream already carries it.
		return
	}
	if !h.allowPartial(run.ID) {
		return
	}
	h.broadcaster.BroadcastSession(key, "assistant_partial", map[string]interface{}{
		"run_id": run.ID,
		"text":   text,
	})
}

// allowPartial reports whether this run may broadcast now, and records the
// time when it may.
func (h *Hooks) allowPartial(runID string) bool {
	now := timeNow()
	h.partialMu.Lock()
	defer h.partialMu.Unlock()
	if last, ok := h.lastPartial[runID]; ok && now.Sub(last) < partialBroadcastInterval {
		return false
	}
	h.lastPartial[runID] = now
	return true
}

// forgetPartial drops a finished run's rate-limit bookkeeping.
func (h *Hooks) forgetPartial(runID string) {
	h.partialMu.Lock()
	delete(h.lastPartial, runID)
	h.partialMu.Unlock()
}

// timeNow is time.Now, indirected so tests can drive the partial rate limit.
var timeNow = time.Now

// RunStatusChanged implements agentruns.Subscriber.
func (h *Hooks) RunStatusChanged(run *agentruns.Run) {
	h.syncWorkItem(run)

	live := run.Status == agentruns.StatusQueued || run.Status == agentruns.StatusClaimed || run.Status == agentruns.StatusRunning
	if !live {
		h.forgetPartial(run.ID)
	}

	switch run.Status {
	case agentruns.StatusSucceeded:
		// Genuine success: launch crew successors/handoffs and deliver the
		// conversational reply. A run that went through approval reaches this
		// case only once its proposals are resolved (awaiting_approval ->
		// succeeded), so successors fire on the real outcome, not on unapproved
		// writes.
		h.enqueueSuccessors(run)
		h.deliverInterviewReply(run)
		h.deliverGuidedReply(run)
	case agentruns.StatusAwaitingApproval:
		// The answer is ready but its writes await human review. Deliver the
		// conversational reply so interview/guided sessions aren't left
		// hanging, but hold successors/handoffs until the approval resolves the
		// run to succeeded — otherwise a teammate would build on writes that
		// may still be rejected.
		h.deliverInterviewReply(run)
		h.deliverGuidedReply(run)
	case agentruns.StatusFailed, agentruns.StatusTimedOut:
		h.deliverInterviewFailure(run)
		h.deliverGuidedFailure(run)
	}

	// Last: the session is free again, so hand over the nudge that arrived
	// while this run held it. After the reply is delivered, so the new turn's
	// prompt contains the answer this run just gave.
	if !live {
		h.launchPendingNudge(run)
	}
}

// --- Kanban sync ---

var statusToColumn = map[string]string{
	agentruns.StatusQueued:           workitems.ColumnTodo,
	agentruns.StatusClaimed:          workitems.ColumnInProgress,
	agentruns.StatusRunning:          workitems.ColumnInProgress,
	agentruns.StatusAwaitingApproval: workitems.ColumnReview,
	agentruns.StatusSucceeded:        workitems.ColumnDone,
	agentruns.StatusFailed:           workitems.ColumnTodo,
	agentruns.StatusCancelled:        workitems.ColumnTodo,
	agentruns.StatusTimedOut:         workitems.ColumnTodo,
}

func (h *Hooks) syncWorkItem(run *agentruns.Run) {
	actor := "agent:" + run.ID

	// Auto-create a tracking card for root, non-interview, project-scoped runs.
	if run.WorkItemID == nil {
		if run.Status != agentruns.StatusQueued || run.ParentRunID != nil || run.InterviewSessionID != nil || run.GuidedSessionID != nil || run.ProjectID == nil {
			return
		}
		title := "Agent run: " + run.AgentName
		if run.AgentName == "" {
			title = "Agent run"
		}
		description := agentruns.Truncate(run.Prompt, 300)
		item, err := h.workItemService.Create(workitems.CreateWorkItemRequest{
			ProjectID:    *run.ProjectID,
			Title:        title,
			Description:  description,
			Column:       workitems.ColumnTodo,
			AssigneeType: workitems.AssigneeAgent,
			AssigneeID:   &run.AgentID,
		}, nil, actor)
		if err != nil {
			slog.Error("orchestration: failed to create tracking card for run", "run_id", run.ID, "error", err)
			return
		}
		if err := h.runService.AttachWorkItem(run.ID, item.ID); err != nil {
			slog.Error("orchestration: failed to attach card to run", "work_item_id", item.ID, "run_id", run.ID, "error", err)
		}
		run.WorkItemID = &item.ID
		_ = h.workItemService.RecordRunActivity(item.ID, workitems.KindRunStarted, "Run queued", actor, map[string]interface{}{"run_id": run.ID})
		return
	}

	column, ok := statusToColumn[run.Status]
	if !ok {
		return
	}
	if _, err := h.workItemService.Move(*run.WorkItemID, workitems.MoveRequest{Column: column, SortOrder: 0}, actor); err != nil {
		// A card deleted since the run named it, on its own or with its
		// project, has nothing to move and no activity to keep (#379 bug
		// 149: each status change of such a run logged this ERROR).
		if errors.Is(err, workitems.ErrNotFound) {
			slog.Debug("orchestration: the run's card is gone", "run_id", run.ID, "work_item_id", *run.WorkItemID)
			return
		}
		slog.Error("orchestration: failed to move card", "work_item_id", *run.WorkItemID, "error", err)
	}

	switch run.Status {
	case agentruns.StatusRunning:
		_ = h.workItemService.RecordRunActivity(*run.WorkItemID, workitems.KindRunStarted, "Run started", actor, map[string]interface{}{"run_id": run.ID})
	case agentruns.StatusSucceeded, agentruns.StatusAwaitingApproval:
		_ = h.workItemService.RecordRunActivity(*run.WorkItemID, workitems.KindRunFinished, agentruns.TruncateAnswer(run.FinalText), actor, map[string]interface{}{"run_id": run.ID, "status": run.Status})
	case agentruns.StatusFailed, agentruns.StatusCancelled, agentruns.StatusTimedOut:
		_ = h.workItemService.RecordRunActivity(*run.WorkItemID, workitems.KindRunFailed, run.Error, actor, map[string]interface{}{"run_id": run.ID, "status": run.Status})
	}
}

// --- Team follow-ups ---

func (h *Hooks) enqueueSuccessors(run *agentruns.Run) {
	if run.TeamNodeID == nil || run.TeamID == nil {
		return
	}
	// The agent successors the workspace's budget refused, recorded together
	// once every edge was tried: an event and a note on the run, each naming
	// them and the budget (OpenV REQ-76).
	var skipped []agentruns.Successor
	var refusal error
	for _, edgeType := range []string{teams.EdgeHandsOff, teams.EdgeReviews} {
		edges, err := h.teamService.SuccessorEdges(*run.TeamNodeID, edgeType)
		if err != nil {
			slog.Error("orchestration: successor lookup failed for node", "team_node_id", *run.TeamNodeID, "edge_type", edgeType, "error", err)
			continue
		}
		for _, edge := range edges {
			if target, err := h.launchSuccessor(run, edge, edgeType); errors.Is(err, agentruns.ErrBudgetExceeded) {
				skipped = append(skipped, agentruns.Successor{NodeID: target.ID, Label: target.Label})
				refusal = err
			}
		}
	}
	if len(skipped) > 0 {
		if err := h.runService.SuccessorsSkipped(run, skipped, refusal); err != nil {
			slog.Error("orchestration: failed to record the successors the budget refused", "run_id", run.ID, "error", err)
		}
	}
}

// launchSuccessor starts what an edge leads to: an agent's run, or a card
// for a person. It returns the node the edge leads to, nil when the crew or
// the node is gone, and for an agent the launch's error.
func (h *Hooks) launchSuccessor(run *agentruns.Run, edge *teams.Edge, edgeType string) (*teams.Node, error) {
	graph, err := h.teamService.GetTeam(edge.TeamID)
	if err != nil {
		slog.Error("orchestration: team lookup failed", "team_id", edge.TeamID, "error", err)
		return nil, nil
	}
	var target *teams.Node
	for _, node := range graph.Nodes {
		if node.ID == edge.ToNodeID {
			target = node
			break
		}
	}
	if target == nil {
		return nil, nil
	}

	// Human targets never get an agent run; they get a kanban card instead.
	if target.IsHuman() {
		h.handOffToHuman(run, graph, edge, edgeType, target)
		return target, nil
	}

	output := agentruns.TruncateAnswer(run.FinalText)

	var prompt string
	if template, ok := edge.Config["prompt_template"].(string); ok && strings.TrimSpace(template) != "" {
		prompt = automations.RenderPrompt(template, map[string]string{
			"handoff.output": output,
			"handoff.run_id": run.ID,
		})
	} else if edgeType == teams.EdgeReviews {
		prompt = fmt.Sprintf("You are reviewing the output of a teammate's run (run id %s). Their final report follows. Review it critically: check claims against the OpenV requirements database via your tools, and post your review as a comment.\n\n---\n%s", run.ID, output)
	} else {
		prompt = fmt.Sprintf("A teammate finished their part of the work (run id %s). Their handoff output follows. Continue the work per your role, using your OpenV tools to read whatever requirements or cards you need.\n\n---\n%s", run.ID, output)
	}

	parentID := run.ID
	_, _, err = h.runService.Launch(agentruns.LaunchRequest{
		OrgID:       run.OrgID,
		AgentID:     target.AgentID,
		ProjectID:   run.ProjectID,
		TeamID:      run.TeamID,
		TeamNodeID:  &target.ID,
		ParentRunID: &parentID,
		WorkItemID:  run.WorkItemID,
		Priority:    agentruns.PriorityChild,
		Prompt:      prompt,
	})
	if err != nil {
		slog.Error("orchestration: failed to launch successor for run", "edge_type", edgeType, "run_id", run.ID, "error", err)
	}
	return target, err
}

// handOffToHuman turns a hands-off-to/reviews edge into a human node into a
// kanban card assigned to that person in the run's project, if the person
// may be handed one there (ProjectReach). Anyone else is refused: the reason
// is kept on the run and on its card, no card is made, and no access is
// granted (OpenV REQ-23, REQ-81).
func (h *Hooks) handOffToHuman(run *agentruns.Run, graph *teams.TeamGraph, edge *teams.Edge, edgeType string, target *teams.Node) {
	if run.ProjectID == nil {
		slog.Warn("orchestration: run hands off to human node but has no project; skipping work item", "run_id", run.ID, "team_node_id", target.ID)
		return
	}
	if target.UserID == nil {
		slog.Warn("orchestration: human node has no user; skipping work item", "team_node_id", target.ID, "run_id", run.ID)
		return
	}
	if reason := h.handOffRefusal(*run.ProjectID, *target.UserID, target.Label); reason != "" {
		h.refuseHandOff(run, edgeType, target, reason)
		return
	}

	sourceLabel := run.AgentName
	for _, node := range graph.Nodes {
		if node.ID == edge.FromNodeID {
			if node.Label != "" {
				sourceLabel = node.Label
			}
			break
		}
	}

	title := "Handoff from " + sourceLabel
	if edgeType == teams.EdgeReviews {
		title = "Review request: " + sourceLabel
	}
	description := agentruns.Truncate(run.FinalText, 500)
	description += "\n\n(from agent run " + run.ID + ")"

	actor := "agent:" + run.ID
	item, err := h.workItemService.Create(workitems.CreateWorkItemRequest{
		ProjectID:    *run.ProjectID,
		Title:        title,
		Description:  description,
		Column:       workitems.ColumnTodo,
		AssigneeType: workitems.AssigneeUser,
		AssigneeID:   target.UserID,
	}, nil, actor)
	if err != nil {
		slog.Error("orchestration: failed to create handoff card for human node", "edge_type", edgeType, "team_node_id", target.ID, "run_id", run.ID, "error", err)
		return
	}
	if run.WorkItemID != nil {
		_ = h.workItemService.RecordRunActivity(*run.WorkItemID, workitems.KindRunFinished,
			"Handed off to "+target.Label, actor,
			map[string]interface{}{"run_id": run.ID, "work_item_id": item.ID, "edge_type": edgeType})
	}
}

// handOffRefusal is why a hand-off card may not go to a person in a project,
// or "" when it may: they can open the project (ProjectReach). The reason
// names the rule that refused, with the remedy that would let them in: a
// role in the project for a member with none, and the workspace itself for
// someone who has left it, whom no role in the project lets in. An access
// that cannot be checked, or an answer this does not know, refuses too.
func (h *Hooks) handOffRefusal(projectID, userID, label string) string {
	if h.reach == nil {
		return label + "'s access to this project could not be checked."
	}
	reach, err := h.reach(projectID, userID)
	if err != nil {
		slog.Error("orchestration: could not check a hand-off target's project access", "project_id", projectID, "user_id", userID, "error", err)
		return label + "'s access to this project could not be checked."
	}
	switch reach {
	case ReachAllowed:
		return ""
	case ReachNoRole:
		return label + " has no role in this project and could not open the card. Give them a role in the project to hand work to them."
	case ReachNotInWorkspace:
		return label + " is no longer a member of this workspace and could not open the card. Add them back to the workspace, with a role in the project, to hand work to them."
	}
	slog.Error("orchestration: a hand-off target's project access has an unknown answer", "project_id", projectID, "user_id", userID, "reach", string(reach))
	return label + "'s access to this project could not be checked."
}

// refuseHandOff keeps a refused hand-off's reason on the run, as a note at
// the end of its log, and on the run's card, where a hand-off made is
// recorded.
func (h *Hooks) refuseHandOff(run *agentruns.Run, edgeType string, target *teams.Node, reason string) {
	what := "Hand-off"
	if edgeType == teams.EdgeReviews {
		what = "Review request"
	}
	message := what + " to " + target.Label + " refused: " + reason
	slog.Warn("orchestration: hand-off to a person refused", "run_id", run.ID, "team_node_id", target.ID, "reason", reason)
	if err := h.runService.NoteRun(run.ID, agentruns.NoteHandOffRefused, message, map[string]interface{}{
		"team_node_id": target.ID,
		"user_id":      *target.UserID,
		"edge_type":    edgeType,
	}); err != nil {
		slog.Error("orchestration: failed to note a refused hand-off on the run", "run_id", run.ID, "error", err)
	}
	if run.WorkItemID != nil {
		_ = h.workItemService.RecordRunActivity(*run.WorkItemID, workitems.KindRunFailed, message, "agent:"+run.ID,
			map[string]interface{}{"run_id": run.ID, "team_node_id": target.ID, "edge_type": edgeType})
	}
}

// --- Interview turns ---

func (h *Hooks) deliverInterviewReply(run *agentruns.Run) {
	if run.InterviewSessionID == nil {
		return
	}
	reply := strings.TrimSpace(run.FinalText)
	if reply == "" {
		reply = "(the interviewer had nothing further to add)"
	}
	message, err := h.interviewService.AppendMessage(*run.InterviewSessionID, interviews.RoleAssistant, reply)
	if errors.Is(err, interviews.ErrSessionNotFound) {
		// The session went with its interview or project: no one to answer.
		slog.Debug("orchestration: the run's interview session is gone", "run_id", run.ID, "session_id", *run.InterviewSessionID)
		return
	}
	if err != nil {
		slog.Error("orchestration: failed to append interview reply", "session_id", *run.InterviewSessionID, "error", err)
		return
	}
	if h.broadcaster != nil {
		h.broadcaster.BroadcastSession("interview:"+*run.InterviewSessionID, "message", message)
	}
}

func (h *Hooks) deliverInterviewFailure(run *agentruns.Run) {
	if run.InterviewSessionID == nil {
		return
	}
	message, err := h.interviewService.AppendMessage(*run.InterviewSessionID, interviews.RoleSystem,
		"The interviewer hit a technical problem answering. Your messages are saved — please try again in a moment.")
	if err != nil {
		return
	}
	if h.broadcaster != nil {
		h.broadcaster.BroadcastSession("interview:"+*run.InterviewSessionID, "message", message)
	}
}

// --- Guided copilot turns ---

func (h *Hooks) deliverGuidedReply(run *agentruns.Run) {
	if run.GuidedSessionID == nil {
		return
	}
	reply := strings.TrimSpace(run.FinalText)
	if reply == "" {
		reply = "(the copilot had nothing further to add)"
	}
	message, err := h.guidedService.AppendChatMessage(*run.GuidedSessionID, guided.ChatRoleAssistant, reply)
	if errors.Is(err, guided.ErrSessionNotFound) {
		// The session went with its project: no one to answer.
		slog.Debug("orchestration: the run's guided session is gone", "run_id", run.ID, "session_id", *run.GuidedSessionID)
		return
	}
	if err != nil {
		slog.Error("orchestration: failed to append guided copilot reply", "session_id", *run.GuidedSessionID, "error", err)
		return
	}
	if h.broadcaster != nil {
		h.broadcaster.BroadcastSession("guided:"+*run.GuidedSessionID, "message", message)
	}
}

func (h *Hooks) deliverGuidedFailure(run *agentruns.Run) {
	if run.GuidedSessionID == nil {
		return
	}
	message, err := h.guidedService.AppendChatMessage(*run.GuidedSessionID, guided.ChatRoleSystem,
		"The copilot hit a technical problem answering. Your messages are saved — please try again in a moment.")
	if err != nil {
		return
	}
	if h.broadcaster != nil {
		h.broadcaster.BroadcastSession("guided:"+*run.GuidedSessionID, "message", message)
	}
}

// launchPendingNudge launches the one turn a session's parked nudge is owed,
// now that the run that was in flight has finished. Nudges are commentary:
// a failure is logged, never surfaced to the wizard.
func (h *Hooks) launchPendingNudge(run *agentruns.Run) {
	if run.GuidedSessionID == nil || h.guidedService == nil {
		return
	}
	sessionID := *run.GuidedSessionID
	nudge, err := h.guidedService.TakePendingNudge(sessionID)
	if err != nil {
		slog.Error("orchestration: failed to read the session's pending nudge", "session_id", sessionID, "error", err)
		return
	}
	if nudge == nil {
		return
	}
	// The wizard may have been committed or abandoned while the turn ran: a
	// closed session gets no copilot turn, and the nudge it was owed dies
	// with it. Checked after the take, so the nudge is cleared either way.
	session, err := h.guidedService.GetSession(sessionID)
	if err != nil {
		slog.Warn("orchestration: could not read the guided session for its parked nudge; dropping it", "session_id", sessionID, "error", err)
		return
	}
	if session == nil || session.Status != guided.StatusInProgress {
		status := "missing"
		if session != nil {
			status = session.Status
		}
		slog.Info("orchestration: discarding a parked wizard nudge for a closed guided session", "session_id", sessionID, "status", status)
		return
	}
	if h.nudgeLauncher == nil {
		slog.Warn("orchestration: no nudge launcher wired; dropping the parked wizard nudge", "session_id", sessionID)
		return
	}
	if err := h.nudgeLauncher.LaunchGuidedNudge(sessionID, *nudge, run.LaunchedBy); err != nil {
		slog.Warn("orchestration: failed to launch the coalesced wizard nudge", "session_id", sessionID, "error", err)
	}
}

// --- Board-drives-AI trigger ---

func (h *Hooks) onEvent(e domainevents.Event) {
	if e.EventType != domainevents.WorkItemMoved {
		return
	}
	// Only human moves launch runs — system/agent moves would loop.
	if !strings.HasPrefix(e.Actor, "user:") {
		return
	}
	column, _ := e.Payload["column"].(string)
	assigneeType, _ := e.Payload["assignee_type"].(string)
	if column != workitems.ColumnTodo || assigneeType != workitems.AssigneeAgent {
		return
	}

	item, err := h.workItemService.Get(e.EntityID)
	if err != nil || item == nil || item.AssigneeID == nil {
		return
	}

	// The run belongs to (and is listed within) the card's project's org.
	// Resolve it up front: the run listing fails closed on org, and a card
	// whose workspace can't be resolved can neither be checked nor launched.
	orgID := ""
	if h.projectService != nil {
		if project, err := h.projectService.GetProject(item.ProjectID); err == nil && project != nil {
			orgID = project.OrgID
		}
	}
	if orgID == "" {
		slog.Warn("orchestration: board trigger could not resolve org for project; skipping card", "project_id", item.ProjectID, "work_item_id", item.ID)
		return
	}

	// Skip when a live run is already attached to this card.
	active, err := h.runService.List(agentruns.ListFilter{OrgID: orgID, WorkItemID: item.ID, Limit: 5})
	if err == nil {
		for _, r := range active {
			switch r.Status {
			case agentruns.StatusQueued, agentruns.StatusClaimed, agentruns.StatusRunning:
				return
			}
		}
	}

	// Lean-context prompt: card text and artifact IDs only; the agent pulls
	// requirement content through its OpenV tools.
	var b strings.Builder
	fmt.Fprintf(&b, "Work item: %s\n", item.Title)
	if item.Description != "" {
		fmt.Fprintf(&b, "Description: %s\n", item.Description)
	}
	fmt.Fprintf(&b, "Work item id: %s\n", item.ID)
	if len(item.ArtifactIDs) > 0 {
		fmt.Fprintf(&b, "Linked artifact ids: %s\n", strings.Join(item.ArtifactIDs, ", "))
	}
	b.WriteString("\nUse get_work_item and get_work_item_history for full card context, and fetch each linked artifact via your OpenV tools before acting. If the card lacks the requirements you need, say so and stop.")

	projectID := item.ProjectID
	workItemID := item.ID
	run, _, err := h.runService.Launch(agentruns.LaunchRequest{
		OrgID:      orgID,
		AgentID:    *item.AssigneeID,
		ProjectID:  &projectID,
		WorkItemID: &workItemID,
		Prompt:     b.String(),
	})
	if err != nil {
		slog.Error("orchestration: board trigger failed to launch run for card", "work_item_id", item.ID, "error", err)
		_ = h.workItemService.RecordRunActivity(item.ID, workitems.KindRunFailed, "Failed to launch agent: "+err.Error(), "system", nil)
		return
	}
	_ = h.workItemService.RecordRunActivity(item.ID, workitems.KindRunStarted, "Agent run launched from board", "system", map[string]interface{}{"run_id": run.ID})
}
