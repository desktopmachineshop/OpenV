//go:build windows

package runner

import (
	"os/exec"
	"syscall"
)

const createNewConsole = 0x00000010

// configureInteractiveConsole makes the command open in its own visible
// console window so TUI sign-in flows get a real terminal.
func configureInteractiveConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewConsole}
	// No pipes: the new console owns stdio.
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
}
