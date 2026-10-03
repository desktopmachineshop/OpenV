package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
)

// pagedRunLogs answers Logs as the repository does, a bounded page of the
// entries after the cursor in seq order, with a page of pageSize rather than
// listLogsPageLimit's 2,000, and records each cursor it was asked for.
type pagedRunLogs struct {
	*fakeRunService
	entries  []agentruns.LogEntry
	pageSize int
	cursors  []int
}

func (p *pagedRunLogs) Logs(runID string, afterSeq int) ([]agentruns.LogEntry, error) {
	p.cursors = append(p.cursors, afterSeq)
	var page []agentruns.LogEntry
	for _, e := range p.entries {
		if e.RunID == runID && e.Seq > afterSeq && len(page) < p.pageSize {
			page = append(page, e)
		}
	}
	return page, nil
}

// TestStreamAgentRunDrainsEveryLogPage: the stream's replay reads the run's
// log page after page, each from the last seq it sent, until a page comes
// back empty, and only then sends the status frame. The API tour's replay
// (cmd/server's worker_wire area) holds fewer entries than one page of the
// repository's read, so it cannot see a replay that stops after the first.
func TestStreamAgentRunDrainsEveryLogPage(t *testing.T) {
	h, svc := partialTextFixture(t)
	launcher := "user-1"
	svc.byID["run-1"].LaunchedBy = &launcher
	entries := []agentruns.LogEntry{
		{RunID: "run-1", Seq: 1, Kind: "text", Payload: map[string]interface{}{"text": "one"}},
		{RunID: "run-1", Seq: 2, Kind: "text", Payload: map[string]interface{}{"text": "two"}},
		{RunID: "run-1", Seq: 3, Kind: "system", Payload: map[string]interface{}{}},
	}
	for _, tc := range []struct {
		afterSeq    string
		wantSeqs    []int
		wantCursors []int
	}{
		{afterSeq: "", wantSeqs: []int{1, 2, 3}, wantCursors: []int{0, 2, 3}},
		{afterSeq: "1", wantSeqs: []int{2, 3}, wantCursors: []int{1, 3}},
	} {
		t.Run("after_seq="+tc.afterSeq, func(t *testing.T) {
			logs := &pagedRunLogs{fakeRunService: svc, entries: entries, pageSize: 2}
			h.runService = logs
			h.sseHub = NewSSEHub()
			r := httptest.NewRequest(http.MethodGet, "/api/v1/agent-runs/run-1/stream?after_seq="+tc.afterSeq, nil)
			// A cancelled request: ServeStream replays, then returns at once.
			ctx, cancel := context.WithCancel(r.Context())
			cancel()
			r = mux.SetURLVars(withUser(r.WithContext(ctx), launcher), map[string]string{"id": "run-1"})
			w := httptest.NewRecorder()
			h.StreamAgentRun(w, r)

			var want strings.Builder
			for _, seq := range tc.wantSeqs {
				data, err := json.Marshal(entries[seq-1])
				if err != nil {
					t.Fatal(err)
				}
				fmt.Fprintf(&want, "event: log\ndata: %s\n\n", data)
			}
			want.WriteString("event: status\ndata: {\"run_id\":\"run-1\",\"status\":\"running\"}\n\n")
			if got := w.Body.String(); got != want.String() {
				t.Errorf("the replay wrote\n%q\nwant every page's log frames, then the status\n%q", got, want.String())
			}
			if !slices.Equal(logs.cursors, tc.wantCursors) {
				t.Errorf("Logs was read after seqs %v, want %v: each page from the last seq sent, until one is empty",
					logs.cursors, tc.wantCursors)
			}
		})
	}
}
