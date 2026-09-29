package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openv/requirements-platform/internal/domain/proposals"
)

// TestProposalListFiltersByWorkspaceBeforeItsLimit is the regression test
// for a workspace admin's proposal list that came back short: with no
// project named, the repository read the newest 500 proposals of every
// workspace and the handler kept those of the admin's own, so once other
// workspaces held 500 newer proposals the admin's list was empty. The
// workspace filter is now part of the query, ahead of its limit (OpenV
// REQ-17, REQ-79).
func TestProposalListFiltersByWorkspaceBeforeItsLimit(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)

	orgA, orgB := uuid.New().String(), uuid.New().String()
	projA, projB := uuid.New().String(), uuid.New().String()
	agentID, runA, runB := uuid.New().String(), uuid.New().String(), uuid.New().String()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO organizations (id, name, slug) VALUES ($1, 'A', 'org-a'), ($2, 'B', 'org-b')`, []any{orgA, orgB}},
		{`INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'A''s'), ($3, $4, 'B''s')`, []any{projA, orgA, projB, orgB}},
		{`INSERT INTO agents (id, slug, name, provider) VALUES ($1, 'tc', 'TC', 'claude-code')`, []any{agentID}},
		{`INSERT INTO agent_runs (id, org_id, agent_id, project_id, prompt) VALUES ($1, $2, $3, $4, 'go'), ($5, $6, $3, $7, 'go')`,
			[]any{runA, orgA, agentID, projA, runB, orgB, projB}},
	} {
		if _, err := db.Exec(q.sql, q.args...); err != nil {
			t.Fatalf("seed %q: %v", q.sql, err)
		}
	}

	repo := NewProposalRepository(db)
	older := time.Now().Add(-time.Hour)
	mine := &proposals.Proposal{ID: uuid.New().String(), RunID: runA, ProjectID: projA, Op: proposals.OpCreateArtifact,
		Payload: map[string]interface{}{"title": "A's"}, Status: proposals.StatusPending, CreatedAt: older}
	if err := repo.Save(mine); err != nil {
		t.Fatalf("save A's proposal: %v", err)
	}
	// 500 newer proposals in B, as many as the list's limit.
	if _, err := db.Exec(`
		INSERT INTO agent_proposals (id, run_id, project_id, op, payload, status, created_at)
		SELECT gen_random_uuid(), $1, $2, 'create_artifact', '{}', 'pending', NOW() - n * INTERVAL '1 second'
		FROM generate_series(1, 500) AS n`, runB, projB); err != nil {
		t.Fatalf("seed B's proposals: %v", err)
	}

	list, err := repo.List(orgA, "", "", "")
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	if len(list) != 1 || list[0].ID != mine.ID {
		t.Fatalf("A's list holds %d proposals, want only A's own %s", len(list), mine.ID)
	}
	list, err = repo.List(orgA, "", proposals.StatusPending, "")
	if err != nil {
		t.Fatalf("list A's pending: %v", err)
	}
	if len(list) != 1 || list[0].ID != mine.ID {
		t.Fatalf("A's pending list holds %d proposals, want only A's own %s", len(list), mine.ID)
	}
	list, err = repo.List(orgB, "", "", "")
	if err != nil {
		t.Fatalf("list B: %v", err)
	}
	if len(list) != 500 {
		t.Fatalf("B's list holds %d proposals, want its 500", len(list))
	}
	for _, p := range list {
		if p.ProjectID != projB {
			t.Fatalf("B's list holds %s of project %s", p.ID, p.ProjectID)
		}
	}
	// With no workspace named, every workspace's proposals count towards the
	// limit, as the run and project filters' callers expect.
	list, err = repo.List("", "", "", "")
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(list) != 500 {
		t.Fatalf("the unfiltered list holds %d proposals, want the limit of 500", len(list))
	}
}
