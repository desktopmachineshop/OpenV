package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
)

// TestLinksSuspectMigration: migration 0005 adds the suspect column to a
// pre-ledger database; pre-existing links backfill to FALSE (trusted).
func TestLinksSuspectMigration(t *testing.T) {
	db := testDB(t)

	if err := InitSchema(db); err != nil {
		t.Fatalf("InitSchema (pre-ledger simulation): %v", err)
	}
	var hasColumn bool
	if err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'links' AND column_name = 'suspect'
		)`).Scan(&hasColumn); err != nil {
		t.Fatal(err)
	}
	if hasColumn {
		t.Fatal("precondition failed: baseline schema must not have the suspect column")
	}

	legacyLink := uuid.New().String()
	if _, err := db.Exec(`
		INSERT INTO links (id, from_id, to_id, type, version)
		VALUES ($1, $2, $3, 'verifies', 1)
	`, legacyLink, uuid.New().String(), uuid.New().String()); err != nil {
		t.Fatalf("seed pre-migration link: %v", err)
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, ok := ledgerRows(t, db)[5]; !ok {
		t.Fatal("expected migration 0005 in the ledger")
	}

	var suspect bool
	if err := db.QueryRow(`SELECT suspect FROM links WHERE id = $1`, legacyLink).Scan(&suspect); err != nil {
		t.Fatalf("read backfilled suspect: %v", err)
	}
	if suspect {
		t.Error("pre-existing link backfilled as suspect; want trusted (false)")
	}
}

// TestLinkSuspectRoundTrip exercises the repository suspect operations:
// SetSuspectByArtifact flags every live link touching the artifact (either
// direction) and nothing else; SetSuspect clears one link; readers scan
// the flag back.
func TestLinkSuspectRoundTrip(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewLinkRepository(db)

	artA := uuid.New().String()
	artB := uuid.New().String()
	artC := uuid.New().String()

	mkLink := func(from, to string) *links.Link {
		l := links.NewLink(links.CreateLinkRequest{FromID: from, ToID: to, Type: "relates-to"})
		if err := repo.Save(l); err != nil {
			t.Fatalf("save link: %v", err)
		}
		return l
	}
	outgoing := mkLink(artA, artB) // A -> B
	incoming := mkLink(artC, artA) // C -> A
	unrelated := mkLink(artB, artC)

	if err := repo.SetSuspectByArtifact(artA, true); err != nil {
		t.Fatalf("SetSuspectByArtifact: %v", err)
	}

	suspectOf := func(id string) bool {
		t.Helper()
		l, err := repo.FindByID(id)
		if err != nil {
			t.Fatalf("FindByID %s: %v", id, err)
		}
		return l.Suspect
	}

	if !suspectOf(outgoing.ID) {
		t.Error("outgoing link (from = A) not marked suspect")
	}
	if !suspectOf(incoming.ID) {
		t.Error("incoming link (to = A) not marked suspect")
	}
	if suspectOf(unrelated.ID) {
		t.Error("unrelated link was marked suspect")
	}

	// List readers must surface the flag too.
	fromA, err := repo.FindByFromID(artA)
	if err != nil {
		t.Fatalf("FindByFromID: %v", err)
	}
	if len(fromA) != 1 || !fromA[0].Suspect {
		t.Errorf("FindByFromID = %+v, want one suspect link", fromA)
	}

	// Per-link confirmation clears exactly that link.
	if err := repo.SetSuspect(outgoing.ID, false); err != nil {
		t.Fatalf("SetSuspect: %v", err)
	}
	if suspectOf(outgoing.ID) {
		t.Error("confirmed link still suspect")
	}
	if !suspectOf(incoming.ID) {
		t.Error("confirming one link cleared another")
	}

	// Approval-style bulk clear.
	if err := repo.SetSuspectByArtifact(artA, false); err != nil {
		t.Fatalf("SetSuspectByArtifact(clear): %v", err)
	}
	if suspectOf(incoming.ID) {
		t.Error("bulk clear left a link suspect")
	}
}

// TestUpdateArtifactTemporalIntervals is the issue-#161 regression test:
// updating an artifact must close the archived row's validity interval
// exactly where the new row's opens — valid_to(old) == valid_from(new),
// strictly after the old row's valid_from (previously the service reused
// the stale ValidFrom, producing a zero-width archived interval).
func TestUpdateArtifactTemporalIntervals(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)

	repo := NewArtifactRepository(db)
	svc := artifacts.NewDefaultService(repo)

	created := artifacts.NewArtifact(artifacts.CreateArtifactRequest{
		ProjectID: uuid.New().String(),
		Type:      "requirement",
		Title:     "Temporal",
		Body:      "v1",
	})
	if err := repo.Save(created); err != nil {
		t.Fatalf("save artifact: %v", err)
	}

	// Ensure the clock moves past the stored (microsecond-truncated) stamp.
	time.Sleep(10 * time.Millisecond)

	body := "v2"
	if _, err := svc.UpdateArtifact(created.ID, artifacts.UpdateArtifactRequest{
		Body: &body,
	}); err != nil {
		t.Fatalf("UpdateArtifact: %v", err)
	}

	var archivedValidFrom, archivedValidTo, currentValidFrom time.Time
	if err := db.QueryRow(`
		SELECT valid_from, valid_to FROM artifacts
		WHERE id = $1 AND valid_to IS NOT NULL
	`, created.ID).Scan(&archivedValidFrom, &archivedValidTo); err != nil {
		t.Fatalf("read archived row: %v", err)
	}
	if err := db.QueryRow(`
		SELECT valid_from FROM artifacts
		WHERE id = $1 AND valid_to IS NULL
	`, created.ID).Scan(&currentValidFrom); err != nil {
		t.Fatalf("read current row: %v", err)
	}

	if !archivedValidTo.Equal(currentValidFrom) {
		t.Errorf("archived valid_to (%v) != current valid_from (%v); intervals must abut", archivedValidTo, currentValidFrom)
	}
	if !archivedValidTo.After(archivedValidFrom) {
		t.Errorf("archived interval is empty or inverted: valid_from %v, valid_to %v", archivedValidFrom, archivedValidTo)
	}
	if !currentValidFrom.After(archivedValidFrom) {
		t.Errorf("current valid_from (%v) did not advance past the original (%v)", currentValidFrom, archivedValidFrom)
	}
}
