package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEnsurePayloadFile locks in the extraction contract: create when
// missing, leave an identical file alone, replace a differing one.
func TestEnsurePayloadFile(t *testing.T) {
	dir := t.TempDir()

	wrote, err := ensurePayloadFile(dir, "agentd", []byte("v1"))
	if err != nil || !wrote {
		t.Fatalf("first write = (%v, %v), want (true, nil)", wrote, err)
	}

	wrote, err = ensurePayloadFile(dir, "agentd", []byte("v1"))
	if err != nil || wrote {
		t.Fatalf("identical rewrite = (%v, %v), want (false, nil)", wrote, err)
	}

	wrote, err = ensurePayloadFile(dir, "agentd", []byte("v2"))
	if err != nil || !wrote {
		t.Fatalf("changed rewrite = (%v, %v), want (true, nil)", wrote, err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "agentd"))
	if err != nil || string(got) != "v2" {
		t.Fatalf("content after replace = %q, %v; want v2", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "agentd.new")); !os.IsNotExist(err) {
		t.Error("temporary file left behind")
	}
}
