package main

import (
	"bytes"
	"os"
	"path/filepath"
)

// ensurePayloadFile writes content to dir/name unless an identical file is
// already there, reporting whether it wrote. The write goes through a
// temporary file and a rename, so a concurrently executing old copy is
// swapped out whole where the OS allows it (on Windows a running binary
// can't be replaced — the caller treats that as a warning, and the stale
// copy is refreshed on the next launch).
func ensurePayloadFile(dir, name string, content []byte) (bool, error) {
	target := filepath.Join(dir, name)
	if existing, err := os.ReadFile(target); err == nil && bytes.Equal(existing, content) {
		return false, nil
	}
	tmp := target + ".new"
	if err := os.WriteFile(tmp, content, 0o755); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	return true, nil
}
