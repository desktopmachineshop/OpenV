package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cliName is the command cli_harness_test.go builds and runs.
const cliName = "agentd"

// TestCLI snapshots agentd's command line (refactor plan S8, invariant
// I14): its twelve flags with their help and defaults, and the defaults the
// environment gives them (not WORKER_API_KEY, RUNNER_POOL_KEY, RUNNER_POOL
// or RUNNER_NODE_NAME, which agentd reads after parsing, so its usage text
// prints no key), trimmed, with the default and one warning naming the
// variable for a value that breaks internal/envparse's rule, flag errors,
// the refusal without a worker key, which comes before any network call,
// and a key with spaces or a line break around it, used exactly as set and
// named once, never printed (#379, question 24). Nothing here reaches the network: -h and a flag error stop in
// flag.Parse.
func TestCLI(t *testing.T) {
	runCLI(t, []cliScenario{
		{name: "help", args: []string{"-h"}},
		{name: "unknown_flag", args: []string{"--bogus"}},
		{name: "no_worker_key"},
		{name: "env_defaults", args: []string{"-h"}, env: []string{
			"OPENV_API_URL=https://api.example.test",
			"WORKER_API_KEY=wk-example",
			"AGENT_CONCURRENCY= 3",
			"AGENT_CHILD_CONCURRENCY=0",
			"AGENT_WORKSPACE_RETENTION=90m",
			"RUNNER_POOL_KEY=pk-example",
			"RUNNER_POOL=gpu",
			"RUNNER_NODE_NAME=node-a",
			"RUNNER_SESSION_ROOT=/srv/openv/sessions",
			"OPENV_HOSTED=TRUE",
		}},
		{name: "hosted_true", args: []string{"-h"}, env: []string{"OPENV_HOSTED=true"}},
		// A value that breaks the rule keeps the flag's own default and
		// warns once, naming the variable and not the value.
		{name: "malformed_env", args: []string{"-h"}, env: []string{
			"AGENT_CONCURRENCY=-5",
			"AGENT_CHILD_CONCURRENCY=2x",
			"AGENT_WORKSPACE_RETENTION=-1h",
			"OPENV_HOSTED=yes",
		}},
		// Every flag error prints the same usage text as -h.
		{name: "bad_flag_value_env_keys", args: []string{"-concurrency=x"}, env: []string{
			"WORKER_API_KEY=wk-example",
			"RUNNER_POOL_KEY=pk-example",
		}},
		// The keys still come from the environment when their flags are not
		// given, and a flag given empty still wins. A file where the
		// workspaces directory's parent should go stops a run before the
		// network.
		{name: "worker_key_from_env", env: []string{"WORKER_API_KEY=wk-example"}, setup: blockWorkspaces},
		{name: "worker_key_flag_empty", args: []string{"-worker-key="}, env: []string{"WORKER_API_KEY=wk-example"}},
		{name: "pool_key_from_env", env: []string{"RUNNER_POOL_KEY=pk-example"}, setup: blockWorkspaces},
		{name: "pool_key_flag_empty", args: []string{"-pool-key="}, env: []string{"RUNNER_POOL_KEY=pk-example"}},
		// A key with a line break after it, or a space in front, is a
		// credential used exactly as set: one warning names its variable,
		// and TestCLIPrintsNoKey holds that it prints no key.
		{name: "worker_key_line_break", env: []string{"WORKER_API_KEY=wk-example\n"}, setup: blockWorkspaces},
		{name: "pool_key_leading_space", env: []string{"RUNNER_POOL_KEY= pk-example"}, setup: blockWorkspaces},
		// A key given on the command line still works, and agentd warns that
		// ps shows it there, naming the variable to use instead and never
		// the key (issue #379's question 18); an empty one, above, puts no
		// key there and draws no warning.
		{name: "worker_key_flag", args: []string{"-worker-key=wk-example"}, setup: blockWorkspaces},
		{name: "pool_key_flag", args: []string{"-pool-key=pk-example"}, setup: blockWorkspaces},
	})
}

// TestCLIPrintsNoKey reads every agentd snapshot and fails when a key value
// a scenario sets appears anywhere but where the scenario gives it, its env
// lines and its command line, so regenerating the goldens cannot quietly
// accept a key printed again.
func TestCLIPrintsNoKey(t *testing.T) {
	goldens, err := filepath.Glob(filepath.Join("testdata", "cli", "*.txt"))
	if err != nil || len(goldens) == 0 {
		t.Fatalf("no agentd snapshots (%v)", err)
	}
	for _, g := range goldens {
		src, err := os.ReadFile(g)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(line, "env ") || strings.HasPrefix(line, "$ "+cliName) {
				continue
			}
			for _, key := range []string{"wk-example", "pk-example"} {
				if strings.Contains(line, key) {
					t.Errorf("%s:%d prints the key %q: %s", g, i+1, key, line)
				}
			}
		}
	}
}

// blockWorkspaces puts a file where the default workspaces directory's
// parent, $HOME/.openv, would go, so creating it fails.
func blockWorkspaces(t *testing.T, home, _ string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, ".openv"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}
