package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// RemoveOrg removes a purged workspace's agent definitions directory, its
// .trash included, and nothing else (#379 bug 157): not another workspace's,
// not the agents directory, not what a symbolic link in its place points at,
// and nothing for an id that is not a workspace's.
func TestRemoveOrgRemovesTheWorkspaceDirectoryAlone(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, "agents")
	svc, err := NewFileService(agentsDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	const purged, other, linked = "6f1c2c1e-5b8a-4f0e-9a51-0c2a8d0e7b11", "0b7e4a52-3c1d-4e8f-8a6b-9d2c1f0e3a44",
		"9e3d1c2b-7a6f-4e5d-8c4b-3a2f1e0d9c88"
	write := func(path string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("---\nslug: x\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(agentsDir, purged, "drafter.md"))
	write(filepath.Join(agentsDir, purged, ".trash", "reviewer-1700000000.md"))
	write(filepath.Join(agentsDir, other, "drafter.md"))
	write(filepath.Join(agentsDir, "legacy.md"))
	outside := filepath.Join(root, "elsewhere", "keep.md")
	write(outside)
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(agentsDir, linked)); err != nil {
		t.Fatal(err)
	}

	if err := svc.RemoveOrg(purged); err != nil {
		t.Fatalf("RemoveOrg(%s): %v", purged, err)
	}
	if _, err := os.Lstat(filepath.Join(agentsDir, purged)); !os.IsNotExist(err) {
		t.Errorf("the purged workspace's directory is still there (%v), want it gone with its .trash", err)
	}
	if err := svc.RemoveOrg(purged); err != nil {
		t.Errorf("RemoveOrg of a directory already gone: %v, want no error", err)
	}
	if err := svc.RemoveOrg(linked); err != nil {
		t.Fatalf("RemoveOrg(%s), a symbolic link: %v", linked, err)
	}
	if _, err := os.Lstat(filepath.Join(agentsDir, linked)); !os.IsNotExist(err) {
		t.Errorf("the symbolic link in a workspace's place is still there (%v), want it removed", err)
	}

	for _, id := range []string{"", ".", "..", "../agents", "../elsewhere", ".trash", "a/b", "legacy.md",
		strings.ToUpper(other), "{" + other + "}", "urn:uuid:" + other, strings.ReplaceAll(other, "-", ""),
		other + "/..", other + "/../..", "/" + other} {
		if err := svc.RemoveOrg(id); err == nil {
			t.Errorf("RemoveOrg(%q) answered no error, want it refused as not a workspace id", id)
		}
	}

	for _, kept := range []string{filepath.Join(agentsDir, other, "drafter.md"), filepath.Join(agentsDir, "legacy.md"), outside} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("%s: %v, want it kept", kept, err)
		}
	}
}

// A FileService with no directory, which NewFileService never builds, still
// removes nothing: the path would be relative to wherever the server runs.
func TestRemoveOrgRefusesWithoutAnAgentsDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	const id = "6f1c2c1e-5b8a-4f0e-9a51-0c2a8d0e7b11"
	if err := os.MkdirAll(filepath.Join(dir, id), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (&FileService{}).RemoveOrg(id); err == nil {
		t.Error("RemoveOrg with no agents directory answered no error, want a refusal")
	}
	if _, err := os.Stat(filepath.Join(dir, id)); err != nil {
		t.Errorf("the directory named after the workspace in the working directory: %v, want it kept", err)
	}
}
