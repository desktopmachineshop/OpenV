package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestEarliestPersonalOrgID pins the bootstrap workspace query against a
// real database: of the personal workspaces, the one whose member account
// was created first, by the account's created_at; "" with no error when
// there is none; and the error when the database cannot be read.
// cmd/server's TestBootstrapOrgIDRule (refactor step X7a) is its
// characterization through the boot's closure, quirks included.
func TestEarliestPersonalOrgID(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewOrgRepository(db)

	at := func(hour int) time.Time {
		return time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC).Add(time.Duration(hour) * time.Hour)
	}
	account := func(name string, hour int) string {
		t.Helper()
		id := uuid.NewString()
		if _, err := db.Exec(`INSERT INTO users (id, email, name, created_at, updated_at) VALUES ($1, $2, $3, $4, $4)`,
			id, name+"@example.test", name, at(hour)); err != nil {
			t.Fatal(err)
		}
		return id
	}
	workspace := func(orgType, member string, hour int) string {
		t.Helper()
		id := uuid.NewString()
		if _, err := db.Exec(`INSERT INTO organizations (id, name, slug, org_type, created_by, created_at, updated_at) VALUES ($1, $2, $2, $3, $4, $5, $5)`,
			id, "w-"+id, orgType, member, at(hour)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'admin')`, id, member); err != nil {
			t.Fatal(err)
		}
		return id
	}

	if id, err := repo.EarliestPersonalOrgID(); id != "" || err != nil {
		t.Fatalf("with no accounts: %q, %v; want \"\", nil", id, err)
	}

	cy, ann := account("cy", 30), account("ann", 10)
	workspace("company", ann, 1)
	if id, err := repo.EarliestPersonalOrgID(); id != "" || err != nil {
		t.Fatalf("with only a company workspace: %q, %v; want \"\", nil", id, err)
	}

	cyPersonal := workspace("personal", cy, 2)
	annPersonal := workspace("personal", ann, 50)
	if id, err := repo.EarliestPersonalOrgID(); id != annPersonal || err != nil {
		t.Fatalf("= %q, %v; want ann's personal workspace %q (the earlier account, its workspace the later), not cy's %q",
			id, err, annPersonal, cyPersonal)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if id, err := repo.EarliestPersonalOrgID(); id != "" || err == nil {
		t.Fatalf("over a closed database: %q, %v; want \"\" and the error", id, err)
	}
}
