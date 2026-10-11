package postgres

import (
	"database/sql/driver"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/chatter"
)

// X13's literal equality for the chatter repository: the two reads' and the
// INSERT's statements, assembled from chatterColumns, are the literals they
// replaced, byte for byte (spacing, line breaks and tabs included), and each
// method still sends its own. The statements are read off the wire by the
// recorder in pgkit_test.go, so the test needs no server and no database, and
// checks what each method sends, not a copy of it. The repository has no
// transaction: each method sends its one statement and nothing else. Delete's
// statement is not in this file: it is not replaced.

const (
	// oldChatterInsert, Save
	oldChatterInsert = `
		INSERT INTO chatter (id, artifact_id, message, is_auto_entry, entry_type, created_by, author_name, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	// oldChatterFindByIDSelect, FindByID
	oldChatterFindByIDSelect = `
		SELECT id, artifact_id, message, is_auto_entry, entry_type, created_by, author_name, created_at, updated_at
		FROM chatter
		WHERE id = $1
	`
	// oldChatterFindByArtifactIDSelect, FindByArtifactID
	oldChatterFindByArtifactIDSelect = `
		SELECT id, artifact_id, message, is_auto_entry, entry_type, created_by, author_name, created_at, updated_at
		FROM chatter
		WHERE artifact_id = $1
		ORDER BY created_at DESC
	`
)

// The list the base wrote out, on one line.
const oldChatterColumns = `id, artifact_id, message, is_auto_entry, entry_type, created_by, author_name, created_at, updated_at`

// The columns are the old list.
func TestChatterColumnsAreTheOldList(t *testing.T) {
	if chatterColumns != oldChatterColumns {
		t.Errorf("chatterColumns =\n  %q\nwant\n  %q", chatterColumns, oldChatterColumns)
	}
}

// Each method sends its old literal and nothing else, and answers as it did
// for no row: FindByID chatter.ErrNotFound, FindByArtifactID a nil list (JSON
// null), never []. A malformed id is no row either for the reads, and each
// statement is sent and answers the same.
func TestChatterStatementsAreTheOldLiterals(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewChatterRepository(db)
	artifact := uuid.New().String()

	entry := chatter.NewChatterEntry(artifact, "a note", false, "comment")
	if err := repo.Save(entry); err != nil {
		t.Fatalf("Save: %v", err)
	}
	wantEvents(t, "Save", rec.take(), "EXEC "+oldChatterInsert)

	got, err := repo.FindByID(uuid.New().String())
	if got != nil || err != chatter.ErrNotFound {
		t.Errorf("FindByID with no row = %v, %v; want nil and chatter.ErrNotFound", got, err)
	}
	wantEvents(t, "FindByID", rec.take(), "QUERY "+oldChatterFindByIDSelect)

	got, err = repo.FindByID("not-a-uuid")
	if got != nil || err != chatter.ErrNotFound {
		t.Errorf("FindByID of a malformed id = %v, %v; want nil and chatter.ErrNotFound", got, err)
	}
	wantEvents(t, "FindByID of a malformed id", rec.take(), "QUERY "+oldChatterFindByIDSelect)

	list, err := repo.FindByArtifactID(artifact)
	rtWantNil(t, "FindByArtifactID with no row", list, err)
	wantEvents(t, "FindByArtifactID", rec.take(), "QUERY "+oldChatterFindByArtifactIDSelect)

	list, err = repo.FindByArtifactID("not-a-uuid")
	rtWantNil(t, "FindByArtifactID of a malformed artifact id", list, err)
	wantEvents(t, "FindByArtifactID of a malformed artifact id", rec.take(), "QUERY "+oldChatterFindByArtifactIDSelect)
}

// The scans read their columns in the order of the list, one row each, as the
// recorder answers a statement with a row of values: FindByID's row and
// FindByArtifactID's row each land in the fields they land in today, a NULL
// created_by reads as nil. (The scan-error places are pinned by
// chatter_repository_columns_test.go.)
func TestChatterScansReadTheirColumnsInOrder(t *testing.T) {
	when := time.Date(2026, 3, 14, 9, 26, 53, 0, time.UTC)
	for _, c := range []struct {
		name      string
		createdBy driver.Value
		want      *string
	}{
		{name: "a user's entry", createdBy: "the-by", want: strPtr("the-by")},
		{name: "an agent's entry", createdBy: nil, want: nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			db, rec := recorderDB(t)
			repo := NewChatterRepository(db)
			values := []driver.Value{"the-id", "the-artifact", "the-message", true, "the-type", c.createdBy,
				"the-author", when, when.Add(time.Minute)}
			rec.answers = []recAnswer{
				{"WHERE id = $1", values},
				{"WHERE artifact_id = $1", values},
			}
			want := &chatter.ChatterEntry{ID: "the-id", ArtifactID: "the-artifact", Message: "the-message",
				IsAutoEntry: true, EntryType: "the-type", CreatedBy: c.want, AuthorName: "the-author",
				CreatedAt: when, UpdatedAt: when.Add(time.Minute)}

			got, err := repo.FindByID("the-id")
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("FindByID = %#v, %v; want %#v", got, err, want)
			}
			list, err := repo.FindByArtifactID("the-artifact")
			if err != nil || !reflect.DeepEqual(list, []*chatter.ChatterEntry{want}) {
				t.Errorf("FindByArtifactID = %#v, %v; want [%#v]", list, err, want)
			}
		})
	}
}

// strPtr is a pointer to s, for an expected created_by.
func strPtr(s string) *string { return &s }
