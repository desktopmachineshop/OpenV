package runner

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/providers"
)

// The console flow (loginBroker.handleInteractiveLogin: claude, or gemini,
// on a personal runner) opens the CLI's sign-in in a terminal on the
// runner's machine and follows it. How it ends when the terminal cannot be
// opened, when the member cancels and when the sign-in runs out of time had
// no test, so its messages could change unseen (#379 bug 124). These tests
// drive it through loginBroker.handleLogin, as the sign-in loop does, with
// a stand-in claude, and compare every progress report it sent.

// consoleOpened is the console flow's report once the terminal is open.
var consoleOpened = loginProgress{Status: providers.LoginURLReady,
	Detail: "A sign-in terminal has opened on the machine running agentd — complete the sign-in there. This page updates automatically when it finishes."}

// standInWaitingClaude is a claude whose sign-in waits in its terminal for a
// member who never finishes it.
const standInWaitingClaude = `case "$1" in
--version) echo "9.9.9 (Claude Code)" ;;
auth) exec sleep 30 ;;
*) exit 2 ;;
esac
`

// installWaitingClaude puts standInWaitingClaude in bin, with a sleep beside
// it that the stand-in's PATH, bin alone, would not find otherwise.
func installWaitingClaude(t *testing.T, bin string) {
	t.Helper()
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("the stand-in claude needs sleep: %v", err)
	}
	if err := os.Symlink(sleep, filepath.Join(bin, "sleep")); err != nil {
		t.Fatalf("link sleep into the stand-in's PATH: %v", err)
	}
	writeScript(t, bin, "claude", standInWaitingClaude)
}

// runConsoleSignIn has install put a claude in a directory of its own, the
// only one on PATH, then runs a claude sign-in through handleLogin on a
// personal runner, under ctx, against a stand-in API that answers the
// sign-in's state with status. It returns every progress report the flow
// sent, in order, and how long the flow took.
func runConsoleSignIn(t *testing.T, ctx context.Context, install func(t *testing.T, bin string), status string) ([]loginProgress, time.Duration) {
	t.Helper()
	skipWithoutPOSIXShell(t)
	bin := t.TempDir()
	install(t, bin)
	isolateRunnerEnv(t, bin)
	t.Setenv("HOME", t.TempDir())
	api := newFakeAPI(t, func(c apiCall) (int, string) {
		if c.is("GET", "/provider-logins/l1/full") {
			return http.StatusOK, `{"id":"l1","status":"` + status + `"}`
		}
		return 0, ""
	})
	w := NewWorker(NewClient(api.URL(), "worker-key"), Options{WorkerID: "w-console"})

	returned := make(chan struct{})
	start := time.Now()
	go func() {
		defer close(returned)
		w.logins.handleLogin(ctx, &providers.LoginRequest{ID: "l1", Provider: providers.ProviderClaudeCode})
	}()
	select {
	case <-returned:
	case <-time.After(20 * time.Second):
		t.Fatal("the console sign-in did not return")
	}
	took := time.Since(start)
	s := &signInWorld{t: t}
	got, _ := s.progressOf("l1", api.Calls())
	return got, took
}

// A claude that is on PATH but cannot be started, its interpreter missing,
// fails the sign-in with why the terminal did not open, and nothing else
// is reported.
func TestConsoleSignInReportsATerminalThatCannotOpen(t *testing.T) {
	var startErr error
	got, _ := runConsoleSignIn(t, context.Background(), func(t *testing.T, bin string) {
		path := filepath.Join(bin, "claude")
		if err := os.WriteFile(path, []byte("#!/nonexistent/openv-stand-in-shell\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		startErr = exec.Command(path).Start()
		if startErr == nil {
			t.Fatal("a script with a missing interpreter started")
		}
	}, providers.LoginClaimed)
	want := []loginProgress{{Status: providers.LoginFailed, Detail: "failed to open the sign-in terminal: " + startErr.Error()}}
	if !sameProgress(got, want) {
		t.Errorf("the console sign-in reported\n  %+v\nwant\n  %+v", got, want)
	}
}

// A member who cancels the sign-in while its terminal is open ends it: the
// flow stops the CLI, which has not finished, and reports nothing more. The
// sign-in is already cancelled on the API's side, and the CLI's exit is not
// a failure to report.
func TestConsoleSignInStopsQuietlyWhenCancelled(t *testing.T) {
	if testing.Short() {
		t.Skip("waits on the console flow's 2-second poll")
	}
	got, took := runConsoleSignIn(t, context.Background(), installWaitingClaude, providers.LoginCancelled)
	if want := []loginProgress{consoleOpened}; !sameProgress(got, want) {
		t.Errorf("the cancelled console sign-in reported\n  %+v\nwant\n  %+v", got, want)
	}
	if took > 10*time.Second {
		t.Errorf("the cancelled console sign-in took %v to stop its CLI, want one poll (2s) and the CLI's exit", took.Round(time.Millisecond))
	}
}

// A sign-in still open when its time runs out (loginTimeout, 10 minutes; a
// shorter context here) stops the CLI and fails as timed out.
func TestConsoleSignInTimesOut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	got, _ := runConsoleSignIn(t, ctx, installWaitingClaude, providers.LoginClaimed)
	want := []loginProgress{consoleOpened, {Status: providers.LoginFailed, Detail: "sign-in timed out after 10 minutes"}}
	if !sameProgress(got, want) {
		t.Errorf("the timed-out console sign-in reported\n  %+v\nwant\n  %+v", got, want)
	}
}
