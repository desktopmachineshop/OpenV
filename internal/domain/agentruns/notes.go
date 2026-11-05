package agentruns

import (
	"time"
)

// Notes the server keeps on a run, beside what its worker logged. A note is an
// entry of kind marker at the end of the run's log, whose payload names it
// (marker), says it in words (message) and carries its detail, the shape of
// the worker's own markers.

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
