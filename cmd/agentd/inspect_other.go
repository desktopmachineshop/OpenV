//go:build !linux

package main

// forbidInspection does nothing outside Linux: on Windows and macOS a
// process running as the same user can read agentd's environment through
// the operating system, and only running agents as another user stops it.
func forbidInspection() error { return nil }
