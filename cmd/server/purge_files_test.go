package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// purgingOrgs purges the workspaces it lists, answering each one's stored
// files, and fails the purge of failing.
type purgingOrgs struct {
	orgs.Repository
	expired []string
	files   map[string][]string
	failing string
}

func (f *purgingOrgs) ListExpiredDeletedOrgIDs(time.Time) ([]string, error) { return f.expired, nil }

func (f *purgingOrgs) PurgeOrg(id string) ([]string, error) {
	if id == f.failing {
		return nil, errors.New("the database went away")
	}
	return f.files[id], nil
}

// The purge removes the stored files of the workspaces it purged, once
// their purges have committed (#379 bug 143: the files of a purged
// workspace, its figures, evidence and logo, stayed on disk), those of a
// workspace purged beside one whose purge failed included. A file already
// gone, and one that will not go, are logged and leave the rest to go.
func TestThePurgeRemovesThePurgedWorkspacesFiles(t *testing.T) {
	dir := t.TempDir()
	figure, evidence, logo := filepath.Join(dir, "a_figure.png"), filepath.Join(dir, "evidence-a"), filepath.Join(dir, "org-logos", "a.png")
	stuck := filepath.Join(dir, "stuck") // a directory with a file in it, which os.Remove refuses
	kept := filepath.Join(dir, "b_figure.png")
	for _, f := range []string{figure, evidence, logo, filepath.Join(stuck, "inside"), kept} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	repo := &purgingOrgs{
		expired: []string{"org-a", "org-b", "org-c"},
		files: map[string][]string{
			"org-a": {evidence, figure, stuck, filepath.Join(dir, "already gone")},
			"org-b": {kept},
			"org-c": {logo},
		},
		failing: "org-b",
	}
	// Cancelled already: the loop purges once, at once, and returns.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	agentService, err := agents.NewFileService(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	runPurgeLoop(ctx, orgs.NewDefaultService(repo), agentService)

	for _, f := range []string{figure, evidence, logo} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("%s after the purge: %v, want it removed", filepath.Base(f), err)
		}
	}
	for _, f := range []string{kept, stuck} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("%s after the purge: %v, want it left", filepath.Base(f), err)
		}
	}
}
