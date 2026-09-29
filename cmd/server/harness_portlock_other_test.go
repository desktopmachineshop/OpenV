//go:build unix && !(linux || darwin || dragonfly || freebsd || illumos || netbsd || openbsd)

package main

// lockPort claims nothing where the syscall package has no flock (AIX and
// Solaris): ports are kept apart within the test process only
// (harness_portlock_test.go has the lock).
func lockPort(int) (release func(), ok bool) { return func() {}, true }
