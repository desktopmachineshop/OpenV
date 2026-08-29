package runner

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
)

// TestFinishRetriesTransientFailures: a Finish that hits a couple of 5xx blips
// still lands once the API recovers, so the run's terminal report is never
// dropped on a transient failure.
func TestFinishRetriesTransientFailures(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "worker-key")
	if err := c.Finish("run-1", agentruns.FinishRequest{
		Status:     agentruns.StatusFailed,
		Error:      "boom",
		ErrorClass: agentruns.ErrorClassAgentError,
	}); err != nil {
		t.Fatalf("Finish after transient 5xx = %v, want nil", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("server saw %d calls, want 3 (2 failures + 1 success)", got)
	}
}

// TestFinishGivesUpAfterExhaustingRetries: when the API never recovers, Finish
// returns the last error rather than a nil-response panic.
func TestFinishGivesUpAfterExhaustingRetries(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "still down", http.StatusBadGateway)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "worker-key")
	if err := c.Finish("run-1", agentruns.FinishRequest{Status: agentruns.StatusFailed}); err == nil {
		t.Fatal("Finish against a persistently failing API = nil, want error")
	}
	// 1 initial attempt + transientRetries retries.
	if got, want := atomic.LoadInt32(&calls), int32(transientRetries+1); got != want {
		t.Errorf("server saw %d calls, want %d", got, want)
	}
}

// TestFinishDoesNotRetryClientErrors: a 4xx (e.g. a 409 already-finished
// conflict) is a definitive answer — it must be surfaced immediately, not
// retried.
func TestFinishDoesNotRetryClientErrors(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "already finished", http.StatusConflict)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "worker-key")
	if err := c.Finish("run-1", agentruns.FinishRequest{Status: agentruns.StatusSucceeded}); err == nil {
		t.Fatal("Finish with a 409 = nil, want error")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server saw %d calls, want 1 (no retry on 4xx)", got)
	}
}
