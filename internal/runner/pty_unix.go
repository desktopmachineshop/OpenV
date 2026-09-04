//go:build !windows

package runner

import (
	"io"
	"os/exec"

	"github.com/creack/pty"
)

// ptySupported reports whether this platform can drive a TUI sign-in over a
// pseudo-terminal.
const ptySupported = true

// startPTY starts the command attached to a pseudo-terminal and returns the
// controlling end. The terminal is made very wide on purpose: the vendor CLIs
// soft-wrap their sign-in URL to the terminal width, and a wrapped URL cannot
// be scraped back out of the output reliably.
func startPTY(cmd *exec.Cmd) (io.ReadWriteCloser, error) {
	return pty.StartWithSize(cmd, &pty.Winsize{Rows: 50, Cols: 400})
}
