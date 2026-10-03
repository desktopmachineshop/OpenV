package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/providers"
)

// A successful sign-in adds the provider to the providers the next claim
// reports (refactor plan step S15a). Every sign-in flow ends in
// Worker.redetect, which re-runs the adapter's Detect, reports it, and adds
// the provider to the list the claim loop sends (Worker.addProvider,
// snapshotProviders in tryClaim). Refactor step M15b moves the sign-in
// methods to a loginBroker; these tests drive only Worker.Run, a stand-in
// API and stand-in vendor CLIs, so they hold across that move, for each of
// the four flows: the piped one (codex on a personal runner), the console
// one (claude on a personal runner), the pseudo-terminal one (claude on a
// headless runner) and the loopback one (codex on a headless runner). They
// wait on the worker's 3-second sign-in and 2-second claim ticks, so they
// skip under -short.

const (
	standInCodex = `case "$1" in
--version) echo "codex-cli 0.0.0-stand-in" ;;
login)
  echo "Starting local login server on http://localhost:1455."
  echo "If your browser did not open, navigate to this URL to authenticate:"
  echo "https://auth.openai.com/oauth/authorize?client_id=stand-in&redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback"
  echo "Successfully logged in" ;;
*) exit 2 ;;
esac
`
	standInClaude = `case "$1" in
--version) echo "9.9.9 (Claude Code)" ;;
auth)
  case "$2" in
  status) echo '{"loggedIn": true, "authMethod": "claude.ai"}' ;;
  login) : ;;
  *) exit 2 ;;
  esac ;;
*) exit 2 ;;
esac
`
)

// signInWorld is the stand-in API's sign-in side and the PATH the vendor
// CLIs are installed into while the worker runs.
type signInWorld struct {
	t       *testing.T
	bin     string
	staging string
	api     *fakeAPI

	mu     sync.Mutex
	logins []string
}

func newSignInWorld(t *testing.T) *signInWorld {
	t.Helper()
	skipWithoutPOSIXShell(t)
	if testing.Short() {
		t.Skip("waits on the worker's 3-second sign-in and 2-second claim ticks")
	}
	s := &signInWorld{t: t, bin: t.TempDir(), staging: t.TempDir()}
	// The stand-ins are written before the worker starts any process and
	// renamed into PATH later, so no exec can meet a file still open for
	// writing.
	writeScript(t, s.staging, "codex", standInCodex)
	writeScript(t, s.staging, "claude", standInClaude)
	isolateRunnerEnv(t, s.bin)
	t.Setenv("HOME", t.TempDir())
	s.api = newFakeAPI(t, s.respond)
	return s
}

func (s *signInWorld) respond(c apiCall) (int, string) {
	if !c.is("POST", "/provider-logins/claim") {
		return 0, ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.logins) == 0 {
		return http.StatusNoContent, ""
	}
	login := s.logins[0]
	s.logins = s.logins[1:]
	return http.StatusOK, login
}

// install puts a stand-in CLI on the worker's PATH.
func (s *signInWorld) install(name string) {
	s.t.Helper()
	if err := os.Rename(filepath.Join(s.staging, name), filepath.Join(s.bin, name)); err != nil {
		s.t.Fatalf("install the stand-in %s: %v", name, err)
	}
}

// requestSignIn queues a sign-in request for the worker's next poll.
func (s *signInWorld) requestSignIn(id, provider string) {
	raw, err := json.Marshal(providers.LoginRequest{ID: id, OrgID: "org-1", Provider: provider,
		Target: providers.LoginTargetWorkspace, Status: providers.LoginClaimed})
	if err != nil {
		s.t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logins = append(s.logins, string(raw))
}

type loginProgress struct {
	Status    string `json:"status"`
	AuthURL   string `json:"auth_url"`
	Detail    string `json:"detail"`
	PasteKind string `json:"paste_kind"`
}

// progressOf returns a sign-in's progress reports in order.
func (s *signInWorld) progressOf(id string, calls []apiCall) ([]loginProgress, []apiCall) {
	var out []loginProgress
	var at []apiCall
	for _, c := range calls {
		if c.is("POST", "/provider-logins/"+id+"/progress") {
			var p loginProgress
			c.json(s.t, &p)
			out = append(out, p)
			at = append(at, c)
		}
	}
	return out, at
}

// awaitOutcome waits for a sign-in's terminal progress report and returns
// every report it made and the request that carried the last.
func (s *signInWorld) awaitOutcome(id string) ([]loginProgress, apiCall) {
	s.t.Helper()
	calls := s.api.await(s.t, 20*time.Second, "sign-in "+id+" to complete or fail", func(calls []apiCall) bool {
		got, _ := s.progressOf(id, calls)
		if len(got) == 0 {
			return false
		}
		last := got[len(got)-1].Status
		return last == providers.LoginCompleted || last == providers.LoginFailed
	})
	got, at := s.progressOf(id, calls)
	return got, at[len(at)-1]
}

func (s *signInWorld) start(opts Options) (context.CancelFunc, <-chan error) {
	s.t.Helper()
	w := NewWorker(NewClient(s.api.URL(), "worker-key"), opts)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	// The first detection finds no vendor CLI at all.
	isReport := func(c apiCall) bool { return c.is("POST", "/provider-settings/detect") }
	calls := s.api.await(s.t, 15*time.Second, "the worker's first detection report", func(calls []apiCall) bool {
		return count(calls, isReport) > 0
	})
	for _, c := range calls {
		if isReport(c) && strings.Contains(string(c.Body), `"installed":true`) {
			s.t.Fatalf("the first detection report found a vendor CLI with none on PATH: %s", c.Body)
		}
	}
	return cancel, done
}

// raceWindow is how soon after the post-sign-in detection report a claim may
// still carry the old providers: the worker adds the provider once the
// report's answer is back, and a claim tick can fall in between.
const raceWindow = 200 * time.Millisecond

// checkAddedBySignIn holds what follows a completed sign-in to the provider
// being added: the next detection report is that provider's alone, and
// installed, and the next claim reports want.
func (s *signInWorld) checkAddedBySignIn(id, provider string, completed apiCall, want []string) {
	s.t.Helper()
	isReport := func(c apiCall) bool { return c.is("POST", "/provider-settings/detect") && c.Seq > completed.Seq }
	calls := s.api.await(s.t, 15*time.Second, "the detection report after sign-in "+id, func(calls []apiCall) bool {
		return count(calls, isReport) > 0
	})
	var report apiCall
	for _, c := range calls {
		if isReport(c) {
			report = c
			break
		}
	}
	var detected map[string]map[string]interface{}
	report.json(s.t, &detected)
	if len(detected) != 1 || detected[provider]["installed"] != true {
		s.t.Errorf("the detection report after sign-in %s = %s, want %s alone, installed", id, report.Body, provider)
	}

	isNext := func(c apiCall) bool { return isClaim(c) && c.Seq > report.Seq }
	for skip := 0; ; skip++ {
		claims := s.api.await(s.t, 15*time.Second, "a claim after sign-in "+id+" added "+provider, func(calls []apiCall) bool {
			return count(calls, isNext) > skip
		})
		var next []apiCall
		for _, c := range claims {
			if isNext(c) {
				next = append(next, c)
			}
		}
		claim := next[skip]
		var body claimBody
		claim.json(s.t, &body)
		got := strings.Join(body.Providers, ",")
		if got == strings.Join(want, ",") {
			return
		}
		if claim.At.Sub(report.At) < raceWindow && !contains(got, provider) {
			continue // the tick fell before the worker had the report's answer
		}
		s.t.Errorf("the first claim after sign-in %s (%s) reported providers %v, want %v", id, provider, body.Providers, want)
		return
	}
}

// TestSignInAddsTheProviderToTheNextClaim, on a personal runner: a sign-in
// for a CLI that is not installed fails and adds nothing, so the worker
// still claims nothing; once the CLI is installed, a piped sign-in (codex)
// and a console one (claude) each add their provider to the next claim.
func TestSignInAddsTheProviderToTheNextClaim(t *testing.T) {
	s := newSignInWorld(t)
	cancel, done := s.start(Options{WorkerID: "w-signin", Concurrency: 1, WorkspaceBase: t.TempDir()})
	defer cancel()

	s.requestSignIn("l1", providers.ProviderCodexCLI)
	got, _ := s.awaitOutcome("l1")
	want := []loginProgress{{Status: providers.LoginFailed,
		Detail: `the "codex" CLI is not installed on the worker host — install it first, then retry`}}
	if !sameProgress(got, want) {
		t.Errorf("sign-in l1 (codex not installed) reported %+v, want %+v", got, want)
	}

	s.install("codex")
	s.requestSignIn("l2", providers.ProviderCodexCLI)
	got, completed := s.awaitOutcome("l2")
	want = []loginProgress{
		{Status: providers.LoginURLReady,
			AuthURL: "https://auth.openai.com/oauth/authorize?client_id=stand-in&redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback",
			Detail:  "A sign-in page should have opened in a browser on the machine running agentd. Complete sign-in there; this page updates automatically."},
		{Status: providers.LoginCompleted, Detail: "Signed in successfully."},
	}
	if !sameProgress(got, want) {
		t.Errorf("sign-in l2 (codex, piped) reported %+v, want %+v", got, want)
	}
	if n := count(s.api.Calls(), func(c apiCall) bool { return isClaim(c) && c.Seq < completed.Seq }); n != 0 {
		t.Errorf("the worker claimed %d time(s) before any provider was signed in, want none", n)
	}
	s.checkAddedBySignIn("l2", providers.ProviderCodexCLI, completed, []string{providers.ProviderCodexCLI})

	s.install("claude")
	s.requestSignIn("l3", providers.ProviderClaudeCode)
	got, completed = s.awaitOutcome("l3")
	want = []loginProgress{
		{Status: providers.LoginURLReady,
			Detail: "A sign-in terminal has opened on the machine running agentd — complete the sign-in there. This page updates automatically when it finishes."},
		{Status: providers.LoginCompleted, Detail: "Signed in successfully."},
	}
	if !sameProgress(got, want) {
		t.Errorf("sign-in l3 (claude, console) reported %+v, want %+v", got, want)
	}
	s.checkAddedBySignIn("l3", providers.ProviderClaudeCode, completed,
		[]string{providers.ProviderCodexCLI, providers.ProviderClaudeCode})

	cancel()
	<-done
}

// TestHeadlessSignInAddsTheProviderToTheNextClaim, on a headless runner (a
// leased pool node): a pseudo-terminal sign-in (claude) and a loopback one
// (codex) each add their provider to the next claim.
func TestHeadlessSignInAddsTheProviderToTheNextClaim(t *testing.T) {
	s := newSignInWorld(t)
	if !ptySupported {
		t.Skip("this platform has no pseudo-terminal sign-in")
	}
	cancel, done := s.start(Options{WorkerID: "w-headless", Concurrency: 1, Headless: true, WorkspaceBase: t.TempDir()})
	defer cancel()

	s.install("claude")
	s.requestSignIn("h1", providers.ProviderClaudeCode)
	got, completed := s.awaitOutcome("h1")
	if last := got[len(got)-1]; last.Status != providers.LoginCompleted || last.Detail != "Signed in successfully." {
		t.Errorf("sign-in h1 (claude, pseudo-terminal) ended %+v, want completed", last)
	}
	s.checkAddedBySignIn("h1", providers.ProviderClaudeCode, completed, []string{providers.ProviderClaudeCode})

	s.install("codex")
	s.requestSignIn("h2", providers.ProviderCodexCLI)
	got, completed = s.awaitOutcome("h2")
	if last := got[len(got)-1]; last.Status != providers.LoginCompleted || last.Detail != "Signed in successfully." {
		t.Errorf("sign-in h2 (codex, loopback) ended %+v, want completed", last)
	}
	s.checkAddedBySignIn("h2", providers.ProviderCodexCLI, completed,
		[]string{providers.ProviderClaudeCode, providers.ProviderCodexCLI})

	cancel()
	<-done
}

func sameProgress(got, want []loginProgress) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
