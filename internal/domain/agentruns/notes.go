package agentruns

import (
	"errors"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/events"
)

// Notes the server keeps on a run, beside what its worker logged: what a
// finished crew run's edges could not do, which would otherwise show only in
// the server's own log. A note is an entry of kind marker at the end of the
// run's log, whose payload names it (marker), says it in words (message) and
// carries its detail, the shape of the worker's own markers.
const (
	// NoteHandOffRefused: a hand-off to a person was refused, since the
	// person may not be handed a card in the run's project (OpenV REQ-23,
	// REQ-81).
	NoteHandOffRefused = "handoff_refused"
	// NoteSuccessorsSkipped: agent successors were not launched, since the
	// workspace is over its monthly budget (OpenV REQ-76).
	NoteSuccessorsSkipped = "successors_skipped"
)

// Successor names a crew node that an edge of a finished run's node leads to.
type Successor struct {
	NodeID string
	Label  string
}

// NoteRun keeps a note on a run: an entry of kind marker at the end of its
// log, with the note's marker, message and detail in its payload, sent to the
// log's subscribers (the run's stream) as a worker's batch is.
func (s *DefaultService) NoteRun(runID, marker, message string, detail map[string]interface{}) error {
	payload := make(map[string]interface{}, len(detail)+2)
	for k, v := range detail {
		payload[k] = v
	}
	payload["marker"] = marker
	payload["message"] = message
	entry, err := s.repo.AppendNote(runID, LogEntry{RunID: runID, Kind: LogMarker, Payload: payload, CreatedAt: time.Now().UTC()})
	if err != nil {
		return err
	}
	run, err := s.Get(runID)
	if err != nil {
		return err
	}
	for _, sub := range s.subscribers {
		sub.RunLogsAppended(run, []LogEntry{entry})
	}
	return nil
}

// SuccessorsSkipped records that a finished crew run's agent successors were
// not launched because the workspace's budget refused them (refusal, the
// launch's ErrBudgetExceeded): a RunSuccessorsSkipped event and a note on the
// run, each naming the skipped successors and the budget refusal's reason,
// which names the budget and the month's spend (OpenV REQ-76).
func (s *DefaultService) SuccessorsSkipped(run *Run, skipped []Successor, refusal error) error {
	labels := make([]string, 0, len(skipped))
	nodeIDs := make([]string, 0, len(skipped))
	for _, n := range skipped {
		labels = append(labels, n.Label)
		nodeIDs = append(nodeIDs, n.NodeID)
	}
	reason := budgetReason(refusal)
	if s.bus != nil {
		projectID, teamID := "", ""
		if run.ProjectID != nil {
			projectID = *run.ProjectID
		}
		if run.TeamID != nil {
			teamID = *run.TeamID
		}
		s.bus.Publish(events.New(events.RunSuccessorsSkipped, projectID, run.ID, "agent:"+run.ID, map[string]interface{}{
			"agent_id":      run.AgentID,
			"team_id":       teamID,
			"successors":    labels,
			"team_node_ids": nodeIDs,
			"reason":        reason,
		}).WithOrg(run.OrgID))
	}
	verb := " was not launched: "
	if len(labels) > 1 {
		verb = " were not launched: "
	}
	return s.NoteRun(run.ID, NoteSuccessorsSkipped, joinNames(labels)+verb+reason, map[string]interface{}{
		"successors":    labels,
		"team_node_ids": nodeIDs,
		"reason":        reason,
	})
}

// budgetReason is the reason a budget refusal gives, without the sentinel
// Launch puts before it; the sentinel alone when the guard gave none.
func budgetReason(refusal error) string {
	if refusal == nil {
		return ErrBudgetExceeded.Error()
	}
	text := refusal.Error()
	if errors.Is(refusal, ErrBudgetExceeded) {
		if reason := strings.TrimPrefix(text, ErrBudgetExceeded.Error()+": "); reason != text && reason != "" {
			return reason
		}
	}
	return text
}

// joinNames lists names as prose: "A", "A and B", "A, B and C".
func joinNames(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
