//go:build !windows

package main

import "errors"

// Protocol registration is Windows-only for now. On macOS the handler comes
// from an app bundle's Info.plist; on Linux from a .desktop file — both are
// packaging concerns rather than runtime registration.
func registerProtocol() error {
	return errors.New("automatic protocol registration is only supported on Windows; on this platform run the connector directly")
}

func unregisterProtocol() error {
	return errors.New("automatic protocol registration is only supported on Windows")
}
