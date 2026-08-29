package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
)

// sseKeepaliveInterval is how often an idle SSE stream emits a comment line
// so NATs and load balancers don't drop the connection as dead. A var (not a
// const) so tests can shrink it.
var sseKeepaliveInterval = 25 * time.Second

// sseEvent is one server-sent event frame.
type sseEvent struct {
	Event string
	Data  interface{}
}

// SSEHub fans run log/status updates out to connected EventSource clients.
// It implements agentruns.Subscriber.
type SSEHub struct {
	mu      sync.Mutex
	streams map[string][]chan sseEvent // run id -> listener channels
}

// NewSSEHub creates an empty hub.
func NewSSEHub() *SSEHub {
	return &SSEHub{streams: map[string][]chan sseEvent{}}
}

// RunLogsAppended broadcasts new log entries for a run.
func (h *SSEHub) RunLogsAppended(run *agentruns.Run, entries []agentruns.LogEntry) {
	for _, e := range entries {
		h.broadcast(run.ID, sseEvent{Event: "log", Data: e})
	}
}

// RunStatusChanged broadcasts a run status transition.
func (h *SSEHub) RunStatusChanged(run *agentruns.Run) {
	h.broadcast(run.ID, sseEvent{Event: "status", Data: map[string]interface{}{
		"run_id": run.ID,
		"status": run.Status,
	}})
}

// BroadcastSession sends an event on an arbitrary stream key (used for
// interview sessions, which reuse the same SSE plumbing).
func (h *SSEHub) BroadcastSession(key string, event string, data interface{}) {
	h.broadcast(key, sseEvent{Event: event, Data: data})
}

func (h *SSEHub) broadcast(key string, e sseEvent) {
	h.mu.Lock()
	listeners := make([]chan sseEvent, len(h.streams[key]))
	copy(listeners, h.streams[key])
	h.mu.Unlock()
	for _, ch := range listeners {
		select {
		case ch <- e:
		default: // slow client; drop rather than block
		}
	}
}

func (h *SSEHub) subscribe(key string) chan sseEvent {
	ch := make(chan sseEvent, 64)
	h.mu.Lock()
	h.streams[key] = append(h.streams[key], ch)
	h.mu.Unlock()
	return ch
}

func (h *SSEHub) unsubscribe(key string, ch chan sseEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	listeners := h.streams[key]
	for i, c := range listeners {
		if c == ch {
			h.streams[key] = append(listeners[:i], listeners[i+1:]...)
			break
		}
	}
	if len(h.streams[key]) == 0 {
		delete(h.streams, key)
	}
}

// ServeStream writes an SSE stream for the given key. replay is invoked
// first to emit any historical events; then live events flow until the
// client disconnects.
func (h *SSEHub) ServeStream(w http.ResponseWriter, r *http.Request, key string, replay func(emit func(event string, data interface{})) error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	write := func(event string, data interface{}) {
		payload, err := json.Marshal(data)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
	}

	// Subscribe before replay so no events are missed in between.
	ch := h.subscribe(key)
	defer h.unsubscribe(key, ch)

	if replay != nil {
		if err := replay(write); err != nil {
			// The real error may carry internals (SQL text, file paths) and
			// some streams are public — log it, send a generic event.
			slog.Error("sse replay failed", "key", key, "error", err)
			write("error", map[string]string{"error": "stream unavailable"})
		}
	}
	flusher.Flush()

	// Periodic SSE comment lines keep idle streams (e.g. an interview waiting
	// on a slow participant) alive through NATs/LBs; comments are ignored by
	// EventSource clients. The ticker stops with the connection.
	keepalive := time.NewTicker(sseKeepaliveInterval)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case e := <-ch:
			write(e.Event, e.Data)
			flusher.Flush()
		}
	}
}
