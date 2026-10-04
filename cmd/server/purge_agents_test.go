package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/persistence/postgres"
)

// The workspace purge removes a purged workspace's agent definitions
// directory, its .trash included, once the purge has committed (#379 bug
// 157). Left on disk, the definitions outlived the workspace, and the next
// boot's SyncAllFromDisk registered them again as agents of a workspace that
// no longer exists. A workspace still inside its grace period keeps its
// definitions. Against a real database (OPENV_TEST_DATABASE_URL; skipped
// when unset), the purge loop runs its first purge and stops, its context
// already done.
func TestThePurgeRemovesAPurgedWorkspacesAgentDefinitions(t *testing.T) {
	db := freshDatabase(t)
	conn, err := postgres.Connect(db.url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := postgres.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	agentRepo := postgres.NewAgentRepository(conn)
	agentsDir := filepath.Join(t.TempDir(), "agents")
	agentService, err := agents.NewFileService(agentsDir, agentRepo)
	if err != nil {
		t.Fatal(err)
	}

	const grace = orgs.DeletionGraceDays * 24 * time.Hour
	purged, kept := uuid.NewString(), uuid.NewString()
	for id, deleted := range map[string]time.Duration{purged: grace + time.Hour, kept: grace - time.Hour} {
		if _, err := conn.Exec(`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Workspace', $2)`, id, "w-"+id); err != nil {
			t.Fatal(err)
		}
		if _, err := agentService.SaveDefinition(id, &agents.Definition{Slug: "drafter", Name: "Drafter", Provider: "claude",
			AllowedTools: []string{"Read"}, SystemPrompt: "Draft."}); err != nil {
			t.Fatal(err)
		}
		if _, err := agentService.SaveDefinition(id, &agents.Definition{Slug: "reviewer", Name: "Reviewer", Provider: "claude",
			AllowedTools: []string{"Read"}, SystemPrompt: "Review."}); err != nil {
			t.Fatal(err)
		}
		if err := agentService.Delete(id, "reviewer"); err != nil { // into the workspace's .trash
			t.Fatal(err)
		}
		if _, err := conn.Exec(`UPDATE organizations SET deleted_at = $2 WHERE id = $1`, id, time.Now().UTC().Add(-deleted)); err != nil {
			t.Fatal(err)
		}
	}
	trash, err := os.ReadDir(filepath.Join(agentsDir, purged, ".trash"))
	if err != nil || len(trash) != 1 {
		t.Fatalf("the purged workspace's .trash: %v, %v; want the deleted reviewer in it", trash, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runPurgeLoop(ctx, orgs.NewDefaultService(postgres.NewOrgRepository(conn)), agentService)

	var left int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM organizations WHERE id = $1`, purged).Scan(&left); err != nil || left != 0 {
		t.Fatalf("the workspace past its grace period: %d rows left (%v), want it purged", left, err)
	}
	if _, err := os.Lstat(filepath.Join(agentsDir, purged)); !os.IsNotExist(err) {
		t.Errorf("the purged workspace's agent definitions directory: %v, want it removed, .trash and all", err)
	}
	if _, err := os.Stat(filepath.Join(agentsDir, kept, "drafter.md")); err != nil {
		t.Errorf("the definitions of a workspace inside its grace period: %v, want them kept", err)
	}

	// The next boot syncs the agents directory into the registry.
	if err := agentService.SyncAllFromDisk(); err != nil {
		t.Fatal(err)
	}
	if registered, err := agentRepo.List(purged); err != nil || len(registered) != 0 {
		t.Errorf("after the next boot's sync, the purged workspace has %d agents (%v), want none", len(registered), err)
	}
	if registered, err := agentRepo.List(kept); err != nil || len(registered) != 1 {
		t.Errorf("after the next boot's sync, the workspace inside its grace period has %d agents (%v), want its drafter", len(registered), err)
	}
}
