package orchestration

import (
	"fmt"
	"log"
	"strings"

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
}

// NewHooks creates the orchestration hooks.
func NewHooks(runService agentruns.Service, teamService teams.Service, workItemService workitems.Service, interviewService interviews.Service, guidedService guided.Service, projectService projects.Service, broadcaster SessionBroadcaster) *Hooks {
	return &Hooks{
		runService:       runService,
		teamService:      teamService,
		workItemService:  workItemService,
		interviewService: interviewService,
		guidedService:    guidedService,
		projectService:   projectService,
		broadcaster:      broadcaster,
	}
}

// SubscribeBus attaches the board-drives-AI trigger.
func (h *Hooks) SubscribeBus(bus domainevents.Bus) {
	bus.Subscribe(h.onEvent)
}

// RunLogsAppended implements agentruns.Subscriber (no-op; the SSE hub
// handles log fan-out).
func (h *Hooks) RunLogsAppended(run *agentruns.Run, entries []agentruns.LogEntry) {}

// RunStatusChanged implements agentruns.Subscriber.
func (h *Hooks) RunStatusChanged(run *agentruns.Run) {
	h.syncWorkItem(run)

	switch run.Status {
	case agentruns.StatusSucceeded, agentruns.StatusAwaitingApproval:
		h.enqueueSuccessors(run)
		h.deliverInterviewReply(run)
		h.deliverGuidedReply(run)
	case agentruns.StatusFailed, agentruns.StatusTimedOut:
		h.deliverInterviewFailure(run)
		h.deliverGuidedFailure(run)
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
			log.Printf("orchestration: failed to create tracking card for run %s: %v", run.ID, err)
			return
		}
		if err := h.runService.AttachWorkItem(run.ID, item.ID); err != nil {
			log.Printf("orchestration: failed to attach card %s to run %s: %v", item.ID, run.ID, err)
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
		log.Printf("orchestration: failed to move card %s: %v", *run.WorkItemID, err)
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
	for _, edgeType := range []string{teams.EdgeHandsOff, teams.EdgeReviews} {
		edges, err := h.teamService.SuccessorEdges(*run.TeamNodeID, edgeType)
		if err != nil {
			log.Printf("orchestration: successor lookup failed for node %s: %v", *run.TeamNodeID, err)
			continue
		}
		for _, edge := range edges {
			h.launchSuccessor(run, edge, edgeType)
		}
	}
}

func (h *Hooks) launchSuccessor(run *agentruns.Run, edge *teams.Edge, edgeType string) {
	graph, err := h.teamService.GetTeam(edge.TeamID)
	if err != nil {
		log.Printf("orchestration: team %s lookup failed: %v", edge.TeamID, err)
		return
	}
	var target *teams.Node
	for _, node := range graph.Nodes {
		if node.ID == edge.ToNodeID {
			target = node
			break
		}
	}
	if target == nil {
		return
	}

	// Human targets never get an agent run; they get a kanban card instead.
	if target.IsHuman() {
		h.handOffToHuman(run, graph, edge, edgeType, target)
		return
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
		log.Printf("orchestration: failed to launch %s successor for run %s: %v", edgeType, run.ID, err)
	}
}

// handOffToHuman turns a hands-off-to/reviews edge into a human node into a
// kanban card assigned to that person in the run's project.
func (h *Hooks) handOffToHuman(run *agentruns.Run, graph *teams.TeamGraph, edge *teams.Edge, edgeType string, target *teams.Node) {
	if run.ProjectID == nil {
		log.Printf("orchestration: run %s hands off to human node %s but has no project; skipping work item", run.ID, target.ID)
		return
	}
	if target.UserID == nil {
		log.Printf("orchestration: human node %s has no user; skipping work item for run %s", target.ID, run.ID)
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
		log.Printf("orchestration: failed to create %s card for human node %s from run %s: %v", edgeType, target.ID, run.ID, err)
		return
	}
	if run.WorkItemID != nil {
		_ = h.workItemService.RecordRunActivity(*run.WorkItemID, workitems.KindRunFinished,
			"Handed off to "+target.Label, actor,
			map[string]interface{}{"run_id": run.ID, "work_item_id": item.ID, "edge_type": edgeType})
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
	if err != nil {
		log.Printf("orchestration: failed to append interview reply for session %s: %v", *run.InterviewSessionID, err)
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
	if err != nil {
		log.Printf("orchestration: failed to append guided copilot reply for session %s: %v", *run.GuidedSessionID, err)
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

	// Skip when a live run is already attached to this card.
	active, err := h.runService.List(agentruns.ListFilter{WorkItemID: item.ID, Limit: 5})
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

	// The run belongs to the card's project's org.
	orgID := ""
	if h.projectService != nil {
		if project, err := h.projectService.GetProject(item.ProjectID); err == nil && project != nil {
			orgID = project.OrgID
		}
	}
	if orgID == "" {
		log.Printf("orchestration: board trigger could not resolve org for project %s; skipping card %s", item.ProjectID, item.ID)
		return
	}

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
		log.Printf("orchestration: board trigger failed to launch run for card %s: %v", item.ID, err)
		_ = h.workItemService.RecordRunActivity(item.ID, workitems.KindRunFailed, "Failed to launch agent: "+err.Error(), "system", nil)
		return
	}
	_ = h.workItemService.RecordRunActivity(item.ID, workitems.KindRunStarted, "Agent run launched from board", "system", map[string]interface{}{"run_id": run.ID})
}
