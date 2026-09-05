package runner

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestPTYRelayAgainstRealCLI drives the real `claude setup-token` through the
// same steps handlePTYLogin takes, to see what the relay would observe.
// Manual: needs the vendor CLI and a throwaway HOME, so it is skipped unless
// OPENV_PTY_PROBE=1.
func TestPTYRelayAgainstRealCLI(t *testing.T) {
	if os.Getenv("OPENV_PTY_PROBE") != "1" {
		t.Skip("set OPENV_PTY_PROBE=1 to run the manual pty relay probe")
	}
	// OPENV_PTY_PROBE_CMD overrides the command, so the probe can compare
	// the sign-in flows the CLI offers (setup-token vs auth login).
	name, args := "claude", []string{"setup-token"}
	if custom := os.Getenv("OPENV_PTY_PROBE_CMD"); custom != "" {
		fields := strings.Fields(custom)
		name, args = fields[0], fields[1:]
	}
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	tty, err := startPTY(cmd)
	if err != nil {
		t.Fatalf("startPTY: %v", err)
	}
	defer tty.Close()

	chunks := make(chan string, 64)
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := tty.Read(buf)
			if n > 0 {
				chunks <- string(buf[:n])
			}
			if err != nil {
				close(chunks)
				return
			}
		}
	}()

	screen := newScreenBuffer(32 * 1024)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	drain := func(d time.Duration, label string) {
		deadline := time.After(d)
		for {
			select {
			case chunk, ok := <-chunks:
				if !ok {
					t.Logf("[%s] pty closed", label)
					return
				}
				screen.add(chunk)
			case err := <-done:
				t.Logf("[%s] process exited: %v", label, err)
				return
			case <-deadline:
				t.Logf("[%s] tuiError = %q", label, tuiError(screen.String()))
				return
			}
		}
	}

	drain(18*time.Second, "before code")
	t.Logf("URL seen: %q", authURLPattern.FindString(stripANSI(screen.String())))

	screen.reset()
	if _, err := io.WriteString(tty, "stale-code-from-another-attempt\r"); err != nil {
		t.Fatalf("write code: %v", err)
	}
	for i := 1; i <= 4; i++ {
		drain(5*time.Second, "after code")
	}
	t.Logf("FINAL visible lines: %q", visibleLines(screen.String()))
	_ = cmd.Process.Kill()
}
