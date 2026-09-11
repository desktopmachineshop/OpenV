package runner

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
)

// partialHandle is a RunHandle whose answer-so-far the test drives.
type partialHandle struct {
	mu      sync.Mutex
	partial string
	events  chan RunEvent
}

func newPartialHandle() *partialHandle { return &partialHandle{events: make(chan RunEvent, 8)} }

func (f *partialHandle) Events() <-chan RunEvent { return f.events }
func (f *partialHandle) Wait() (Result, error)   { return Result{}, nil }
func (f *partialHandle) Cancel()                 {}
func (f *partialHandle) PartialText() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.partial
}
func (f *partialHandle) write(text string) {
	f.mu.Lock()
	f.partial = text
	f.mu.Unlock()
}

// A handle with no answer-so-far at all: the pump must not invent one.
type muteHandle struct{ events chan RunEvent }

func (m *muteHandle) Events() <-chan RunEvent { return m.events }
func (m *muteHandle) Wait() (Result, error)   { return Result{}, nil }
func (m *muteHandle) Cancel()                 {}

type recordedPush struct {
	Entries     []agentruns.LogEntry `json:"entries"`
	PartialText string               `json:"partial_text"`
}

// logServer records every log push in the current (object) body shape.
func logServer(t *testing.T, pushes *[]recordedPush, mu *sync.Mutex) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var push recordedPush
		if err := json.Unmarshal(body, &push); err != nil {
			t.Errorf("log body is not the streaming shape: %s", body)
		}
		mu.Lock()
		*pushes = append(*pushes, push)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"cancel_requested":false,"status":"running"}`))
	}))
}

// The pump ships the whole answer so far, and only when it changed — an
// unchanged answer would re-send the same kilobytes every 750ms for nothing.
func TestPumpSendsPartialTextOnlyWhenItChanges(t *testing.T) {
	var mu sync.Mutex
	var pushes []recordedPush
	srv := logServer(t, &pushes, &mu)
	defer srv.Close()

	w := &Worker{client: NewClient(srv.URL, "worker-key")}
	handle := newPartialHandle()

	done := make(chan bool, 1)
	go func() { done <- w.pump("run-1", handle) }()

	handle.write("Your vision ")
	time.Sleep(900 * time.Millisecond)
	handle.write("Your vision statement is vague.")
	time.Sleep(900 * time.Millisecond)
	// No further writing: the next flush must carry no partial text.
	time.Sleep(900 * time.Millisecond)
	close(handle.events)
	<-done

	mu.Lock()
	defer mu.Unlock()
	var sent []string
	for _, p := range pushes {
		if p.PartialText != "" {
			sent = append(sent, p.PartialText)
		}
	}
	if len(sent) != 2 {
		t.Fatalf("partial pushes = %v, want exactly the two changes", sent)
	}
	if sent[0] != "Your vision " || sent[1] != "Your vision statement is vague." {
		t.Fatalf("partial pushes = %v, want the whole text each time", sent)
	}
}

// A provider that reports nothing (gemini) streams nothing, but its logs and
// heartbeats keep flowing.
func TestPumpSendsNoPartialForSilentProvider(t *testing.T) {
	var mu sync.Mutex
	var pushes []recordedPush
	srv := logServer(t, &pushes, &mu)
	defer srv.Close()

	w := &Worker{client: NewClient(srv.URL, "worker-key")}
	handle := &muteHandle{events: make(chan RunEvent, 1)}
	done := make(chan bool, 1)
	go func() { done <- w.pump("run-1", handle) }()
	handle.events <- RunEvent{Kind: agentruns.LogText, Payload: map[string]interface{}{"text": "hello"}}
	time.Sleep(900 * time.Millisecond)
	close(handle.events)
	<-done

	mu.Lock()
	defer mu.Unlock()
	entries := 0
	for _, p := range pushes {
		if p.PartialText != "" {
			t.Fatalf("a provider with no partial text pushed %q", p.PartialText)
		}
		entries += len(p.Entries)
	}
	if entries != 1 {
		t.Fatalf("pushed %d log entries, want the one event", entries)
	}
}

// An API that predates the streaming body answers 400. The runner must not
// lose its logs (or its heartbeat) over that: it downgrades to the legacy
// bare-array body and stays there.
func TestPushLogsFallsBackToLegacyBody(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		if len(body) > 0 && body[0] != '[' {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"cancel_requested":true,"status":"running"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "worker-key")
	entries := []agentruns.LogEntry{{RunID: "run-1", Seq: 1, Kind: agentruns.LogText}}
	cancelled, status, err := c.PushLogs("run-1", entries, "half an answer")
	if err != nil {
		t.Fatalf("PushLogs against an old API = %v, want the legacy retry to succeed", err)
	}
	if !cancelled || status != "running" {
		t.Fatalf("cancel_requested=%v status=%q from the legacy response", cancelled, status)
	}
	// A second push goes straight to the legacy shape.
	if _, _, err := c.PushLogs("run-1", entries, "more of the answer"); err != nil {
		t.Fatalf("second PushLogs: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 3 {
		t.Fatalf("server saw %d bodies, want 3 (object, legacy retry, legacy)", len(bodies))
	}
	if bodies[0][0] != '{' || bodies[1][0] != '[' || bodies[2][0] != '[' {
		t.Fatalf("body shapes = %q", bodies)
	}
}
