package runner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// The scripted API, CLIs and adapter the runner characterization tests
// (refactor plan step S15a) drive the runner with. Every test that uses them
// runs the runner's real loops — Worker.Run, its claim and sign-in loops,
// PoolAgent's leases — against a stand-in for the OpenV API, so what they
// pin is what the runner sends, not how its functions are cut.

// apiCall is one request the runner made, in arrival order.
type apiCall struct {
	Seq    int
	At     time.Time
	Method string
	Path   string
	Auth   string // the credential, without "Bearer "
	Body   []byte
}

// is reports whether the call is method on a path ending in suffix.
func (c apiCall) is(method, suffix string) bool {
	return c.Method == method && strings.HasSuffix(c.Path, suffix)
}

// json decodes the call's body into v, failing the test if it does not decode.
func (c apiCall) json(t *testing.T, v interface{}) {
	t.Helper()
	if err := json.Unmarshal(c.Body, v); err != nil {
		t.Fatalf("%s %s: body %q does not decode: %v", c.Method, c.Path, c.Body, err)
	}
}

// claimBody is what Client.Claim sends.
type claimBody struct {
	WorkerID    string   `json:"worker_id"`
	Providers   []string `json:"providers"`
	MinPriority int      `json:"min_priority"`
	Hosted      bool     `json:"hosted"`
}

// fakeAPI is a stand-in for the OpenV API. Each request is recorded, then
// answered by the test's responder; a responder that returns status 0 leaves
// the request to defaultAPIAnswer.
type fakeAPI struct {
	srv     *httptest.Server
	respond func(c apiCall) (int, string)

	mu    sync.Mutex
	calls []apiCall
}

func newFakeAPI(t *testing.T, respond func(c apiCall) (int, string)) *fakeAPI {
	t.Helper()
	a := &fakeAPI{respond: respond}
	a.srv = httptest.NewServer(http.HandlerFunc(a.serve))
	t.Cleanup(a.srv.Close)
	return a
}

func (a *fakeAPI) URL() string { return a.srv.URL }

func (a *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	a.mu.Lock()
	c := apiCall{
		Seq:    len(a.calls),
		At:     time.Now(),
		Method: r.Method,
		Path:   r.URL.Path,
		Auth:   strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "),
		Body:   body,
	}
	a.calls = append(a.calls, c)
	a.mu.Unlock()

	status, out := 0, ""
	if a.respond != nil {
		status, out = a.respond(c)
	}
	if status == 0 {
		status, out = defaultAPIAnswer(c)
	}
	if out != "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, out)
}

// defaultAPIAnswer is what the API answers when a test does not script the
// request: an empty queue, no sign-in waiting, and every report accepted.
func defaultAPIAnswer(c apiCall) (int, string) {
	switch {
	case c.is("POST", "/agent-runs/claim"), c.is("POST", "/provider-logins/claim"):
		return http.StatusNoContent, ""
	case c.is("POST", "/start"), c.is("POST", "/release"):
		return http.StatusNoContent, ""
	case c.is("POST", "/logs"):
		return http.StatusOK, `{"cancel_requested":false,"status":"running"}`
	case c.is("GET", "/repo-connections"):
		return http.StatusOK, "null"
	case c.is("POST", "/heartbeat"):
		return http.StatusOK, `{"assignment":null}`
	case c.is("POST", "/runner-pool/nodes"):
		return http.StatusCreated, `{"id":"node-1","name":"","pool":"","status":"idle"}`
	case c.is("GET", "/full"):
		return http.StatusOK, `{"id":"","status":"claimed"}`
	}
	return http.StatusOK, "{}"
}

// Calls returns a copy of every request so far.
func (a *fakeAPI) Calls() []apiCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]apiCall(nil), a.calls...)
}

// match returns the calls for which keep is true, in order.
func (a *fakeAPI) match(keep func(apiCall) bool) []apiCall {
	var out []apiCall
	for _, c := range a.Calls() {
		if keep(c) {
			out = append(out, c)
		}
	}
	return out
}

// await polls until cond holds over the calls so far, and fails the test
// naming what it waited for if that takes longer than timeout.
func (a *fakeAPI) await(t *testing.T, timeout time.Duration, what string, cond func([]apiCall) bool) []apiCall {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		calls := a.Calls()
		if cond(calls) {
			return calls
		}
		if time.Now().After(deadline) {
			t.Fatalf("waited %s for %s; the runner made %d request(s):\n%s", timeout, what, len(calls), describeCalls(calls))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// describeCalls renders calls for a failure message.
func describeCalls(calls []apiCall) string {
	var b strings.Builder
	for _, c := range calls {
		b.WriteString("  ")
		b.WriteString(c.Method + " " + c.Path + " [" + c.Auth + "]")
		if len(c.Body) > 0 {
			body := string(c.Body)
			if len(body) > 200 {
				body = body[:200] + "..."
			}
			b.WriteString(" " + body)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// count returns how many calls satisfy keep.
func count(calls []apiCall, keep func(apiCall) bool) int {
	n := 0
	for _, c := range calls {
		if keep(c) {
			n++
		}
	}
	return n
}

// isClaim selects run claims, and isClaimAt those of one pool.
func isClaim(c apiCall) bool { return c.is("POST", "/api/v1/agent-runs/claim") }

func isClaimAt(t *testing.T, minPriority int) func(apiCall) bool {
	return func(c apiCall) bool {
		if !isClaim(c) {
			return false
		}
		var body claimBody
		c.json(t, &body)
		return body.MinPriority == minPriority
	}
}

// runIDOf returns the run id of a call on /api/v1/agent-runs/{id}/<verb>.
func runIDOf(c apiCall) string {
	rest := strings.TrimPrefix(c.Path, "/api/v1/agent-runs/")
	if rest == c.Path {
		return ""
	}
	id, _, _ := strings.Cut(rest, "/")
	return id
}

// --- Vendor CLIs as shell scripts ---

// skipWithoutPOSIXShell skips a test that stands in for a vendor CLI with a
// shell script.
func skipWithoutPOSIXShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in vendor CLIs are POSIX shell scripts")
	}
}

// writeScript writes an executable shell script. The scripts use shell
// builtins only, so they work with PATH holding nothing but their directory.
func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write the stand-in %s: %v", name, err)
	}
	return path
}

// isolateRunnerEnv gives a test a PATH of only bin (so the adapters find no
// real vendor CLI) and clears the provider keys their detection and the
// worker's api-key path read. It sets the variables with t.Setenv, which
// restores them when the test ends.
func isolateRunnerEnv(t *testing.T, bin string) {
	t.Helper()
	t.Setenv("PATH", bin)
	clearProviderKeys(t)
}

// clearProviderKeys empties the provider key variables (and the CLI config
// locations detection reads) for the length of the test.
func clearProviderKeys(t *testing.T) {
	t.Helper()
	for _, name := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY",
		"CODEX_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(name, "")
	}
}

// --- A scripted adapter ---

// scriptedAdapter is an Adapter whose detection and runs a test decides.
type scriptedAdapter struct {
	name      string
	installed bool
	start     func(ctx context.Context, spec RunSpec) (RunHandle, error)
}

func (a *scriptedAdapter) Name() string { return a.name }
func (a *scriptedAdapter) Detect(context.Context) Availability {
	if !a.installed {
		return Availability{Detail: a.name + " is not installed (stand-in)"}
	}
	return Availability{Installed: true, Version: "0.0.0-stand-in", LoggedIn: true}
}
func (a *scriptedAdapter) Start(ctx context.Context, spec RunSpec) (RunHandle, error) {
	return a.start(ctx, spec)
}

// heldRun is a RunHandle that stays running until the test ends it, the
// member cancels it (the pump's Cancel), or the worker's context ends (a
// shutdown kills the CLI).
type heldRun struct {
	events chan RunEvent
	done   chan struct{}
	once   sync.Once

	mu     sync.Mutex
	result Result
	err    error
	panics bool
}

func newHeldRun(ctx context.Context) *heldRun {
	h := &heldRun{events: make(chan RunEvent), done: make(chan struct{})}
	go func() {
		select {
		case <-ctx.Done():
			h.end(Result{ExitCode: -1}, errorString("signal: killed"), false)
		case <-h.done:
		}
	}()
	return h
}

// end finishes the run: its events close and Wait returns result and err,
// or panics when panics is set. Only the first end counts.
func (h *heldRun) end(result Result, err error, panics bool) {
	h.once.Do(func() {
		h.mu.Lock()
		h.result, h.err, h.panics = result, err, panics
		h.mu.Unlock()
		close(h.events)
		close(h.done)
	})
}

func (h *heldRun) Events() <-chan RunEvent { return h.events }
func (h *heldRun) Cancel()                 { h.end(Result{ExitCode: -1}, errorString("signal: killed"), false) }
func (h *heldRun) Wait() (Result, error) {
	<-h.done
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.panics {
		panic("stand-in CLI handle panicked in Wait")
	}
	return h.result, h.err
}

// finishedRun is a RunHandle that has already ended with result and err.
func finishedRun(result Result, err error) RunHandle {
	h := &heldRun{events: make(chan RunEvent), done: make(chan struct{})}
	h.end(result, err, false)
	return h
}

// errorString is an error with fixed text, so a golden can quote it.
type errorString string

func (e errorString) Error() string { return string(e) }
