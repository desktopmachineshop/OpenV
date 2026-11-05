package agentruns

import (
	"errors"
	"reflect"
	"testing"
)

// testNote is a marker for a note the tests keep.
const testNote = "test_note"

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

	if err := svc.NoteRun("r1", testNote, "A note.", map[string]interface{}{"detail": "n2"}); err != nil {
		t.Fatalf("NoteRun: %v", err)
	}
	logs := repo.logs["r1"]
	if len(logs) != 3 {
		t.Fatalf("log = %+v, want the note after the worker's two entries", logs)
	}
	note := logs[2]
	want := map[string]interface{}{"marker": testNote, "message": "A note.", "detail": "n2"}
	if note.Seq != 3 || note.Kind != LogMarker || !reflect.DeepEqual(note.Payload, want) || note.CreatedAt.IsZero() {
		t.Errorf("note = %+v, want seq 3, kind marker, payload %v, a time", note, want)
	}
	if len(rec.appended) != 1 || rec.appended[0].Seq != 3 {
		t.Errorf("subscribers were sent %+v, want the note", rec.appended)
	}

	if err := svc.NoteRun("gone", testNote, "x", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("NoteRun on a run no row has = %v, want ErrNotFound", err)
	}
}
