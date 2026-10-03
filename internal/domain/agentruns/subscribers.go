// Who hears of a run's changes: the Subscribers registered at wiring (the
// SSE hub and the crew and kanban hooks), and the event bus, which carries
// RunFinished for every terminal status.

package agentruns

import "github.com/openv/requirements-platform/internal/domain/events"

// Subscriber is notified on run lifecycle changes and appended logs
// (used by the SSE hub and by team/kanban follow-up hooks).
type Subscriber interface {
	RunLogsAppended(run *Run, entries []LogEntry)
	RunStatusChanged(run *Run)
	// RunPartialText is called when a live run reports more assistant text
	// (the whole text so far, not a delta). Subscribers that stream it to a
	// chat panel are expected to rate-limit their own fan-out.
	RunPartialText(run *Run, text string)
}

// AddSubscriber registers a lifecycle subscriber (not concurrency-safe;
// call during wiring only).
func (s *DefaultService) AddSubscriber(sub Subscriber) {
	s.subscribers = append(s.subscribers, sub)
}

func (s *DefaultService) notifyStatus(run *Run) {
	for _, sub := range s.subscribers {
		sub.RunStatusChanged(run)
	}
}

// publishRunFinished emits the RunFinished event for a run's terminal status.
// The notifier turns a failed status into an inbox alert for the launcher and
// the automation trigger matcher fires run-finished automations, so every
// terminal transition — the worker's Finish, the stale reaper, and the
// awaiting_approval resolution — must funnel through here or those consumers
// silently miss the run.
func (s *DefaultService) publishRunFinished(run *Run) {
	if s.bus == nil {
		return
	}
	projectID := ""
	if run.ProjectID != nil {
		projectID = *run.ProjectID
	}
	launchedBy := ""
	if run.LaunchedBy != nil {
		launchedBy = *run.LaunchedBy
	}
	s.bus.Publish(events.New(events.RunFinished, projectID, run.ID, "agent:"+run.ID, map[string]interface{}{
		"status":      run.Status,
		"agent_id":    run.AgentID,
		"launched_by": launchedBy,
	}).WithOrg(run.OrgID))
}
