package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// TestChildEnvDropsRunnerCredentials pins what a provider CLI inherits: the
// runner's environment and the entries layered on top, but neither of the
// runner's own OpenV keys, whatever their case.
func TestChildEnvDropsRunnerCredentials(t *testing.T) {
	t.Setenv("WORKER_API_KEY", "wk-example")
	t.Setenv("RUNNER_POOL_KEY", "pk-example")
	t.Setenv("OPENV_CHILD_ENV_SETTING", "kept")
	env := childEnv("OPENV_RUN_TOKEN=run-token", "TERM=xterm-256color")

	has := map[string]bool{}
	for _, kv := range env {
		has[kv] = true
		if isRunnerCredential(kv) {
			t.Errorf("childEnv passes on the runner credential %q", kv)
		}
	}
	for _, want := range []string{"OPENV_CHILD_ENV_SETTING=kept", "OPENV_RUN_TOKEN=run-token", "TERM=xterm-256color"} {
		if !has[want] {
			t.Errorf("childEnv lacks %q", want)
		}
	}
	if got := env[len(env)-2:]; got[0] != "OPENV_RUN_TOKEN=run-token" || got[1] != "TERM=xterm-256color" {
		t.Errorf("childEnv's extra entries are not last, in order: %q", got)
	}

	for kv, want := range map[string]bool{
		"WORKER_API_KEY=x":    true,
		"worker_api_key=x":    true,
		"RUNNER_POOL_KEY=":    true,
		"WORKER_API_KEYS=x":   false,
		"MY_WORKER_API_KEY=x": false,
		"RUNNER_POOL=x":       false,
	} {
		if got := isRunnerCredential(kv); got != want {
			t.Errorf("isRunnerCredential(%q) = %v, want %v", kv, got, want)
		}
	}
}

// lineCollector is a streamParser that keeps every stdout line.
type lineCollector struct {
	mu    sync.Mutex
	lines []string
}

func (c *lineCollector) ParseLine(line string, _ func(RunEvent)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, line)
}

func (c *lineCollector) Result(exitCode int, stderrTail string) (Result, error) {
	if exitCode != 0 {
		return Result{}, fmt.Errorf("helper exited %d: %s", exitCode, stderrTail)
	}
	return Result{}, nil
}

// TestStartProcHidesRunnerCredentials runs a real child through startProc,
// the path every agent run takes, and checks the environment it sees.
func TestStartProcHidesRunnerCredentials(t *testing.T) {
	t.Setenv("WORKER_API_KEY", "wk-example")
	t.Setenv("RUNNER_POOL_KEY", "pk-example")
	t.Setenv("OPENV_CHILD_ENV_SETTING", "kept")
	c := &lineCollector{}
	h, err := startProc(context.Background(), procConfig{
		Command:    os.Args[0],
		Args:       []string{"-test.run=^TestChildEnvHelperProcess$"},
		Env:        map[string]string{"OPENV_CHILD_ENV_HELPER": "1", "OPENV_RUN_TOKEN": "run-token"},
		TimeoutSec: 60,
	}, c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Wait(); err != nil {
		t.Fatal(err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	seen := strings.Join(c.lines, "\n")
	for _, want := range []string{"OPENV_CHILD_ENV_SETTING=kept", "OPENV_RUN_TOKEN=run-token"} {
		if !strings.Contains(seen, want) {
			t.Errorf("the child did not see %q", want)
		}
	}
	for _, line := range c.lines {
		if isRunnerCredential(line) || strings.Contains(line, "wk-example") || strings.Contains(line, "pk-example") {
			t.Errorf("the child sees a runner credential: %s", line)
		}
	}
}

// TestChildEnvHelperProcess is the child TestStartProcHidesRunnerCredentials
// starts: it prints its environment, one entry a line, and does nothing when
// run as an ordinary test.
func TestChildEnvHelperProcess(t *testing.T) {
	if os.Getenv("OPENV_CHILD_ENV_HELPER") != "1" {
		return
	}
	for _, kv := range os.Environ() {
		fmt.Println(kv)
	}
	os.Exit(0)
}

// TestRunVersionHidesRunnerCredentials checks the provider version probe's
// environment the same way.
func TestRunVersionHidesRunnerCredentials(t *testing.T) {
	t.Setenv("WORKER_API_KEY", "wk-example")
	t.Setenv("RUNNER_POOL_KEY", "pk-example")
	t.Setenv("OPENV_CHILD_ENV_HELPER", "1")
	out, err := runVersion(context.Background(), os.Args[0], "-test.run=^TestChildEnvHelperProcess$")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "OPENV_CHILD_ENV_HELPER=1") {
		t.Fatalf("the probe printed no environment: %q", out)
	}
	if strings.Contains(out, "wk-example") || strings.Contains(out, "pk-example") {
		t.Errorf("the version probe sees a runner credential: %q", out)
	}
}

// TestRunGitHidesRunnerCredentials runs a git shell alias, which runs with
// git's environment as a hook the agent wrote into its workspace would, and
// checks neither key reaches it.
func TestRunGitHidesRunnerCredentials(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the alias runs env through a POSIX shell")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("WORKER_API_KEY", "wk-example")
	t.Setenv("RUNNER_POOL_KEY", "pk-example")
	t.Setenv("OPENV_CHILD_ENV_SETTING", "kept")
	dir := t.TempDir()
	out, err := runGit(dir, "-c", "alias.showenv=!env", "showenv")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "OPENV_CHILD_ENV_SETTING=kept") {
		t.Fatalf("git printed no environment: %q", out)
	}
	if !strings.Contains(out, "PWD="+dir+"\n") && !strings.HasSuffix(out, "PWD="+dir) {
		t.Errorf("git does not see PWD=%s, which os/exec gave it when it inherited the environment: %q", dir, out)
	}
	if strings.Contains(out, "wk-example") || strings.Contains(out, "pk-example") {
		t.Errorf("git sees a runner credential: %q", out)
	}
}
