package main

import "testing"

// cliName is the command cli_harness_test.go builds and runs.
const cliName = "agentd"

// TestCLI snapshots agentd's command line (refactor plan S8, invariant
// I14): its twelve flags with their help and defaults, and the defaults the
// environment gives them (the values of WORKER_API_KEY and RUNNER_POOL_KEY
// included, which -h prints), an unknown flag, and the refusal without a
// worker key, which comes before any network call. Nothing here reaches the
// network: -h and an unknown flag stop in flag.Parse.
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
	})
}
