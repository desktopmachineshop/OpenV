//go:build !windows

package runner

import (
	"os"
	"os/exec"
)

// configureInteractiveConsole attaches the command to agentd's own terminal
// on Unix hosts (agentd is typically run from a shell there); the sign-in
// TUI renders in that terminal.
func configureInteractiveConsole(cmd *exec.Cmd) {
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
}
