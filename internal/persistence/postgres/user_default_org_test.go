package postgres

import (
	"testing"

	"github.com/google/uuid"
)

// The default workspace round-trips, clears on "", and clears itself when
// the workspace it named is gone — a choice must never point at nothing.
func TestDefaultOrgRoundTripsAndClearsWithTheWorkspace(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewUserRepository(db)

	userID := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO users (id, email, name, auth_provider, created_at, updated_at) VALUES ($1, $2, 'Ada', 'password', NOW(), NOW())`, userID, userID+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	orgID := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Acme', $2)`, orgID, "acme-"+orgID[:8]); err != nil {
		t.Fatalf("seed org: %v", err)
	}

	if err := repo.SetDefaultOrg(userID, orgID); err != nil {
		t.Fatalf("set: %v", err)
	}
	u, err := repo.FindUserByID(userID)
	if err != nil || u.DefaultOrgID != orgID {
		t.Fatalf("read back %q (%v), want %s", u.DefaultOrgID, err, orgID)
	}

	if err := repo.SetDefaultOrg(userID, ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if u, _ = repo.FindUserByID(userID); u.DefaultOrgID != "" {
		t.Fatalf("after clearing: %q", u.DefaultOrgID)
	}

	if err := repo.SetDefaultOrg(userID, orgID); err != nil {
		t.Fatalf("set again: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM organizations WHERE id = $1`, orgID); err != nil {
		t.Fatalf("delete org: %v", err)
	}
	if u, _ = repo.FindUserByID(userID); u.DefaultOrgID != "" {
		t.Fatalf("a purged workspace is still the default: %q", u.DefaultOrgID)
	}
}
