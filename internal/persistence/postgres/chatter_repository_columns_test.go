package postgres

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/chatter"
)

// The chatter repository's columns and scans (refactor plan X13's
// characterization for chatter_repository.go, in the manner of the provider
// setting repository's), pinned as found: the nine columns FindByID and
// FindByArtifactID read back, through the one list both of them select, each
// holding a value no other does, so a swap shows; the NULLs a column may hold
// and what each read answers for them; the scan error that names a column's
// place; what a read answers for a row no one has, for a malformed id, and
// for an artifact with no entries (nil, never []); FindByArtifactID's order;
// what Save writes into each column, refuses, and logs; what Delete removes
// and refuses; and what each method hands back for a failing database. The
// existing test (chatter_repository_test.go) pins the authorship columns read
// back through FindByArtifactID and nothing else. The repository has no
// transaction (every method sends one statement), so there is none to pin.
//
// Every test that reads or writes rows gets its own database through rtDB;
// none takes a lock, so none needs NOWAIT.

// rtChatter is an entry on artifact whose every field differs from the one
// for any other n, so a swap of two columns in a scan, or of two rows, shows.
// An odd n is a user's comment, with created_by set; an even n is an agent's
// auto entry, with created_by NULL. Its created_at is rtAt(n) and its
// updated_at rtAt(100+n).
func rtChatter(artifact string, n int) *chatter.ChatterEntry {
	e := &chatter.ChatterEntry{
		ID:          uuid.New().String(),
		ArtifactID:  artifact,
		Message:     fmt.Sprintf("note %d", n),
		IsAutoEntry: n%2 == 0,
		EntryType:   fmt.Sprintf("type-%d", n),
		AuthorName:  fmt.Sprintf("author %d", n),
		CreatedAt:   rtAt(n),
		UpdatedAt:   rtAt(100 + n),
	}
	if n%2 == 1 {
		by := uuid.New().String()
		e.CreatedBy = &by
	}
	return e
}

// rtChatterSave stores an entry, failing the test on an error, and answers it.
func rtChatterSave(t *testing.T, repo *ChatterRepository, e *chatter.ChatterEntry) *chatter.ChatterEntry {
	t.Helper()
	if err := repo.Save(e); err != nil {
		t.Fatalf("Save %s: %v", e.ID, err)
	}
	return e
}

// rtChatterSansTimes is a copy of an entry with its times zeroed, so the other
// fields can be compared with rtWantSame; the times are compared with
// rtWantTimestamp.
func rtChatterSansTimes(e *chatter.ChatterEntry) chatter.ChatterEntry {
	c := *e
	c.CreatedAt, c.UpdatedAt = time.Time{}, time.Time{}
	return c
}

// rtWantChatter fails unless got is sent, every field, with each time as a
// TIMESTAMP column hands it back.
func rtWantChatter(t *testing.T, what string, got, sent *chatter.ChatterEntry) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: no entry, want %s", what, sent.ID)
	}
	rtWantTimestamp(t, what+" created_at", got.CreatedAt, sent.CreatedAt)
	rtWantTimestamp(t, what+" updated_at", got.UpdatedAt, sent.UpdatedAt)
	rtWantSame(t, what, rtChatterSansTimes(got), rtChatterSansTimes(sent))
}

// Every column FindByID reads back is the one stored: the id, the artifact,
// the message, is_auto_entry, entry_type, created_by (nil for an agent's
// entry), author_name, created_at and updated_at. Another entry's columns are
// not the ones read.
func TestChatterFindByIDReadsBackEveryColumn(t *testing.T) {
	db := rtDB(t)
	repo := NewChatterRepository(db)
	artifact, other := uuid.New().String(), uuid.New().String()

	user := rtChatterSave(t, repo, rtChatter(artifact, 1))
	agent := rtChatterSave(t, repo, rtChatter(artifact, 2))
	rtChatterSave(t, repo, rtChatter(other, 3))

	for _, want := range []*chatter.ChatterEntry{user, agent} {
		got, err := repo.FindByID(want.ID)
		if err != nil {
			t.Fatalf("FindByID(%s): %v", want.ID, err)
		}
		rtWantChatter(t, "FindByID of "+want.Message, got, want)
	}
}

// Every column FindByArtifactID reads back is the one stored, for each of an
// artifact's entries, newest first by created_at. The entries are stored out
// of that order, and another artifact's newest entry is in no list of this
// artifact.
func TestChatterFindByArtifactIDReadsBackEveryColumnNewestFirst(t *testing.T) {
	db := rtDB(t)
	repo := NewChatterRepository(db)
	artifact, other := uuid.New().String(), uuid.New().String()

	middle := rtChatterSave(t, repo, rtChatter(artifact, 2))
	oldest := rtChatterSave(t, repo, rtChatter(artifact, 1))
	newest := rtChatterSave(t, repo, rtChatter(artifact, 3))
	rtChatterSave(t, repo, rtChatter(other, 4))

	list, err := repo.FindByArtifactID(artifact)
	if err != nil || len(list) != 3 {
		t.Fatalf("FindByArtifactID = %s, %v; want three entries", rtJSON(list), err)
	}
	for i, want := range []*chatter.ChatterEntry{newest, middle, oldest} {
		rtWantChatter(t, fmt.Sprintf("FindByArtifactID[%d]", i), list[i], want)
	}
}

// A read answers ErrNotFound (the entry's own sentinel) for a row no one has,
// and for a malformed id, which noRow treats as no row. FindByArtifactID
// answers a nil list (JSON null), never [], for an artifact with no entries,
// and nil, nil for a malformed artifact id.
func TestChatterReadsAnswerNothingForNoRow(t *testing.T) {
	db := rtDB(t)
	repo := NewChatterRepository(db)
	artifact := uuid.New().String()
	rtChatterSave(t, repo, rtChatter(artifact, 1))

	got, err := repo.FindByID(uuid.New().String())
	if got != nil || err != chatter.ErrNotFound {
		t.Errorf("FindByID of an id no row has = %s, %v; want nil and chatter.ErrNotFound", rtJSON(got), err)
	}
	got, err = repo.FindByID("not-a-uuid")
	if got != nil || err != chatter.ErrNotFound {
		t.Errorf("FindByID of a malformed id = %s, %v; want nil and chatter.ErrNotFound", rtJSON(got), err)
	}

	list, err := repo.FindByArtifactID(uuid.New().String())
	rtWantNil(t, "FindByArtifactID of an artifact with no entries", list, err)
	list, err = repo.FindByArtifactID("not-a-uuid")
	rtWantNil(t, "FindByArtifactID of a malformed artifact id", list, err)
}

// Delete removes the one row it names and no other. Deleting an id no row has
// is no failure. A malformed id is refused by Postgres, and the refusal is
// handed back as it came: unlike the reads, Delete does not answer a malformed
// id as one no row has.
func TestChatterDeleteRemovesTheNamedRowOnly(t *testing.T) {
	db := rtDB(t)
	repo := NewChatterRepository(db)
	artifact := uuid.New().String()
	gone := rtChatterSave(t, repo, rtChatter(artifact, 1))
	kept := rtChatterSave(t, repo, rtChatter(artifact, 2))

	if err := repo.Delete(gone.ID); err != nil {
		t.Fatalf("Delete(%s): %v", gone.ID, err)
	}
	got, err := repo.FindByID(gone.ID)
	if got != nil || err != chatter.ErrNotFound {
		t.Errorf("FindByID of a deleted entry = %s, %v; want nil and chatter.ErrNotFound", rtJSON(got), err)
	}
	list, err := repo.FindByArtifactID(artifact)
	if err != nil || len(list) != 1 {
		t.Fatalf("FindByArtifactID after the delete = %s, %v; want the one entry kept", rtJSON(list), err)
	}
	rtWantChatter(t, "FindByArtifactID after the delete", list[0], kept)

	if err := repo.Delete(uuid.New().String()); err != nil {
		t.Errorf("Delete of an id no row has: %v, want no error", err)
	}
	rtWantRefused(t, "Delete of a malformed id", repo.Delete("not-a-uuid"))

	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM chatter`).Scan(&rows); err != nil || rows != 1 {
		t.Errorf("rows after the deletes = %d, %v; want the one entry kept", rows, err)
	}
}

// Save writes every column of a new row as sent: the id, the artifact, the
// message, is_auto_entry, entry_type, created_by (NULL when the entry has
// none), author_name, created_at and updated_at. A created_at in another zone
// is stored by its wall clock, its offset dropped rather than converted, as
// a TIMESTAMP column does.
func TestChatterSaveWritesEveryColumn(t *testing.T) {
	db := rtDB(t)
	repo := NewChatterRepository(db)
	artifact := uuid.New().String()

	user := rtChatterSave(t, repo, rtChatter(artifact, 1))
	agent := rtChatter(artifact, 2)
	agent.CreatedAt = rtAt(2).In(rtCEST)
	rtChatterSave(t, repo, agent)

	type rawRow struct {
		id, artifactID, message, entryType, authorName string
		isAutoEntry                                    bool
		createdBy                                      sql.NullString
		createdAt, updatedAt                           time.Time
	}
	read := func(e *chatter.ChatterEntry) rawRow {
		var r rawRow
		if err := db.QueryRow(`SELECT id::text, artifact_id::text, message, is_auto_entry, entry_type, created_by::text,
				author_name, created_at, updated_at FROM chatter WHERE id = $1`, e.ID).Scan(
			&r.id, &r.artifactID, &r.message, &r.isAutoEntry, &r.entryType, &r.createdBy,
			&r.authorName, &r.createdAt, &r.updatedAt); err != nil {
			t.Fatalf("read the row of %s: %v", e.ID, err)
		}
		return r
	}

	r := read(user)
	if r.id != user.ID || r.artifactID != artifact || r.message != user.Message || r.isAutoEntry != user.IsAutoEntry ||
		r.entryType != user.EntryType || r.authorName != user.AuthorName {
		t.Errorf("the row of the user's entry = %+v, want the entry as sent %+v", r, user)
	}
	if !r.createdBy.Valid || r.createdBy.String != *user.CreatedBy {
		t.Errorf("the row of the user's entry created_by = %v, want %s", r.createdBy, *user.CreatedBy)
	}
	rtWantTimestamp(t, "the row's created_at", r.createdAt, user.CreatedAt)
	rtWantTimestamp(t, "the row's updated_at", r.updatedAt, user.UpdatedAt)

	r = read(agent)
	if r.id != agent.ID || r.artifactID != artifact || r.message != agent.Message || r.isAutoEntry != agent.IsAutoEntry ||
		r.entryType != agent.EntryType || r.authorName != agent.AuthorName {
		t.Errorf("the row of the agent's entry = %+v, want the entry as sent %+v", r, agent)
	}
	if r.createdBy.Valid {
		t.Errorf("the row of the agent's entry created_by = %q, want NULL", r.createdBy.String)
	}
	rtWantTimestamp(t, "the row's created_at in another zone", r.createdAt, agent.CreatedAt)
	rtWantTimestamp(t, "the row's updated_at", r.updatedAt, agent.UpdatedAt)
}

// Save hands back what Postgres refuses as it came, stores nothing, and logs
// each failure: a malformed artifact id is refused as a malformed uuid, and
// another entry's id is the primary key's unique violation. Each failed Save
// logs one line, which names the failure.
func TestChatterSaveHandsBackWhatItRefuses(t *testing.T) {
	db := rtDB(t)
	repo := NewChatterRepository(db)
	artifact := uuid.New().String()
	stored := rtChatterSave(t, repo, rtChatter(artifact, 1))

	logged := rtCaptureLog(t)
	bad := rtChatter(artifact, 2)
	bad.ArtifactID = "not-a-uuid"
	rtWantRefused(t, "Save of a malformed artifact id", repo.Save(bad))

	clash := rtChatter(uuid.New().String(), 3)
	clash.ID = stored.ID
	rtWantPQ(t, "Save of another entry's id", repo.Save(clash), "23505", "chatter_pkey")

	if n := strings.Count(logged.String(), "Error saving chatter entry"); n != 2 {
		t.Errorf("the failed Saves logged %d lines naming the failure, want 2: %q", n, logged.String())
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM chatter`).Scan(&rows); err != nil || rows != 1 {
		t.Errorf("rows after the refused Saves = %d, %v; want the one stored entry", rows, err)
	}
}

// A NULL in a column the reads scan fails FindByID, unwrapped, with no entry,
// naming the column and its place in the scan. FindByArtifactID fails the same
// way, with no list, for every such column but artifact_id, which its WHERE
// compares: a NULL artifact_id matches no FindByArtifactID, so that read
// answers nil, nil. The id is the primary key, so no row holds it NULL, and
// created_by is the one nullable column, which both reads take as nil.
type rtChatterScanCase struct {
	column  string
	index   int
	message string
	// filtered is set for artifact_id, which FindByArtifactID's WHERE compares
	// to its argument: a NULL artifact_id matches no FindByArtifactID.
	filtered bool
}

func TestChatterNullColumnFailsTheScanNamingItsPlace(t *testing.T) {
	const str = "converting NULL to string is unsupported"
	const boolean = "sql/driver: couldn't convert <nil> (<nil>) into type bool"
	const stamp = "unsupported Scan, storing driver.Value type <nil> into type *time.Time"
	for _, c := range []rtChatterScanCase{
		{column: "artifact_id", index: 1, message: str, filtered: true},
		{column: "message", index: 2, message: str},
		{column: "is_auto_entry", index: 3, message: boolean},
		{column: "entry_type", index: 4, message: str},
		{column: "author_name", index: 6, message: str},
		{column: "created_at", index: 7, message: stamp},
		{column: "updated_at", index: 8, message: stamp},
	} {
		t.Run(c.column, func(t *testing.T) {
			db := rtDB(t)
			repo := NewChatterRepository(db)
			artifact := uuid.New().String()
			e := rtChatterSave(t, repo, rtChatter(artifact, 1))

			rtSeed(t, db, `ALTER TABLE chatter ALTER COLUMN `+c.column+` DROP NOT NULL`)
			rtSeed(t, db, `UPDATE chatter SET `+c.column+` = NULL WHERE id = $1`, e.ID)

			want := fmt.Sprintf("sql: Scan error on column index %d, name %q: %s", c.index, c.column, c.message)
			got, err := repo.FindByID(e.ID)
			if got != nil || err == nil || err.Error() != want {
				t.Errorf("FindByID over a NULL %s = %s, %v; want nil and %s", c.column, rtJSON(got), err, want)
			}
			list, err := repo.FindByArtifactID(artifact)
			if c.filtered {
				rtWantNil(t, "FindByArtifactID over a NULL artifact_id", list, err)
				return
			}
			if list != nil || err == nil || err.Error() != want {
				t.Errorf("FindByArtifactID over a NULL %s = %s, %v; want nil and %s", c.column, rtJSON(list), err, want)
			}
		})
	}
}

// A database that fails a statement for any other reason hands its error back
// as it came, with no entry and no list (nil): Save, FindByID, FindByArtifactID
// and Delete each. A failed Save logs its line. It needs no server.
func TestChatterHandsBackADatabaseFailure(t *testing.T) {
	repo := NewChatterRepository(rtClosedDB(t))
	artifact := uuid.New().String()
	logged := rtCaptureLog(t)

	rtWantClosed(t, "Save", repo.Save(rtChatter(artifact, 1)), "")
	if !strings.Contains(logged.String(), "Error saving chatter entry") {
		t.Errorf("a failed Save logged %q, want the line naming the failure", logged.String())
	}
	got, err := repo.FindByID(uuid.New().String())
	rtWantClosed(t, "FindByID", err, "")
	if got != nil {
		t.Errorf("FindByID on a failing database answered %s, want nil", rtJSON(got))
	}
	list, err := repo.FindByArtifactID(artifact)
	rtWantClosed(t, "FindByArtifactID", err, "")
	if list != nil {
		t.Errorf("FindByArtifactID on a failing database answered %s, want nil", rtJSON(list))
	}
	rtWantClosed(t, "Delete", repo.Delete(uuid.New().String()), "")
}
