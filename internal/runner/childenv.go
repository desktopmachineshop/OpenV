package runner

import (
	"os"
	"strings"
)

// runnerCredentials name the variables that can hold OpenV credentials on
// the runner's host: the worker key the runner authenticates with, the
// deployment-wide key a pool node registers with, and the workspace runner
// key or the account's email and password that openv-mcp and the OpenV
// scripts read, which a host that also runs those may set. A provider CLI
// carries out the agent's tool calls for whoever launched the run, on a
// pool node for whichever member holds the lease, and git runs any hook the
// agent writes into the run's workspace, so no process the runner starts
// may read any of them; the agent reaches OpenV with the run's own token,
// OPENV_RUN_TOKEN.
var runnerCredentials = []string{
	"WORKER_API_KEY", "RUNNER_POOL_KEY",
	"OPENV_API_TOKEN", "OPENV_EMAIL", "OPENV_PASSWORD",
}

// childEnv is the environment every process the runner starts gets (a
// provider CLI's run, sign-in or version probe, and git): the runner's own,
// which is how the CLI finds its settings, less the runner's credentials,
// with extra (KEY=VALUE entries) layered on top.
func childEnv(extra ...string) []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+len(extra))
	for _, kv := range env {
		if !isRunnerCredential(kv) {
			out = append(out, kv)
		}
	}
	return append(out, extra...)
}

// isRunnerCredential reports whether a KEY=VALUE entry sets one of
// runnerCredentials, ignoring case as Windows does.
func isRunnerCredential(kv string) bool {
	name, _, _ := strings.Cut(kv, "=")
	for _, c := range runnerCredentials {
		if strings.EqualFold(name, c) {
			return true
		}
	}
	return false
}
