package postgres

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openv/requirements-platform/internal/domain/baselines"
)

// seedProject inserts the workspace and project a baseline hangs off, and
// returns the project id.
func seedProject(t *testing.T, repo *BaselineRepository) string {
	t.Helper()
	orgID, projectID := uuid.New().String(), uuid.New().String()
	if _, err := repo.db.Exec(`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Baselines', $2)`,
		orgID, "baselines-"+orgID[:8]); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	if _, err := repo.db.Exec(`INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'Cabinet')`,
		projectID, orgID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return projectID
}

// seedUser inserts an account a baseline can be attributed to, and returns its
// id and name.
func seedUser(t *testing.T, repo *BaselineRepository, name string) string {
	t.Helper()
	id := uuid.New().String()
	_, err := repo.db.Exec(`
		INSERT INTO users (id, email, name, auth_provider, created_at, updated_at)
		VALUES ($1, $2, $3, 'password', NOW(), NOW())
	`, id, id+"@example.com", name)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}

// A baseline is the audit anchor of a project, so who captured it has to
// survive the round trip — and come back as a name rather than an id, because
// a UUID answers nobody's question about who agreed to this.
func TestABaselineRemembersWhoCapturedIt(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewBaselineRepository(db)

	author := seedUser(t, repo, "Ada Lovelace")
	projectID := seedProject(t, repo)

	captured := &baselines.Baseline{
		ID:        uuid.New().String(),
		ProjectID: projectID,
		Name:      "Design freeze — rev A",
		Snapshot:  json.RawMessage(`{"artifacts":[]}`),
		CreatedAt: time.Now().Truncate(time.Millisecond),
		CreatedBy: &author,
	}
	if err := repo.Create(captured); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.GetByID(captured.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.CreatedBy == nil || *got.CreatedBy != author {
		t.Errorf("created_by = %v, want %s", got.CreatedBy, author)
	}
	if got.CreatedByName != "Ada Lovelace" {
		t.Errorf("created_by_name = %q, want the author's name", got.CreatedByName)
	}

	// The list is where a person picks a baseline, so it has to carry the
	// author too — it is the only place the choice is actually made.
	list, err := repo.ListByProjectID(projectID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("listed %d baselines, want 1", len(list))
	}
	if list[0].CreatedByName != "Ada Lovelace" {
		t.Errorf("the list does not name the author: %q", list[0].CreatedByName)
	}
}

// Baselines captured before authorship was recorded have no author, and an
// automation holding a workspace key is nobody. Both must read back cleanly
// rather than failing the scan or inventing a name.
func TestAnUnattributedBaselineReadsBackWithoutAnAuthor(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewBaselineRepository(db)

	projectID := seedProject(t, repo)
	anonymous := &baselines.Baseline{
		ID:        uuid.New().String(),
		ProjectID: projectID,
		Name:      "Captured by an automation",
		Snapshot:  json.RawMessage(`{}`),
		CreatedAt: time.Now().Truncate(time.Millisecond),
		CreatedBy: nil,
	}
	if err := repo.Create(anonymous); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.GetByID(anonymous.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.CreatedBy != nil {
		t.Errorf("created_by = %v, want nil", *got.CreatedBy)
	}
	if got.CreatedByName != "" {
		t.Errorf("created_by_name = %q, want empty", got.CreatedByName)
	}

	list, err := repo.ListByProjectID(projectID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("an unattributed baseline vanished from the list (%d rows)", len(list))
	}
}

// Deleting an account must not take the project's history with it: the
// baseline stays, and simply stops naming anybody.
func TestDeletingTheAuthorLeavesTheBaselineStanding(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewBaselineRepository(db)

	author := seedUser(t, repo, "Departed Colleague")
	projectID := seedProject(t, repo)
	captured := &baselines.Baseline{
		ID:        uuid.New().String(),
		ProjectID: projectID,
		Name:      "Release candidate 1",
		Snapshot:  json.RawMessage(`{"artifacts":[]}`),
		CreatedAt: time.Now().Truncate(time.Millisecond),
		CreatedBy: &author,
	}
	if err := repo.Create(captured); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := db.Exec(`DELETE FROM users WHERE id = $1`, author); err != nil {
		t.Fatalf("delete the author: %v", err)
	}

	got, err := repo.GetByID(captured.ID)
	if err != nil {
		t.Fatalf("the baseline did not survive its author: %v", err)
	}
	if got.CreatedBy != nil {
		t.Errorf("created_by still points at a deleted account: %v", *got.CreatedBy)
	}
	if got.Name != "Release candidate 1" {
		t.Errorf("name = %q", got.Name)
	}
}
