package orchestration

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	domainevents "github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/guided"
	"github.com/openv/requirements-platform/internal/domain/interviews"
	"github.com/openv/requirements-platform/internal/domain/projects"
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

// partialBroadcastInterval is the floor between two assistant_partial events
// for one run. The worker already batches at 750ms, but a retry, a second
// worker, or a future faster pump must not turn a reply into an SSE flood.
const partialBroadcastInterval = 500 * time.Millisecond

// Hooks wires run lifecycle changes to the kanban board and to interview
// and guided sessions, and lets kanban card moves drive agent runs.
// It implements agentruns.Subscriber and subscribes to the event bus.
type Hooks struct {
	runService       agentruns.Service
	workItemService  workitems.Service
	interviewService interviews.Service
	guidedService    guided.Service
	projectService   projects.Service
	broadcaster      SessionBroadcaster
	nudgeLauncher    GuidedNudgeLauncher

	// Last assistant_partial broadcast per run, for the rate limit. Entries
	// are dropped when the run reaches a terminal state.
	partialMu   sync.Mutex
	lastPartial map[string]time.Time
}

// NewHooks creates the orchestration hooks.
func NewHooks(runService agentruns.Service, workItemService workitems.Service, interviewService interviews.Service, guidedService guided.Service, projectService projects.Service, broadcaster SessionBroadcaster) *Hooks {
	return &Hooks{
		runService:       runService,
		workItemService:  workItemService,
		interviewService: interviewService,
		guidedService:    guidedService,
		projectService:   projectService,
		broadcaster:      broadcaster,
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
		// Genuine success: deliver the conversational reply. An agent run is
		// a single run: crews, whose successors and hand-offs once launched
		// here, are gone (issue 732).
		h.deliverInterviewReply(run)
		h.deliverGuidedReply(run)
	case agentruns.StatusAwaitingApproval:
		// The answer is ready but its writes await human review. Deliver the
		// conversational reply so interview/guided sessions aren't left
		// hanging.
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

	// Auto-create a tracking card for root, project-scoped runs an
	// automation, a schedule or a job of the app started. A conversation is
	// not a task: an interview, the assistant's guided chat, or a prompt a
	// person typed to an agent in the app gets no card (the retry of a run
	// that had none is launched as a conversation too).
	if run.WorkItemID == nil {
		if run.Status != agentruns.StatusQueued || run.ParentRunID != nil || run.InterviewSessionID != nil || run.GuidedSessionID != nil || run.ProjectID == nil {
			return
		}
		if run.Conversation {
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
