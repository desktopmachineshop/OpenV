package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/providers"
)

// TestStartHeartbeatBeatsUntilStopped: the loop beats repeatedly at the given
// interval and never again once stop has returned.
func TestStartHeartbeatBeatsUntilStopped(t *testing.T) {
	var mu sync.Mutex
	beats := 0
	stop := startHeartbeat(2*time.Millisecond, func() {
		mu.Lock()
		beats++
		mu.Unlock()
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := beats
		mu.Unlock()
		if n >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("heartbeat fired %d times, want >= 3", n)
		}
		time.Sleep(time.Millisecond)
	}

	stop()
	mu.Lock()
	atStop := beats
	mu.Unlock()

	// The interval is 2ms; if the goroutine survived stop it would beat
	// many times over this window.
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	final := beats
	mu.Unlock()
	if final != atStop {
		t.Errorf("beats after stop returned: %d -> %d", atStop, final)
	}
}

// TestStartHeartbeatStopWaitsForInflightBeat: stop must not return while a
// beat is still executing — the caller relies on that to hand heartbeating
// over to the log pump without two concurrent pushers.
func TestStartHeartbeatStopWaitsForInflightBeat(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var mu sync.Mutex
	inFlight := false
	stop := startHeartbeat(time.Millisecond, func() {
		mu.Lock()
		inFlight = true
		mu.Unlock()
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		mu.Lock()
		inFlight = false
		mu.Unlock()
	})

	<-entered // a beat is now blocked mid-flight
	go func() {
		time.Sleep(10 * time.Millisecond)
		close(release)
	}()
	stop() // must block until the beat above finishes

	mu.Lock()
	still := inFlight
	mu.Unlock()
	if still {
		t.Error("stop returned while a beat was still in flight")
	}

	stop() // idempotent: second call must not panic or block
}

// hostSecretEnv is a variable of the runner's host that is not a provider
// key: a project's provider setting may name it, and the runner must not
// read it.
const hostSecretEnv = "OPENV_RUNNER_TEST_HOST_SECRET"

// TestRunEnvChecksTheKeyNameBeforeReadingIt: a run on api-key auth whose
// provider setting names a variable outside the provider-key catalogue
// fails without the runner reading that variable, so the setting cannot
// turn the runner into a reader of its host's secrets (#379 bug 96).
// Worker.runEnv asks providers.IsAllowedAPIKeyEnv before os.Getenv; the
// other order fails the same run with the same message, so only the read
// itself tells them apart. Go's test log (-test.testlogfile, which the go
// command uses to know which variables a test read) records every
// os.Getenv, so the test runs its two claims in a child test process with
// the log on and reads it back: the allowed variable was read, the host
// secret never was.
func TestRunEnvChecksTheKeyNameBeforeReadingIt(t *testing.T) {
	const test = "TestRunEnvChecksTheKeyNameBeforeReadingIt"
	if os.Getenv(childTestEnv) != test {
		reads := envReadsOf(t, test)
		if !slices.Contains(reads, "GOOGLE_API_KEY") {
			t.Fatalf("the child read %q, without the allowed GOOGLE_API_KEY: the test log is not recording reads", reads)
		}
		if slices.Contains(reads, hostSecretEnv) {
			t.Errorf("the runner read %s, which a provider setting named but is not a provider key variable", hostSecretEnv)
		}
		return
	}

	api := newFakeAPI(t, nil)
	w := NewWorker(NewClient(api.URL(), "worker-key"), Options{WorkerID: "w-env", WorkspaceBase: t.TempDir()})
	claim := func(id, keyEnv string) *ClaimResponse {
		return &ClaimResponse{Run: &agentruns.Run{ID: id}, Agent: &agents.Agent{Provider: providers.ProviderClaudeCode},
			RunToken: "rt-" + id, Auth: &RunAuth{Mode: "api-key", APIKeyEnv: keyEnv}}
	}

	// Not t.Setenv, which reads the variable to restore it: this process
	// ends with the test, and the log must hold the runner's reads alone.
	if err := os.Setenv("GOOGLE_API_KEY", "google-key-on-the-host"); err != nil {
		t.Fatal(err)
	}
	env, ok := w.runEnv(claim("allowed", "GOOGLE_API_KEY"))
	if !ok || env["ANTHROPIC_API_KEY"] != "google-key-on-the-host" {
		t.Fatalf("a claim naming GOOGLE_API_KEY: ok = %v, env = %v; want the key under ANTHROPIC_API_KEY", ok, env)
	}
	if _, ok := w.runEnv(claim("refused", hostSecretEnv)); ok {
		t.Fatalf("a claim naming %s was given an environment", hostSecretEnv)
	}
	finishes := api.match(func(c apiCall) bool { return c.is("POST", "/agent-runs/refused/finish") })
	if len(finishes) != 1 || !strings.Contains(string(finishes[0].Body), "which is not a provider key variable") {
		t.Errorf("the refused run's finish = %s, want one failing it as not a provider key variable", describeCalls(finishes))
	}
}

// envReadsOf runs test alone in a child test process (childTestEnv naming
// it) with Go's test log written to a file, and returns the variables the
// child read with os.Getenv or os.LookupEnv, in the order the log records
// them.
func envReadsOf(t *testing.T, test string) []string {
	t.Helper()
	logFile := filepath.Join(t.TempDir(), "testlog.txt")
	cmd := exec.Command(os.Args[0], "-test.run=^"+test+"$", "-test.count=1", "-test.v", "-test.timeout=2m",
		"-test.testlogfile="+logFile)
	cmd.Env = append(os.Environ(), childTestEnv+"="+test)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "--- PASS: "+test+" ") {
		t.Fatalf("%s, run in a child test process, did not pass (%v):\n%s", test, err, out)
	}
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("the child test process wrote no test log: %v", err)
	}
	var reads []string
	for _, line := range strings.Split(string(data), "\n") {
		if name, ok := strings.CutPrefix(line, "getenv "); ok {
			reads = append(reads, name)
		}
	}
	return reads
}
