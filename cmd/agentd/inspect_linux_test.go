//go:build linux

package main

import (
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// TestForbidInspectionClearsDumpable checks the flag itself, then puts it
// back for the rest of the test binary.
func TestForbidInspectionClearsDumpable(t *testing.T) {
	t.Cleanup(func() { _ = unix.Prctl(unix.PR_SET_DUMPABLE, 1, 0, 0, 0) })
	if err := forbidInspection(); err != nil {
		t.Fatal(err)
	}
	got, err := unix.PrctlRetInt(unix.PR_GET_DUMPABLE, 0, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Fatalf("PR_GET_DUMPABLE = %d after forbidInspection, want 0", got)
	}
}

// TestAgentdEnvironUnreadable starts the real agentd as a pool node, holds
// its first request so it stays up, and reads /proc/<pid>/environ as the
// same user, as an agent it ran could: the read must be refused. A process
// with CAP_SYS_PTRACE (root, here) may read it anyway, so the check needs an
// ordinary user, as CI's runner is.
func TestAgentdEnvironUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root may read any process's environ; run as an ordinary user")
	}
	reached := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case reached <- struct{}{}:
		default:
		}
		<-release
		http.Error(w, "test over", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	defer close(release)

	home := t.TempDir()
	cmd := exec.Command(cliBuild(t), "-api", srv.URL)
	cmd.Env = []string{"HOME=" + home, "XDG_CONFIG_HOME=" + home, "RUNNER_POOL_KEY=pk-example"}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	select {
	case <-reached:
	case <-time.After(30 * time.Second):
		t.Fatal("agentd never reached the API")
	}

	environ, err := os.ReadFile("/proc/" + strconv.Itoa(cmd.Process.Pid) + "/environ")
	if err == nil {
		if strings.Contains(string(environ), "pk-example") {
			t.Fatal("a same-user process reads agentd's pool key from /proc/<pid>/environ")
		}
		t.Fatal("a same-user process reads agentd's /proc/<pid>/environ")
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("reading agentd's environ: %v, want a permission error", err)
	}
}
