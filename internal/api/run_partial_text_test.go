package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
)

// appendLogs drives the worker log endpoint with a raw body.
func appendLogs(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.AppendAgentRunLogs(w, workerRunReq(body, "org-1", "", "run-1"))
	return w
}

func partialTextFixture() (*Handler, *fakeRunService) {
	svc := &fakeRunService{byID: map[string]*agentruns.Run{
		"run-1": {ID: "run-1", OrgID: "org-1", Status: agentruns.StatusRunning},
	}}
	return &Handler{runService: svc}, svc
}

// The streaming body carries the whole answer so far alongside the batch, and
// the handler hands both to the service.
func TestAppendAgentRunLogsStoresPartialText(t *testing.T) {
	h, svc := partialTextFixture()
	body := `{"entries":[{"run_id":"run-1","seq":1,"kind":"text","payload":{"text":"hi"}}],"partial_text":"Your vision statement"}`
	if w := appendLogs(t, h, body); w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", w.Code, w.Body.String())
	}
	if len(svc.appendedPartials) != 1 || svc.appendedPartials[0] != "Your vision statement" {
		t.Fatalf("partial text reaching the service = %v", svc.appendedPartials)
	}
	if len(svc.appendedEntries) != 1 || len(svc.appendedEntries[0]) != 1 {
		t.Fatalf("entries reaching the service = %v", svc.appendedEntries)
	}

	var resp struct {
		CancelRequested bool   `json:"cancel_requested"`
		Status          string `json:"status"`
	}
	w := appendLogs(t, h, `{"entries":[],"partial_text":"Your vision statement is"}`)
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != agentruns.StatusRunning {
		t.Fatalf("status in response = %q, want the run's status", resp.Status)
	}
	if len(svc.appendedPartials) != 2 || svc.appendedPartials[1] != "Your vision statement is" {
		t.Fatalf("second partial = %v, want the whole text again (not a delta)", svc.appendedPartials)
	}
}

// A runner built before streaming posts a bare array. It must keep working —
// its logs and its heartbeat depend on this endpoint.
func TestAppendAgentRunLogsAcceptsLegacyArrayBody(t *testing.T) {
	h, svc := partialTextFixture()
	body := `[{"run_id":"run-1","seq":1,"kind":"text","payload":{"text":"hi"}}]`
	if w := appendLogs(t, h, body); w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", w.Code, w.Body.String())
	}
	if len(svc.appendedEntries) != 1 || len(svc.appendedEntries[0]) != 1 {
		t.Fatalf("entries reaching the service = %v", svc.appendedEntries)
	}
	if svc.appendedPartials[0] != "" {
		t.Fatalf("legacy body produced partial text %q", svc.appendedPartials[0])
	}
}

// Garbage is still a bad request, in either shape.
func TestAppendAgentRunLogsRejectsGarbage(t *testing.T) {
	h, _ := partialTextFixture()
	for _, body := range []string{`not json`, `{"entries":"nope"}`, `[{"seq":"one"}]`} {
		if w := appendLogs(t, h, body); w.Code != http.StatusBadRequest {
			t.Fatalf("body %q answered %d, want 400", body, w.Code)
		}
	}
}

// The pump sends the answer so far, not a delta, so a batch lost on the wire
// cannot corrupt what the reader sees: the next one repairs it.
func TestPartialTextIsWholeTextNotDelta(t *testing.T) {
	h, svc := partialTextFixture()
	appendLogs(t, h, `{"entries":[],"partial_text":"One"}`)
	// ...second batch lost...
	appendLogs(t, h, `{"entries":[],"partial_text":"One two three"}`)
	last := svc.appendedPartials[len(svc.appendedPartials)-1]
	if !strings.HasPrefix(last, "One") || last != "One two three" {
		t.Fatalf("last partial = %q, want the full text so far", last)
	}
}
