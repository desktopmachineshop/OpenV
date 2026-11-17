package postgres

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/savedsearches"
)

// TestSavedSearchRepository: a member's saved searches round-trip, a save
// under a name already used replaces the value and keeps the id, the list is
// the member's own then the workspace's shared ones by name, scoped to the
// workspace and the scope, and a member deletes only their own.
func TestSavedSearchRepository(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewSavedSearchRepository(db)

	org, other := uuid.New().String(), uuid.New().String()
	dana, sam := uuid.New().String(), uuid.New().String()
	for i, o := range []string{org, other} {
		if _, err := db.Exec(`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Org', $2)`, o, "org-"+o[:8]+string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	for _, u := range []string{dana, sam} {
		if _, err := db.Exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, 'Member')`, u, u+"@example.com"); err != nil {
			t.Fatal(err)
		}
	}
	save := func(user, orgID, scope, name, value string, shared bool) *savedsearches.SavedSearch {
		t.Helper()
		s, err := savedsearches.New(user, orgID, scope, name, json.RawMessage(value), shared)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.Upsert(s); err != nil {
			t.Fatalf("upsert %q: %v", name, err)
		}
		return s
	}

	first := save(dana, org, "requirements", "Zeta", `{"chips":[],"text":"a"}`, false)
	again := save(dana, org, "requirements", "Zeta", `{"chips":[],"text":"b"}`, true)
	if again.ID != first.ID {
		t.Fatalf("a save under the same name made a new row: %s vs %s", again.ID, first.ID)
	}
	save(dana, org, "requirements", "alpha", `{}`, false)
	save(sam, org, "requirements", "Beta", `{}`, true)
	save(sam, org, "requirements", "Private", `{}`, false)
	save(sam, other, "requirements", "Elsewhere", `{}`, true)
	save(dana, org, "global-search", "Other scope", `{}`, false)

	list, err := repo.ListVisible(dana, org, "requirements")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range list {
		names = append(names, s.Name)
	}
	if want := []string{"alpha", "Zeta", "Beta"}; len(names) != 3 || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Fatalf("list = %v, want %v", names, want)
	}
	var v map[string]interface{}
	if err := json.Unmarshal(list[1].Value, &v); err != nil || v["text"] != "b" || !list[1].Shared {
		t.Fatalf("replaced value = %s shared %v", list[1].Value, list[1].Shared)
	}

	if n, _ := repo.CountForUser(dana, org, "requirements"); n != 2 {
		t.Fatalf("count = %d, want 2", n)
	}
	if n, _ := repo.DeleteForUser(sam, org, first.ID); n != 0 {
		t.Fatal("another member deleted a shared search")
	}
	if n, _ := repo.DeleteForUser(dana, org, "not-a-uuid"); n != 0 {
		t.Fatal("a malformed id deleted something")
	}
	if n, err := repo.DeleteForUser(dana, org, first.ID); n != 1 || err != nil {
		t.Fatalf("owner delete = %d, %v", n, err)
	}
}
