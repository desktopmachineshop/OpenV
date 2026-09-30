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

// credentialValues are the OpenV credentials a runner's host may hold, each
// with a value of its own, so that a child's output shows which one it saw;
// they are listed here rather than read from runnerCredentials, so that one
// dropped from that list shows as a child that sees it.
var credentialValues = map[string]string{
	"WORKER_API_KEY":  "wk-example",
	"RUNNER_POOL_KEY": "pk-example",
	"OPENV_API_TOKEN": "api-token-example",
	"OPENV_EMAIL":     "agent-owner@example.com",
	"OPENV_PASSWORD":  "password-example",
}

// setRunnerCredentials sets every credential of credentialValues in the
// test's environment, and a setting a child should see.
func setRunnerCredentials(t *testing.T) {
	t.Helper()
	for name, value := range credentialValues {
		t.Setenv(name, value)
	}
	t.Setenv("OPENV_CHILD_ENV_SETTING", "kept")
}

// seesACredential reports the first credential value in a child's output.
func seesACredential(out string) string {
	for name, value := range credentialValues {
		if strings.Contains(out, value) {
			return name + "=" + value
		}
	}
	return ""
}

// TestChildEnvDropsRunnerCredentials pins what a provider CLI inherits: the
// runner's environment and the entries layered on top, but none of the
// OpenV credentials the runner's host may hold, whatever their case: the
// runner's own keys since #414, and a workspace runner key and an account's
// email and password since issue #379's question 17.
func TestChildEnvDropsRunnerCredentials(t *testing.T) {
	setRunnerCredentials(t)
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

	for _, kv := range env {
		if seen := seesACredential(kv); seen != "" {
			t.Errorf("childEnv passes on %s", seen)
		}
	}
	for kv, want := range map[string]bool{
		"WORKER_API_KEY=x":             true,
		"worker_api_key=x":             true,
		"RUNNER_POOL_KEY=":             true,
		"OPENV_API_TOKEN=x":            true,
		"openv_api_token=x":            true,
		"OPENV_EMAIL=a@example.com":    true,
		"OPENV_PASSWORD=x":             true,
		"Openv_Password=":              true,
		"WORKER_API_KEYS=x":            false,
		"MY_WORKER_API_KEY=x":          false,
		"RUNNER_POOL=x":                false,
		"OPENV_RUN_TOKEN=x":            false,
		"OPENV_API_URL=x":              false,
		"OPENV_EMAIL_VERIFICATION=off": false,
		"OPENV_API_TOKENS=x":           false,
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
	setRunnerCredentials(t)
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
		if isRunnerCredential(line) || seesACredential(line) != "" {
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
	setRunnerCredentials(t)
	t.Setenv("OPENV_CHILD_ENV_HELPER", "1")
	out, err := runVersion(context.Background(), os.Args[0], "-test.run=^TestChildEnvHelperProcess$")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "OPENV_CHILD_ENV_HELPER=1") {
		t.Fatalf("the probe printed no environment: %q", out)
	}
	if seen := seesACredential(out); seen != "" {
		t.Errorf("the version probe sees a runner credential, %s", seen)
	}
}

// TestRunGitHidesRunnerCredentials runs a git shell alias, which runs with
// git's environment as a hook the agent wrote into its workspace would, and
// checks no credential reaches it.
func TestRunGitHidesRunnerCredentials(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the alias runs env through a POSIX shell")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	setRunnerCredentials(t)
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
	if seen := seesACredential(out); seen != "" {
		t.Errorf("git sees a runner credential, %s", seen)
	}
}
