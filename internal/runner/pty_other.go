//go:build windows

package runner

import (
	"errors"
	"io"
	"os/exec"
)

// ptySupported reports whether this platform can drive a TUI sign-in over a
// pseudo-terminal. Windows hosts run a real console window instead
// (configureInteractiveConsole), which is what a personal runner wants
// anyway; headless mode is a Linux container concern.
const ptySupported = false

func startPTY(cmd *exec.Cmd) (io.ReadWriteCloser, error) {
	return nil, errors.New("pseudo-terminal sign-in is not supported on this platform")
}
