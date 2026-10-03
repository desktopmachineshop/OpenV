package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/providers"
)

// A sign-in CLI that fails says why in its last lines, and the failure the
// member is shown quotes them. The piped flow (codex on a personal runner,
// Worker.handleLogin) and the loopback flow (codex on a headless one,
// Worker.handleLoopbackLogin) read the CLI's output on a goroutine of their
// own into a tail of its last lines. That reader used to add to the tail
// while the flow read it, a data race the race detector reports, and
// Cmd.Wait closed the reader's pipe as soon as the CLI exited, so the lines
// it had not read yet, the reason among them, were lost. These tests drive
// each flow with a stand-in codex that prints its sign-in link, a burst of
// progress lines and then its reason, and exits 3; under -race they also
// fail on the race. A burst is lost only when the reader falls behind, which
// is a matter of timing, so two more tests hold each flow to its wait for
// the rest of the output deterministically: a stand-in whose reason comes
// from a process it leaves behind, after it has exited, must be quoted, and
// one whose leftover process keeps the output open must not hold the failure
// back for much longer than outputDrainTimeout.

// standInFailingCodex prints more output than the reader takes in one read,
// so that it is still reading when the CLI exits.
const standInFailingCodex = `case "$1" in
--version) echo "codex-cli 0.0.0-stand-in" ;;
login)
  echo "https://auth.openai.com/oauth/authorize?client_id=stand-in&redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback"
  i=0
  while [ $i -lt 2000 ]; do echo "progress line $i of the stand-in sign-in"; i=$((i+1)); done
  echo "error: the stand-in sign-in was refused"
  exit 3 ;;
*) exit 2 ;;
esac
`

// failedSignInDetail is the failure a flow keeping the last lines lines of
// standInFailingCodex's output reports.
func failedSignInDetail(lines int) string {
	var tail []string
	for i := 2000 - (lines - 1); i < 2000; i++ {
		tail = append(tail, fmt.Sprintf("progress line %d of the stand-in sign-in", i))
	}
	tail = append(tail, "error: the stand-in sign-in was refused")
	return "sign-in command failed: exit status 3 — output tail: " + strings.Join(tail, " | ")
}

// runFailingSignIn installs standInFailingCodex, runs flow against a
// stand-in API until it returns, and returns the sign-in's last progress
// report.
func runFailingSignIn(t *testing.T, headless bool, flow func(w *Worker, ctx context.Context, login *providers.LoginRequest)) loginProgress {
	t.Helper()
	skipWithoutPOSIXShell(t)
	got, _ := runStandInSignIn(t, standInBin(t, standInFailingCodex), headless, flow)
	return got
}

// standInBin writes codex as the stand-in codex in a directory of its own,
// beside a link to each of the named system tools, which the stand-in's
// PATH, holding that directory alone, would not find otherwise.
func standInBin(t *testing.T, codex string, tools ...string) string {
	t.Helper()
	bin := t.TempDir()
	writeScript(t, bin, "codex", codex)
	for _, tool := range tools {
		path, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("the stand-in codex needs %s: %v", tool, err)
		}
		if err := os.Symlink(path, filepath.Join(bin, tool)); err != nil {
			t.Fatalf("link %s into the stand-in's PATH: %v", tool, err)
		}
	}
	return bin
}

// runStandInSignIn runs flow with PATH holding bin alone against a stand-in
// API until it returns, and returns the sign-in's last progress report and
// how long the flow took.
func runStandInSignIn(t *testing.T, bin string, headless bool, flow func(w *Worker, ctx context.Context, login *providers.LoginRequest)) (loginProgress, time.Duration) {
	t.Helper()
	isolateRunnerEnv(t, bin)
	t.Setenv("HOME", t.TempDir())
	api := newFakeAPI(t, nil)
	w := NewWorker(NewClient(api.URL(), "worker-key"), Options{WorkerID: "w-signin", Headless: headless})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	returned := make(chan struct{})
	start := time.Now()
	go func() {
		defer close(returned)
		flow(w, ctx, &providers.LoginRequest{ID: "l1", Provider: providers.ProviderCodexCLI})
	}()
	select {
	case <-returned:
	case <-time.After(40 * time.Second):
		t.Fatal("the sign-in flow did not return after its CLI failed")
	}
	took := time.Since(start)

	s := &signInWorld{t: t}
	got, _ := s.progressOf("l1", api.Calls())
	if len(got) == 0 {
		t.Fatal("the sign-in reported no progress")
	}
	return got[len(got)-1], took
}

// TestPipedSignInFailureQuotesTheCLIsLastLines: the piped flow keeps the
// last 30 lines, and a failure quotes all of them, the CLI's reason last.
func TestPipedSignInFailureQuotesTheCLIsLastLines(t *testing.T) {
	got := runFailingSignIn(t, false, func(w *Worker, ctx context.Context, login *providers.LoginRequest) {
		w.handleLogin(ctx, login)
	})
	want := loginProgress{Status: providers.LoginFailed, Detail: failedSignInDetail(30)}
	if got != want {
		t.Errorf("the failed piped sign-in reported\n  %+v\nwant\n  %+v", got, want)
	}
}

// TestLoopbackSignInFailureQuotesTheCLIsLastLines: the loopback flow keeps
// the last 40 lines, and a failure quotes all of them, the CLI's reason last.
func TestLoopbackSignInFailureQuotesTheCLIsLastLines(t *testing.T) {
	got := runFailingSignIn(t, true, func(w *Worker, ctx context.Context, login *providers.LoginRequest) {
		flow, _ := flowFor(login.Provider)
		w.handleLoopbackLogin(ctx, login, flow)
	})
	want := loginProgress{Status: providers.LoginFailed, Detail: failedSignInDetail(40)}
	if got != want {
		t.Errorf("the failed loopback sign-in reported\n  %+v\nwant\n  %+v", got, want)
	}
}

// signInFlows are the two flows that read a sign-in CLI's output on a pipe
// of their own and quote its tail when the CLI fails.
var signInFlows = []struct {
	name     string
	headless bool
	run      func(w *Worker, ctx context.Context, login *providers.LoginRequest)
}{
	{"piped", false, func(w *Worker, ctx context.Context, login *providers.LoginRequest) {
		w.handleLogin(ctx, login)
	}},
	{"loopback", true, func(w *Worker, ctx context.Context, login *providers.LoginRequest) {
		flow, _ := flowFor(login.Provider)
		w.handleLoopbackLogin(ctx, login, flow)
	}},
}

// standInLateReasonCodex prints its sign-in link and exits 3, leaving behind
// a process that holds its output: 0.3 s later that process prints why the
// sign-in failed, then keeps the output open for hold more seconds. Its pid
// goes to pidFile, so that the test can stop it.
func standInLateReasonCodex(pidFile string, hold int) string {
	return strings.NewReplacer("@HOLD@", strconv.Itoa(hold), "@PIDFILE@", pidFile).Replace(`case "$1" in
--version) echo "codex-cli 0.0.0-stand-in" ;;
login)
  echo "https://auth.openai.com/oauth/authorize?client_id=stand-in&redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback"
  ( sleep 0.3; echo "error: the stand-in sign-in was refused"; exec sleep @HOLD@ ) &
  echo $! > '@PIDFILE@'
  exit 3 ;;
*) exit 2 ;;
esac
`)
}

// lateReasonDetail is the failure a flow reports when it quotes all of
// standInLateReasonCodex's output.
const lateReasonDetail = "sign-in command failed: exit status 3 — output tail: " +
	"https://auth.openai.com/oauth/authorize?client_id=stand-in&redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback" +
	" | error: the stand-in sign-in was refused"

// runLateReasonSignIn runs one flow with standInLateReasonCodex. A process
// the stand-in left behind to hold the output open is stopped when the test
// ends; one that holds it for no time ends by itself.
func runLateReasonSignIn(t *testing.T, hold int, headless bool, flow func(w *Worker, ctx context.Context, login *providers.LoginRequest)) (loginProgress, time.Duration) {
	t.Helper()
	skipWithoutPOSIXShell(t)
	pidFile := filepath.Join(t.TempDir(), "left-behind.pid")
	if hold > 0 {
		t.Cleanup(func() {
			raw, err := os.ReadFile(pidFile)
			if err != nil {
				return
			}
			if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil {
				if p, err := os.FindProcess(pid); err == nil {
					_ = p.Kill()
				}
			}
		})
	}
	return runStandInSignIn(t, standInBin(t, standInLateReasonCodex(pidFile, hold), "sleep"), headless, flow)
}

// TestSignInFailureWaitsForALateReason: the CLI has exited, but the reason
// for the failure comes 0.3 s later, from a process it left holding its
// output. Each flow waits for the reader to reach the end of the output, and
// so quotes the reason; a flow that quoted the tail as soon as the CLI
// exited would not have it.
func TestSignInFailureWaitsForALateReason(t *testing.T) {
	for _, f := range signInFlows {
		t.Run(f.name, func(t *testing.T) {
			got, _ := runLateReasonSignIn(t, 0, f.headless, f.run)
			want := loginProgress{Status: providers.LoginFailed, Detail: lateReasonDetail}
			if got != want {
				t.Errorf("the failed %s sign-in reported\n  %+v\nwant\n  %+v", f.name, got, want)
			}
		})
	}
}

// TestSignInFailureStopsWaitingAfterTheDrainBound: the process the CLI left
// behind prints the reason and then keeps the output open for 8 s more. Each
// flow stops waiting for the end of the output after outputDrainTimeout and
// reports the failure, the reason it read by then included; a flow without
// that bound would wait for the leftover process, up to the sign-in's own
// 10-minute deadline, and hold every later sign-in on the runner behind it.
func TestSignInFailureStopsWaitingAfterTheDrainBound(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out the 2-second drain bound in each flow")
	}
	const margin = 1500 * time.Millisecond
	for _, f := range signInFlows {
		t.Run(f.name, func(t *testing.T) {
			got, took := runLateReasonSignIn(t, 8, f.headless, f.run)
			if took > outputDrainTimeout+margin {
				t.Errorf("the failed %s sign-in took %v to report, want at most outputDrainTimeout (%v) and %v",
					f.name, took.Round(10*time.Millisecond), outputDrainTimeout, margin)
			}
			want := loginProgress{Status: providers.LoginFailed, Detail: lateReasonDetail}
			if got != want {
				t.Errorf("the failed %s sign-in reported\n  %+v\nwant\n  %+v", f.name, got, want)
			}
		})
	}
}

// TestLineTailIsSafeForConcurrentUse: the loopback and pseudo-terminal flows
// read the tail for their "no link yet" notice while the output reader may
// still be adding to it. Under -race, a tail without its own lock fails
// here.
func TestLineTailIsSafeForConcurrentUse(t *testing.T) {
	tail := newLineTail(5)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			tail.add(fmt.Sprintf("line %d", i))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_ = tail.String()
		}
	}()
	wg.Wait()
	if got, want := tail.String(), "line 995 | line 996 | line 997 | line 998 | line 999"; got != want {
		t.Errorf("tail = %q, want %q", got, want)
	}
}
