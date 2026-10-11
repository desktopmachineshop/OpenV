package postgres

import (
	"database/sql/driver"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/repoconns"
)

// X13's literal equality for the repo connection repository: the two reads' and
// the INSERT's statements, assembled from repoConnectionColumns, are the
// literals they replaced, byte for byte (spacing, line breaks and tabs
// included), and each method still sends its own. The statements are read off
// the wire by the recorder in pgkit_test.go, so the test needs no server and no
// database, and checks what each method sends, not a copy of it. The repository
// has no transaction: each method sends its one statement and nothing else.
// Update's, Delete's and the local paths' statements are not in this file: they
// are not replaced.

const (
	// oldRepoConnInsert, Save
	oldRepoConnInsert = `
		INSERT INTO repo_connections (
			id, project_id, name, remote_url, default_branch,
			credential_strategy, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	// oldRepoConnFindByIDSelect, FindByID
	oldRepoConnFindByIDSelect = `
		SELECT id, project_id, name, remote_url, default_branch,
			credential_strategy, created_at, updated_at
		FROM repo_connections
		WHERE id = $1
	`
	// oldRepoConnListByProjectSelect, ListByProject
	oldRepoConnListByProjectSelect = `
		SELECT id, project_id, name, remote_url, default_branch,
			credential_strategy, created_at, updated_at
		FROM repo_connections
		WHERE project_id = $1
		ORDER BY created_at
	`
)

// The list the base wrote out, over two lines.
const oldRepoConnColumns = `id, project_id, name, remote_url, default_branch,
			credential_strategy, created_at, updated_at`

// The columns are the old list.
func TestRepoConnectionColumnsAreTheOldList(t *testing.T) {
	if repoConnectionColumns != oldRepoConnColumns {
		t.Errorf("repoConnectionColumns =\n  %q\nwant\n  %q", repoConnectionColumns, oldRepoConnColumns)
	}
}

// Each method sends its old literal and nothing else, and answers as it did for
// no row: FindByID nil and no error, ListByProject a nil list (JSON null),
// never []. A malformed id is no row either for the reads, and each statement
// is sent and answers the same.
func TestRepoConnectionStatementsAreTheOldLiterals(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewRepoConnectionRepository(db)
	project := uuid.New().String()

	conn := &repoconns.RepoConnection{ID: uuid.New().String(), ProjectID: project, Name: "n"}
	if err := repo.Save(conn); err != nil {
		t.Fatalf("Save: %v", err)
	}
	wantEvents(t, "Save", rec.take(), "EXEC "+oldRepoConnInsert)

	got, err := repo.FindByID(uuid.New().String())
	if got != nil || err != nil {
		t.Errorf("FindByID with no row = %v, %v; want nil and no error", got, err)
	}
	wantEvents(t, "FindByID", rec.take(), "QUERY "+oldRepoConnFindByIDSelect)

	got, err = repo.FindByID("not-a-uuid")
	if got != nil || err != nil {
		t.Errorf("FindByID of a malformed id = %v, %v; want nil and no error", got, err)
	}
	wantEvents(t, "FindByID of a malformed id", rec.take(), "QUERY "+oldRepoConnFindByIDSelect)

	list, err := repo.ListByProject(project)
	rtWantNil(t, "ListByProject with no row", list, err)
	wantEvents(t, "ListByProject", rec.take(), "QUERY "+oldRepoConnListByProjectSelect)

	list, err = repo.ListByProject("not-a-uuid")
	rtWantNil(t, "ListByProject of a malformed project id", list, err)
	wantEvents(t, "ListByProject of a malformed project id", rec.take(), "QUERY "+oldRepoConnListByProjectSelect)
}

// The scans read their columns in the order of the list, one row each, as the
// recorder answers a statement with a row of values: FindByID's row and
// ListByProject's row each land in the fields they land in today, and
// MyLocalPath stays empty. (The scan-error places are pinned by
// repo_connection_repository_columns_test.go.)
func TestRepoConnectionScansReadTheirColumnsInOrder(t *testing.T) {
	when := time.Date(2026, 3, 14, 9, 26, 53, 0, time.UTC)
	db, rec := recorderDB(t)
	repo := NewRepoConnectionRepository(db)
	values := []driver.Value{"the-id", "the-project", "the-name", "the-url", "the-branch", "the-strategy",
		when, when.Add(time.Minute)}
	rec.answers = []recAnswer{
		{"WHERE id = $1", values},
		{"WHERE project_id = $1", values},
	}
	want := &repoconns.RepoConnection{ID: "the-id", ProjectID: "the-project", Name: "the-name",
		RemoteURL: "the-url", DefaultBranch: "the-branch", CredentialStrategy: "the-strategy",
		CreatedAt: when, UpdatedAt: when.Add(time.Minute)}

	got, err := repo.FindByID("the-id")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("FindByID = %#v, %v; want %#v", got, err, want)
	}
	list, err := repo.ListByProject("the-project")
	if err != nil || !reflect.DeepEqual(list, []*repoconns.RepoConnection{want}) {
		t.Errorf("ListByProject = %#v, %v; want [%#v]", list, err, want)
	}
}
