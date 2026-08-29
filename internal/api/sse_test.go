package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// canceledSSERequest returns a request whose context is already done, so
// ServeStream performs the replay phase and returns without blocking.
func canceledSSERequest() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/stream", nil)
	ctx, cancel := context.WithCancel(r.Context())
	cancel()
	return r.WithContext(ctx)
}

// TestServeStreamReplayFailureIsGeneric locks in that a failing replay emits
// a generic error event: the real error (which may carry SQL text or file
// paths, and some streams are public) must only reach the server log.
func TestServeStreamReplayFailureIsGeneric(t *testing.T) {
	const internalDetail = "pq: password authentication failed for user openv"
	hub := NewSSEHub()
	w := httptest.NewRecorder()

	hub.ServeStream(w, canceledSSERequest(), "interview:sess-1", func(emit func(event string, data interface{})) error {
		return errors.New(internalDetail)
	})

	body := w.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Fatalf("stream %q is missing the error event", body)
	}
	if !strings.Contains(body, "stream unavailable") {
		t.Fatalf("stream %q is missing the generic error message", body)
	}
	if strings.Contains(body, "password") {
		t.Fatalf("stream %q leaks the internal error text", body)
	}
}

// TestServeStreamKeepalive locks in that an idle stream emits SSE comment
// lines so NATs and load balancers don't drop the connection.
func TestServeStreamKeepalive(t *testing.T) {
	old := sseKeepaliveInterval
	sseKeepaliveInterval = 5 * time.Millisecond
	defer func() { sseKeepaliveInterval = old }()

	hub := NewSSEHub()
	r := httptest.NewRequest(http.MethodGet, "/stream", nil)
	ctx, cancel := context.WithTimeout(r.Context(), 100*time.Millisecond)
	defer cancel()
	w := httptest.NewRecorder()

	hub.ServeStream(w, r.WithContext(ctx), "interview:sess-1", nil)

	if !strings.Contains(w.Body.String(), ": keepalive\n\n") {
		t.Fatalf("idle stream %q emitted no keepalive comment", w.Body.String())
	}
}
