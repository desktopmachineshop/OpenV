package postgres

import (
	"testing"

	"github.com/openv/requirements-platform/internal/domain/sharedproducts"
	"github.com/openv/requirements-platform/internal/seeds"
)

// The starter pool against the real table, where the UNIQUE index on name_key
// is what makes a repeat boot a no-op. The unit tests in internal/seeds prove
// the logic against a fake; this proves the constraint the logic relies on
// actually exists and behaves the way the seeding assumes.
func TestSeedSharedProductPoolAgainstPostgres(t *testing.T) {
	db := testDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewSharedProductRepository(db)

	added, err := seeds.EnsureSharedProductPool(repo)
	if err != nil {
		t.Fatalf("first seed: %v", err)
	}
	if added == 0 {
		t.Fatal("first seed added nothing")
	}

	var rows int
	if err := db.QueryRow(`SELECT count(*) FROM shared_products`).Scan(&rows); err != nil {
		t.Fatalf("count after first seed: %v", err)
	}
	if rows != added {
		t.Fatalf("seed reported %d rows, table holds %d", added, rows)
	}

	// Second boot: the pool is already full, so nothing is added and nothing
	// is duplicated.
	againAdded, err := seeds.EnsureSharedProductPool(repo)
	if err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if againAdded != 0 {
		t.Fatalf("second seed added %d rows, want 0", againAdded)
	}
	var rowsAfter, distinctNames int
	if err := db.QueryRow(
		`SELECT count(*), count(DISTINCT name_key) FROM shared_products`,
	).Scan(&rowsAfter, &distinctNames); err != nil {
		t.Fatalf("count after second seed: %v", err)
	}
	if rowsAfter != rows {
		t.Fatalf("pool grew from %d to %d on a repeat boot", rows, rowsAfter)
	}
	if distinctNames != rowsAfter {
		t.Fatalf("%d rows share %d distinct names; the pool has duplicates", rowsAfter, distinctNames)
	}

	// Seeded entries are ordinary pool members: visible to a reader, and
	// carrying the vote counts every other row does.
	list, err := repo.ListVisible(sharedproducts.ListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("list the seeded pool: %v", err)
	}
	if len(list) != rowsAfter {
		t.Fatalf("listed %d of %d seeded rows; a seeded entry should be visible like any other",
			len(list), rowsAfter)
	}
	for _, p := range list {
		if p.ID == "" || p.Name == "" {
			t.Fatalf("seeded row is incomplete: %+v", p)
		}
		if p.Votes != 0 || p.Voted {
			t.Fatalf("%q starts with votes: %+v", p.Name, p)
		}
	}

	// And votable — the whole point of seeding them rather than rendering
	// them client-side.
	target := list[0]
	total, err := repo.AddVote(target.ID, "11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatalf("vote for a seeded product: %v", err)
	}
	if total != 1 {
		t.Fatalf("vote count %d after one vote, want 1", total)
	}
}
