//go:build linux

package main

import "golang.org/x/sys/unix"

// forbidInspection marks agentd not dumpable. Every agent it runs, and any
// command or git hook an agent starts, runs as the same user as agentd, and
// a same-user process may otherwise read /proc/<agentd pid>/environ, which
// keeps the environment agentd started with, WORKER_API_KEY and
// RUNNER_POOL_KEY included, whatever the runner hands its children. Not
// dumpable, agentd's environ, memory and ptrace are open only to a process
// with CAP_SYS_PTRACE, as for ssh-agent. exec resets the flag, so the
// processes agentd starts stay as they were.
func forbidInspection() error {
	return unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0)
}
