//go:build linux || darwin || dragonfly || freebsd || illumos || netbsd || openbsd

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// lockPort claims a port for this test process against the other harness
// processes on the host (portRegistry.lock): an exclusive, non-blocking
// flock on a lock file of the port's under os.TempDir(), which the kernel
// drops when the file is closed or the process dies, so a killed run leaves
// no claim behind. The file itself is left in place, since removing it would
// let a process that opened it before the removal and one that creates it
// afresh both hold a lock. ok is false only when another process holds the
// lock; where no lock can be taken (the directory is not writable, or the
// file system has no flock), the port is taken with the process-local
// registry's guarantee alone.
func lockPort(port int) (release func(), ok bool) {
	return lockFile(filepath.Join(os.TempDir(), fmt.Sprintf("openv-harness-port-%d.lock", port)))
}

// lockFile takes lockPort's lock on the file at path.
func lockFile(path string) (release func(), ok bool) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}, true
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false
		}
		return func() {}, true
	}
	return func() { _ = f.Close() }, true
}

// TestLockFileExcludesAnotherHolder: a port's lock is held against every
// other open of its file, as another process's would be, until it is
// released. The file is the test's own, so that two harness processes
// running this test at once do not contend for it.
func TestLockFileExcludesAnotherHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "port.lock")
	release, ok := lockFile(path)
	if !ok {
		t.Fatal("lockFile refused a file no one holds")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lockFile left no lock file: %v", err)
	}
	if _, again := lockFile(path); again {
		release()
		t.Fatal("lockFile claimed a file whose lock is held")
	}
	release()
	second, ok := lockFile(path)
	if !ok {
		t.Fatal("lockFile refused a file whose lock was released")
	}
	second()
}
