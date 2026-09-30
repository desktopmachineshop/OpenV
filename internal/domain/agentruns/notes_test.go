package agentruns

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/events"
)

// AppendNote mirrors the SQL: the note is numbered after the last entry of
// the run's log, and a run no row has is refused.
func (f *fakeRunRepo) AppendNote(runID string, entry LogEntry) (LogEntry, error) {
	if _, ok := f.runs[runID]; !ok {
		return entry, ErrNotFound
	}
	entry.RunID = runID
	entry.Seq = 1
	if logs := f.logs[runID]; len(logs) > 0 {
		entry.Seq = logs[len(logs)-1].Seq + 1
	}
	f.logs[runID] = append(f.logs[runID], entry)
	return entry, nil
}

// logRecorder records what a service sends its log subscribers.
type logRecorder struct {
	Subscriber
	appended []LogEntry
}

func (r *logRecorder) RunLogsAppended(run *Run, entries []LogEntry) {
	r.appended = append(r.appended, entries...)
}

// TestNoteRunAppendsAMarkerToTheRunsLog: a note is an entry of kind marker at
// the end of the run's log, after what the worker logged, with its marker,
// message and detail, and the run's stream is sent it as a worker's batch is.
func TestNoteRunAppendsAMarkerToTheRunsLog(t *testing.T) {
	svc, repo := newFakeService(&Run{ID: "r1", Status: StatusSucceeded})
	repo.logs["r1"] = []LogEntry{{RunID: "r1", Seq: 1, Kind: LogText}, {RunID: "r1", Seq: 2, Kind: LogUsage}}
	rec := &logRecorder{}
	svc.AddSubscriber(rec)

	if err := svc.NoteRun("r1", NoteHandOffRefused, "Hand-off to Dana refused.", map[string]interface{}{"team_node_id": "n2"}); err != nil {
		t.Fatalf("NoteRun: %v", err)
	}
	logs := repo.logs["r1"]
	if len(logs) != 3 {
		t.Fatalf("log = %+v, want the note after the worker's two entries", logs)
	}
	note := logs[2]
	want := map[string]interface{}{"marker": NoteHandOffRefused, "message": "Hand-off to Dana refused.", "team_node_id": "n2"}
	if note.Seq != 3 || note.Kind != LogMarker || !reflect.DeepEqual(note.Payload, want) || note.CreatedAt.IsZero() {
		t.Errorf("note = %+v, want seq 3, kind marker, payload %v, a time", note, want)
	}
	if len(rec.appended) != 1 || rec.appended[0].Seq != 3 {
		t.Errorf("subscribers were sent %+v, want the note", rec.appended)
	}

	if err := svc.NoteRun("gone", NoteHandOffRefused, "x", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("NoteRun on a run no row has = %v, want ErrNotFound", err)
	}
}

// TestSuccessorsSkippedPublishesAnEventAndNotesTheRun: the budget's refusal
// of a crew run's successors is recorded beyond the server's log (OpenV
// REQ-76): an agentrun.successors_skipped event naming the successors and the
// budget, as the run's agent, and a note on the run saying the same.
func TestSuccessorsSkippedPublishesAnEventAndNotesTheRun(t *testing.T) {
	bus := &fakeBus{}
	run := &Run{ID: "r1", OrgID: "org-1", AgentID: "a1", ProjectID: strptr("p1"), TeamID: strptr("t1"), Status: StatusSucceeded}
	svc, repo := newFakeServiceWithBus(bus, run)
	const reason = "this workspace has reached its $1.00 monthly budget ($5.00 spent); new runs are blocked until next month or the budget is raised"
	refusal := fmt.Errorf("%w: %s", ErrBudgetExceeded, reason)

	skipped := []Successor{{NodeID: "n2", Label: "Checker"}, {NodeID: "n3", Label: "Critic"}}
	if err := svc.SuccessorsSkipped(run, skipped, refusal); err != nil {
		t.Fatalf("SuccessorsSkipped: %v", err)
	}
	if len(bus.published) != 1 {
		t.Fatalf("published %+v, want one event", bus.published)
	}
	e := bus.published[0]
	wantPayload := map[string]interface{}{
		"agent_id": "a1", "team_id": "t1", "successors": []string{"Checker", "Critic"}, "team_node_ids": []string{"n2", "n3"},
		"reason": reason,
	}
	if e.EventType != events.RunSuccessorsSkipped || e.ProjectID != "p1" || e.EntityID != "r1" || e.Actor != "agent:r1" ||
		e.OrgID != "org-1" || !reflect.DeepEqual(e.Payload, wantPayload) {
		t.Errorf("event = %+v, want agentrun.successors_skipped of r1 in p1 and org-1, as agent:r1, payload %v", e, wantPayload)
	}
	logs := repo.logs["r1"]
	if len(logs) != 1 {
		t.Fatalf("log = %+v, want the note", logs)
	}
	wantNote := map[string]interface{}{
		"marker":        NoteSuccessorsSkipped,
		"message":       "Checker and Critic were not launched: " + reason,
		"successors":    []string{"Checker", "Critic"},
		"team_node_ids": []string{"n2", "n3"},
		"reason":        reason,
	}
	if logs[0].Kind != LogMarker || !reflect.DeepEqual(logs[0].Payload, wantNote) {
		t.Errorf("note = %+v, want %v", logs[0], wantNote)
	}

	// One successor reads in the singular; a refusal the guard gave no reason
	// for says the sentinel's words.
	repo.logs["r1"] = nil
	if err := svc.SuccessorsSkipped(run, skipped[:1], ErrBudgetExceeded); err != nil {
		t.Fatalf("SuccessorsSkipped: %v", err)
	}
	if got := repo.logs["r1"][0].Payload["message"]; got != "Checker was not launched: workspace monthly budget exceeded" {
		t.Errorf("message = %q", got)
	}
}

func TestJoinNames(t *testing.T) {
	for _, tc := range []struct {
		names []string
		want  string
	}{
		{nil, ""},
		{[]string{"A"}, "A"},
		{[]string{"A", "B"}, "A and B"},
		{[]string{"A", "B", "C"}, "A, B and C"},
	} {
		if got := joinNames(tc.names); got != tc.want {
			t.Errorf("joinNames(%v) = %q, want %q", tc.names, got, tc.want)
		}
	}
}
