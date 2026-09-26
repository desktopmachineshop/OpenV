package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The SSE wire contract (refactor plan step S6, invariant I9), pinned in
// contracts/sse-events.json at the repository root:
//
//   - events: every event name the server sends, read from the Go sources at
//     the four places a name enters a stream (the literals passed to
//     BroadcastSession, to a replay's emit, in sseEvent{Event: ...}, and to
//     ServeStream's own write closure), each with the stream keys it is sent
//     on;
//   - streams: the stream keys ServeStream serves, and whether each replays
//     history first;
//   - headers, frame, keepalive and replay_failure: what a live SSEHub
//     writes, observed through httptest.
//
// The frontend's EventSource listeners are checked against the same file by
// frontend/src/arch/sseListeners.test.ts, so a renamed event fails in Go or
// in TS whichever side changes. Only UPDATE_GOLDEN=1 rewrites the file; any
// other value compares.

const sseContractFile = "contracts/sse-events.json"

// s6UpdateEnv set to exactly 1 rewrites this step's goldens (the SSE contract
// and testdata/event_payload_types.txt) instead of comparing against them.
const s6UpdateEnv = "UPDATE_GOLDEN"

func s6Updating() bool { return os.Getenv(s6UpdateEnv) == "1" }

// s6Regenerate is the one command that rewrites the golden a test owns.
func s6Regenerate(test string) string {
	return s6UpdateEnv + "=1 go test ./internal/api -count=1 -run '^" + test + "$'" +
		"\n(only " + s6UpdateEnv + "=1 regenerates; any other value compares)"
}

// checkS6Golden compares got with the file at path (relative to this
// package directory), or rewrites the file under UPDATE_GOLDEN=1. shown is
// how failure messages name the file.
func checkS6Golden(t *testing.T, path, shown string, got []byte, test, meaning string) {
	t.Helper()
	if s6Updating() {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", shown, err)
		}
		t.Logf("regenerated %s", shown)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v\nCreate it with:\n  %s", shown, err, s6Regenerate(test))
	}
	if bytes.Equal(want, got) {
		return
	}
	t.Fatalf("%s does not match the code; first differences (- pinned, + current, by line):\n%s\n"+
		"%s A refactor never changes this file. If the change is deliberate, regenerate it with:\n  %s",
		shown, strings.Join(lineDiff(string(want), string(got), 20), "\n"), meaning, s6Regenerate(test))
}

// sseContract is the document contracts/sse-events.json holds.
type sseContract struct {
	About             string            `json:"about"`
	Events            []sseContractName `json:"events"`
	Streams           []sseStream       `json:"streams"`
	Headers           map[string]string `json:"headers"`
	Frame             string            `json:"frame"`
	Keepalive         string            `json:"keepalive"`
	KeepaliveInterval string            `json:"keepalive_interval"`
	ReplayFailure     string            `json:"replay_failure"`
}

type sseContractName struct {
	Name    string   `json:"name"`
	Streams []string `json:"streams"`
}

type sseStream struct {
	Key    string `json:"key"`
	Replay bool   `json:"replay"`
}

const sseContractAbout = "SSE wire contract (refactor plan S6, invariant I9): the event names the server sends and the " +
	"stream keys each goes on (<id> is the variable part; a bare <id> is an agent run's stream), the streams " +
	"ServeStream serves, and the bytes and headers a stream writes. Written by TestSSEContract " +
	"(internal/api/sse_contract_test.go); frontend/src/arch/sseListeners.test.ts checks every EventSource " +
	"listener against it. Regenerate only for a deliberate change: UPDATE_GOLDEN=1 go test ./internal/api -count=1 " +
	"-run '^TestSSEContract$'"

// TestSSEContract builds the contract from the sources and a live hub and
// compares it with contracts/sse-events.json.
func TestSSEContract(t *testing.T) {
	scan := scanSSESources(t)
	contract := sseContract{
		About:             sseContractAbout,
		Events:            scan.contractEvents(),
		Streams:           scan.contractStreams(),
		Headers:           observeSSEHeaders(t),
		Frame:             observeSSEFrame(t),
		Keepalive:         observeSSEKeepalive(t),
		KeepaliveInterval: sseKeepaliveInterval.String(),
		ReplayFailure:     observeSSEReplayFailure(t),
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(contract); err != nil {
		t.Fatal(err)
	}
	checkS6Golden(t, filepath.Join(scan.root, filepath.FromSlash(sseContractFile)), sseContractFile, buf.Bytes(),
		"TestSSEContract",
		"EventSource clients listen by event name and resume by stream; the frontend and deployed browsers depend on "+
			"every name, key, header and byte here.")
}

// TestSSEEventsGoToServedStreams: every stream key an event is sent on is one
// that ServeStream serves, so nothing is broadcast into a key no client can
// open (a prefix renamed on one side only).
func TestSSEEventsGoToServedStreams(t *testing.T) {
	scan := scanSSESources(t)
	served := map[string]bool{}
	for _, s := range scan.served {
		served[s.Key] = true
	}
	for _, site := range scan.sites {
		for _, stream := range site.streams {
			if !served[stream] {
				t.Errorf("%s sends %q on stream %q, which no ServeStream call serves (served: %v)",
					site.pos, site.event, stream, sortedKeys(served))
			}
		}
	}
}

// TestSSEScanSeesEverySite keeps the scanner honest: each of the four ways a
// name enters a stream is found at least once, as it is today.
func TestSSEScanSeesEverySite(t *testing.T) {
	scan := scanSSESources(t)
	counts := map[string]int{}
	for _, site := range scan.sites {
		counts[site.how]++
	}
	for _, how := range []string{"BroadcastSession", "emit", "sseEvent", "write"} {
		if counts[how] == 0 {
			t.Errorf("the scan found no %s site: it has gone blind (sites found: %v)", how, counts)
		}
	}
	if len(scan.served) == 0 {
		t.Error("the scan found no ServeStream call")
	}
}

// TestServeStreamSendsErrorOnlyWhenReplayFails forces the one path that sends
// the hub's own event: a replay that fails ends in exactly the generic error
// frame, and a replay that succeeds sends only what it emitted.
func TestServeStreamSendsErrorOnlyWhenReplayFails(t *testing.T) {
	failing := serveOnce(t, func(emit func(event string, data interface{})) error {
		emit("message", map[string]string{"id": "m-1"})
		return errors.New("pq: relation \"messages\" does not exist")
	})
	want := "event: message\ndata: {\"id\":\"m-1\"}\n\n" + "event: error\ndata: {\"error\":\"stream unavailable\"}\n\n"
	if failing != want {
		t.Errorf("a failed replay wrote %q, want %q", failing, want)
	}

	ok := serveOnce(t, func(emit func(event string, data interface{})) error {
		emit("message", map[string]string{"id": "m-1"})
		return nil
	})
	if ok != "event: message\ndata: {\"id\":\"m-1\"}\n\n" {
		t.Errorf("a successful replay wrote %q; it must send no error event", ok)
	}

	if none := serveOnce(t, nil); none != "" {
		t.Errorf("a stream without replay wrote %q before any event, want nothing", none)
	}
}

// ---- Observing a live hub ----

// serveOnce runs ServeStream to the end of its replay: the request's context
// is already cancelled, so the live loop returns at once.
func serveOnce(t *testing.T, replay func(emit func(event string, data interface{})) error) string {
	t.Helper()
	w := httptest.NewRecorder()
	NewSSEHub().ServeStream(w, canceledSSERequest(), "interview:s6", replay)
	return w.Body.String()
}

func observeSSEHeaders(t *testing.T) map[string]string {
	t.Helper()
	w := httptest.NewRecorder()
	NewSSEHub().ServeStream(w, canceledSSERequest(), "notify:s6", nil)
	out := map[string]string{}
	for name, values := range w.Result().Header {
		out[name] = strings.Join(values, ", ")
	}
	return out
}

// observeSSEFrame renders one event and writes it back as a template.
func observeSSEFrame(t *testing.T) string {
	t.Helper()
	body := serveOnce(t, func(emit func(event string, data interface{})) error {
		emit("s6-name", "s6-data")
		return nil
	})
	if strings.Count(body, "s6-name") != 1 || strings.Count(body, `"s6-data"`) != 1 {
		t.Fatalf("one emitted event wrote %q", body)
	}
	return strings.NewReplacer("s6-name", "<name>", `"s6-data"`, "<json>").Replace(body)
}

// observeSSEKeepalive lets an idle stream tick and returns the comment it
// repeats; anything else on an idle stream fails.
func observeSSEKeepalive(t *testing.T) string {
	t.Helper()
	old := sseKeepaliveInterval
	sseKeepaliveInterval = 2 * time.Millisecond
	defer func() { sseKeepaliveInterval = old }()

	r := httptest.NewRequest(http.MethodGet, "/stream", nil)
	ctx, cancel := context.WithTimeout(r.Context(), 200*time.Millisecond)
	defer cancel()
	w := httptest.NewRecorder()
	NewSSEHub().ServeStream(w, r.WithContext(ctx), "notify:s6", nil)

	body := w.Body.String()
	end := strings.Index(body, "\n\n")
	if end < 0 {
		t.Fatalf("an idle stream wrote %q: no keepalive in 200ms at a 2ms interval", body)
	}
	unit := body[:end+2]
	if !strings.HasPrefix(unit, ":") || strings.Repeat(unit, strings.Count(body, unit)) != body {
		t.Fatalf("an idle stream wrote %q; want only repeated SSE comment lines", body)
	}
	return unit
}

func observeSSEReplayFailure(t *testing.T) string {
	t.Helper()
	return serveOnce(t, func(func(event string, data interface{})) error {
		return errors.New("replay failed")
	})
}
