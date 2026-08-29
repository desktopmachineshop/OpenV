package runner

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
)

// --- test doubles ---

type fakeHandle struct {
	events chan RunEvent
	waitCh chan struct{}
	result Result
	err    error
}

func (h *fakeHandle) Events() <-chan RunEvent { return h.events }
func (h *fakeHandle) Wait() (Result, error)   { <-h.waitCh; return h.result, h.err }
func (h *fakeHandle) Cancel()                 {}

type fakeAdapter struct {
	start func(ctx context.Context, spec RunSpec) (RunHandle, error)
}

func (a *fakeAdapter) Name() string                        { return "fake" }
func (a *fakeAdapter) Detect(context.Context) Availability { return Availability{Installed: true} }
func (a *fakeAdapter) Start(ctx context.Context, spec RunSpec) (RunHandle, error) {
	return a.start(ctx, spec)
}

// recordingServer captures which worker lifecycle endpoints were hit and the
// finish request body when one arrives.
type recordingServer struct {
	mu         sync.Mutex
	hits       map[string]int
	finishBody agentruns.FinishRequest
	srv        *httptest.Server
}

func newRecordingServer() *recordingServer {
	rs := &recordingServer{hits: map[string]int{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/agent-runs/", func(w http.ResponseWriter, r *http.Request) {
		rs.mu.Lock()
		defer rs.mu.Unlock()
		switch {
		case has(r.URL.Path, "/start"):
			rs.hits["start"]++
			w.WriteHeader(http.StatusNoContent)
		case has(r.URL.Path, "/release"):
			rs.hits["release"]++
			w.WriteHeader(http.StatusNoContent)
		case has(r.URL.Path, "/logs"):
			rs.hits["logs"]++
			json.NewEncoder(w).Encode(map[string]interface{}{"cancel_requested": false, "status": "running"})
		case has(r.URL.Path, "/finish"):
			rs.hits["finish"]++
			_ = json.NewDecoder(r.Body).Decode(&rs.finishBody)
			json.NewEncoder(w).Encode(map[string]interface{}{})
		default:
			w.WriteHeader(http.StatusOK)
		}
	})
	rs.srv = httptest.NewServer(mux)
	return rs
}

func has(path, suffix string) bool {
	return len(path) >= len(suffix) && path[len(path)-len(suffix):] == suffix
}

func (rs *recordingServer) hit(name string) int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.hits[name]
}

func newTestWorker(rs *recordingServer, adapter Adapter) *Worker {
	client := NewClient(rs.srv.URL, "test-key")
	return &Worker{
		client:             client,
		adapters:           map[string]Adapter{"fake": adapter},
		workerID:           "w-test",
		workspaceBase:      "",
		apiURL:             rs.srv.URL,
		workspaceRetention: time.Hour,
	}
}

func testClaim() *ClaimResponse {
	return &ClaimResponse{
		Run:      &agentruns.Run{ID: "r1", Prompt: "hi"},
		Agent:    &agents.Agent{Provider: "fake", Name: "fake-agent"},
		RunToken: "tok",
	}
}

// TestExecuteReleasesOnShutdown: when the worker's context is cancelled
// (SIGINT) while a run is in flight, execute must RELEASE the claim back to the
// queue, not finish it as failed.
func TestExecuteReleasesOnShutdown(t *testing.T) {
	rs := newRecordingServer()
	defer rs.srv.Close()

	ctx, cancel := context.WithCancel(context.Background())

	h := &fakeHandle{events: make(chan RunEvent), waitCh: make(chan struct{})}
	adapter := &fakeAdapter{start: func(startCtx context.Context, _ RunSpec) (RunHandle, error) {
		// Simulate the run being killed the moment the worker shuts down.
		go func() {
			<-ctx.Done()
			close(h.events) // ends the pump
			h.err = errors.New("signal: killed")
			close(h.waitCh) // releases Wait
		}()
		return h, nil
	}}

	w := newTestWorker(rs, adapter)
	w.workspaceBase = t.TempDir()

	done := make(chan struct{})
	go func() { defer close(done); w.execute(ctx, testClaim()) }()

	// Let the run reach the pump, then shut the worker down.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("execute did not return after shutdown")
	}

	if rs.hit("release") != 1 {
		t.Errorf("release hits = %d, want 1 (run should be released to the queue on shutdown)", rs.hit("release"))
	}
	if rs.hit("finish") != 0 {
		t.Errorf("finish hits = %d, want 0 (a shutdown run must not be failed)", rs.hit("finish"))
	}
}

// TestExecutePreservesTimeoutDetail: a timed-out run reports StatusTimedOut and
// keeps the parser's underlying error in the finish message instead of
// discarding it for a generic timeout string.
func TestExecutePreservesTimeoutDetail(t *testing.T) {
	rs := newRecordingServer()
	defer rs.srv.Close()

	h := &fakeHandle{
		events: make(chan RunEvent),
		waitCh: make(chan struct{}),
		err:    &timeoutError{detail: errors.New("stuck waiting on tool result")},
	}
	close(h.events) // no events; pump returns immediately
	close(h.waitCh) // Wait returns at once

	adapter := &fakeAdapter{start: func(context.Context, RunSpec) (RunHandle, error) { return h, nil }}
	w := newTestWorker(rs, adapter)
	w.workspaceBase = t.TempDir()

	w.execute(context.Background(), testClaim())

	if rs.hit("finish") != 1 {
		t.Fatalf("finish hits = %d, want 1", rs.hit("finish"))
	}
	rs.mu.Lock()
	body := rs.finishBody
	rs.mu.Unlock()
	if body.Status != agentruns.StatusTimedOut {
		t.Errorf("status = %q, want %q", body.Status, agentruns.StatusTimedOut)
	}
	if !contains(body.Error, "timeout") {
		t.Errorf("error %q should mention the timeout", body.Error)
	}
	if !contains(body.Error, "stuck waiting on tool result") {
		t.Errorf("error %q should preserve the parser's underlying detail", body.Error)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestTimeoutErrorUnwrapsToDeadline: timeoutError must satisfy
// errors.Is(err, context.DeadlineExceeded) so the timed-out branch in execute
// still fires, while carrying the underlying detail.
func TestTimeoutErrorUnwrapsToDeadline(t *testing.T) {
	underlying := errors.New("boom")
	err := error(&timeoutError{detail: underlying})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Error("timeoutError must unwrap to context.DeadlineExceeded")
	}
	if !contains(err.Error(), "boom") {
		t.Errorf("timeoutError.Error() = %q, want it to include the detail", err.Error())
	}

	// Nil detail is fine (timed out before the parser produced an error).
	if got := (&timeoutError{}).Error(); got == "" {
		t.Error("timeoutError with nil detail should still render a message")
	}
}

// TestEventsDroppedMarker: the synthetic marker carries the marker kind and
// the dropped count so a stalled pump's truncation is visible in the log.
func TestEventsDroppedMarker(t *testing.T) {
	ev := eventsDroppedMarker(7)
	if ev.Kind != agentruns.LogMarker {
		t.Errorf("kind = %q, want %q", ev.Kind, agentruns.LogMarker)
	}
	if ev.Payload["marker"] != "events_dropped" {
		t.Errorf("marker = %v, want events_dropped", ev.Payload["marker"])
	}
	if ev.Payload["dropped"] != 7 {
		t.Errorf("dropped = %v, want 7", ev.Payload["dropped"])
	}
}

// TestProvidersRace exercises concurrent readers (snapshotProviders, as the
// claim loop does) against a writer (addProvider, as post-login redetect does).
// Run under -race to catch unsynchronised access.
func TestProvidersRace(t *testing.T) {
	w := &Worker{}
	var wg sync.WaitGroup
	names := []string{"claude-code", "codex-cli", "gemini-cli", "claude-code"}
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(n int) { defer wg.Done(); w.addProvider(names[n%len(names)]) }(i)
		go func() { defer wg.Done(); _ = w.snapshotProviders() }()
	}
	wg.Wait()

	// Dedup held: at most the three distinct names.
	if got := len(w.snapshotProviders()); got != 3 {
		t.Errorf("distinct providers = %d, want 3 (%v)", got, w.snapshotProviders())
	}
}

// TestCleanupOldRemovesOnlyOld: the sweep removes directories older than the
// retention window and leaves fresh ones (and non-directories) untouched.
func TestCleanupOldRemovesOnlyOld(t *testing.T) {
	base := t.TempDir()

	oldDir := filepath.Join(base, "old-run")
	newDir := filepath.Join(base, "new-run")
	for _, d := range []string{oldDir, newDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Age the old directory well past the retention window.
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(oldDir, old, old); err != nil {
		t.Fatal(err)
	}

	CleanupOld(base, 24*time.Hour)

	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Errorf("old workspace should have been removed, stat err = %v", err)
	}
	if _, err := os.Stat(newDir); err != nil {
		t.Errorf("fresh workspace should have survived, stat err = %v", err)
	}
}
