package postgres

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/repoconns"
)

// The repo connection repository's columns (refactor plan X13's
// characterization for repo_connection_repository.go, in the manner of the
// chatter repository's), pinned as found: the eight columns FindByID and
// ListByProject read back, each holding a value no other does, so a swap shows;
// what Save writes into each column; the wall clock a TIMESTAMP holds for a
// time in another zone; the NULLs the columns may hold once the schema is
// loosened, and the scan error that names each place, for both reads; what the
// reads answer for a connection no one has, for an empty project and for a
// malformed id; the order and project of the list; the local path the reads
// leave empty; and a database that fails. No test pinned these columns before:
// the stamps tests (agent_suite_utc_stamps_test.go) read the two timestamps
// through the service and the malformed-id test only the list's nil.
//
// The repository has no transaction (each method sends one statement), so none
// is pinned. Every test that reads or writes rows gets its own database
// through rtDB; none takes a lock, so none needs NOWAIT.

// rtRCOf is a connection whose every field differs from the one for any other
// n, so a swap of two columns in a scan or an insert shows. Its created_at is
// rtAt(n) and its updated_at rtAt(100+n).
func rtRCOf(project string, n int) *repoconns.RepoConnection {
	return &repoconns.RepoConnection{
		ID:                 uuid.New().String(),
		ProjectID:          project,
		Name:               fmt.Sprintf("name %d", n),
		RemoteURL:          fmt.Sprintf("https://example.com/%d.git", n),
		DefaultBranch:      fmt.Sprintf("branch-%d", n),
		CredentialStrategy: fmt.Sprintf("strategy-%d", n),
		CreatedAt:          rtAt(n),
		UpdatedAt:          rtAt(100 + n),
	}
}

// rtRCSeedProject inserts a project for connections to belong to and answers its id.
func rtRCSeedProject(t *testing.T, db *sql.DB) string {
	t.Helper()
	id := uuid.New().String()
	rtSeedProject(t, db, id, uuid.New().String(), "Connections")
	return id
}

// rtRCSeed inserts a connection by column name, the way no method of the
// repository does, so a read is pinned on a row Save did not write.
func rtRCSeed(t *testing.T, db *sql.DB, c *repoconns.RepoConnection) {
	t.Helper()
	rtSeed(t, db, `INSERT INTO repo_connections (id, project_id, name, remote_url, default_branch, credential_strategy, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		c.ID, c.ProjectID, c.Name, c.RemoteURL, c.DefaultBranch, c.CredentialStrategy, c.CreatedAt, c.UpdatedAt)
}

// rtRCWantSame fails unless got is sent, field by field, with each time as a
// TIMESTAMP column hands it back, so a swap names the field it hit.
func rtRCWantSame(t *testing.T, what string, got, sent *repoconns.RepoConnection) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: no connection, want %s", what, sent.ID)
	}
	if got.ID != sent.ID {
		t.Errorf("%s id = %q, want %q", what, got.ID, sent.ID)
	}
	if got.ProjectID != sent.ProjectID {
		t.Errorf("%s project_id = %q, want %q", what, got.ProjectID, sent.ProjectID)
	}
	if got.Name != sent.Name {
		t.Errorf("%s name = %q, want %q", what, got.Name, sent.Name)
	}
	if got.RemoteURL != sent.RemoteURL {
		t.Errorf("%s remote_url = %q, want %q", what, got.RemoteURL, sent.RemoteURL)
	}
	if got.DefaultBranch != sent.DefaultBranch {
		t.Errorf("%s default_branch = %q, want %q", what, got.DefaultBranch, sent.DefaultBranch)
	}
	if got.CredentialStrategy != sent.CredentialStrategy {
		t.Errorf("%s credential_strategy = %q, want %q", what, got.CredentialStrategy, sent.CredentialStrategy)
	}
	if got.MyLocalPath != "" {
		t.Errorf("%s my_local_path = %q, want it left empty (a user-scoped read fills it)", what, got.MyLocalPath)
	}
	rtWantTimestamp(t, what+" created_at", got.CreatedAt, sent.CreatedAt)
	rtWantTimestamp(t, what+" updated_at", got.UpdatedAt, sent.UpdatedAt)
}

// FindByID and ListByProject read back every column, each with a value no
// other column holds, from rows written by SQL rather than by Save, so the
// reads are pinned on their own. The list holds the project's own connections
// alone, oldest created_at first however they were inserted.
func TestRepoConnectionReadsBackEveryColumn(t *testing.T) {
	db := rtDB(t)
	repo := NewRepoConnectionRepository(db)
	project, other := rtRCSeedProject(t, db), rtRCSeedProject(t, db)
	third, first, second := rtRCOf(project, 3), rtRCOf(project, 1), rtRCOf(project, 2)
	foreign := rtRCOf(other, 4)
	for _, c := range []*repoconns.RepoConnection{third, first, second, foreign} {
		rtRCSeed(t, db, c)
	}

	for _, want := range []*repoconns.RepoConnection{first, second, third, foreign} {
		got, err := repo.FindByID(want.ID)
		if err != nil {
			t.Fatalf("FindByID of %s: %v", want.Name, err)
		}
		rtRCWantSame(t, "FindByID of "+want.Name, got, want)
	}

	list, err := repo.ListByProject(project)
	if err != nil {
		t.Fatalf("ListByProject: %v", err)
	}
	want := []*repoconns.RepoConnection{first, second, third}
	if len(list) != len(want) {
		t.Fatalf("ListByProject listed %s, want %d connections", rtJSON(list), len(want))
	}
	for i, w := range want {
		rtRCWantSame(t, fmt.Sprintf("ListByProject[%d]", i), list[i], w)
	}
}

// Save writes every field into its own column: read back by column name, the
// stored row is what was sent, and both reads answer the same. A time in
// another zone is stored as its wall clock, the offset dropped.
func TestRepoConnectionSaveWritesEveryColumn(t *testing.T) {
	db := rtDB(t)
	repo := NewRepoConnectionRepository(db)
	project := rtRCSeedProject(t, db)
	sent := rtRCOf(project, 5)
	sent.CreatedAt = rtAt(5).In(rtCEST)
	sent.UpdatedAt = rtAt(105).In(rtCEST)

	if err := repo.Save(sent); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var id, projectID, name, remote, branch, strategy string
	var createdAt, updatedAt time.Time
	err := db.QueryRow(`SELECT id, project_id, name, remote_url, default_branch, credential_strategy, created_at, updated_at
		FROM repo_connections WHERE id = $1`, sent.ID).Scan(&id, &projectID, &name, &remote, &branch, &strategy, &createdAt, &updatedAt)
	if err != nil {
		t.Fatalf("reading the stored connection: %v", err)
	}
	for _, c := range []struct{ column, got, want string }{
		{"id", id, sent.ID},
		{"project_id", projectID, sent.ProjectID},
		{"name", name, sent.Name},
		{"remote_url", remote, sent.RemoteURL},
		{"default_branch", branch, sent.DefaultBranch},
		{"credential_strategy", strategy, sent.CredentialStrategy},
	} {
		if c.got != c.want {
			t.Errorf("stored %s = %q, want %q", c.column, c.got, c.want)
		}
	}
	rtWantTimestamp(t, "stored created_at", createdAt, sent.CreatedAt)
	rtWantTimestamp(t, "stored updated_at", updatedAt, sent.UpdatedAt)

	got, err := repo.FindByID(sent.ID)
	if err != nil {
		t.Fatalf("FindByID after Save: %v", err)
	}
	rtRCWantSame(t, "FindByID after Save", got, sent)
	list, err := repo.ListByProject(project)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListByProject after Save = %s, %v; want the one connection", rtJSON(list), err)
	}
	rtRCWantSame(t, "ListByProject after Save", list[0], sent)

	// A second Save of the same id is Postgres's refusal, handed back as it came.
	rtWantPQ(t, "Save of an id the table has", repo.Save(sent), "23505", "repo_connections_pkey")
	// A project no row has is the foreign key's.
	orphan := rtRCOf(uuid.New().String(), 6)
	rtWantPQ(t, "Save in a project no row has", repo.Save(orphan), "23503", "repo_connections_project_id_fkey")
}

// FindByID answers nil and no error for a connection no row has, and for a
// malformed id (noRow counts Postgres's refusal as no row); ListByProject
// answers nil, never an empty list, and no error for a project with none, for
// one no row has and for a malformed id.
func TestRepoConnectionAnswersNilForNone(t *testing.T) {
	db := rtDB(t)
	repo := NewRepoConnectionRepository(db)
	project := rtRCSeedProject(t, db)

	got, err := repo.FindByID(uuid.New().String())
	if got != nil || err != nil {
		t.Errorf("FindByID with no row = %s, %v; want nil and no error", rtJSON(got), err)
	}
	for _, id := range malformedIDs {
		got, err = repo.FindByID(id)
		if got != nil || err != nil {
			t.Errorf("FindByID of the malformed id %q = %s, %v; want nil and no error", id, rtJSON(got), err)
		}
	}

	list, err := repo.ListByProject(project)
	rtWantNil(t, "ListByProject of a project with none", list, err)
	list, err = repo.ListByProject(uuid.New().String())
	rtWantNil(t, "ListByProject of a project no row has", list, err)
	for _, id := range malformedIDs {
		list, err = repo.ListByProject(id)
		rtWantNil(t, fmt.Sprintf("ListByProject of the malformed id %q", id), list, err)
	}
}

// A column the schema forbids NULL, here one a test has let take it, fails the
// read that scans it, naming the column and its place: name the third,
// remote_url the fourth, default_branch the fifth, credential_strategy the
// sixth, created_at and updated_at the seventh and eighth (id and project_id
// are keys, which cannot be NULL). FindByID returns the scan's error as it
// came, with no connection, and ListByProject the same with no list.
func TestRepoConnectionNullColumnsFailTheScanNamingItsPlace(t *testing.T) {
	const str = "converting NULL to string is unsupported"
	const stamp = "unsupported Scan, storing driver.Value type <nil> into type *time.Time"
	for _, c := range []struct {
		column  string
		index   int // the column's place in the list, from 0
		message string
	}{
		{"name", 2, str},
		{"remote_url", 3, str},
		{"default_branch", 4, str},
		{"credential_strategy", 5, str},
		{"created_at", 6, stamp},
		{"updated_at", 7, stamp},
	} {
		t.Run(c.column, func(t *testing.T) {
			db := rtDB(t)
			repo := NewRepoConnectionRepository(db)
			project := rtRCSeedProject(t, db)
			conn := rtRCOf(project, 1)
			rtRCSeed(t, db, conn)
			rtSeed(t, db, `ALTER TABLE repo_connections ALTER COLUMN `+c.column+` DROP NOT NULL`)
			rtSeed(t, db, `UPDATE repo_connections SET `+c.column+` = NULL WHERE id = $1`, conn.ID)

			want := fmt.Sprintf("sql: Scan error on column index %d, name %q: %s", c.index, c.column, c.message)
			got, err := repo.FindByID(conn.ID)
			if got != nil || err == nil || err.Error() != want {
				t.Errorf("FindByID over a NULL %s = %s, %v; want nil and %s", c.column, rtJSON(got), err, want)
			}
			list, err := repo.ListByProject(project)
			if list != nil || err == nil || err.Error() != want {
				t.Errorf("ListByProject over a NULL %s = %s, %v; want nil and %s", c.column, rtJSON(list), err, want)
			}
		})
	}
}

// The reads and Save hand a failing database's error back as it came, with no
// connection, no list and no write. It needs no server.
func TestRepoConnectionHandsBackADatabaseFailure(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewRepoConnectionRepository(db)
	project := uuid.New().String()

	rec.failOn = "FROM repo_connections"
	if got, err := repo.FindByID(uuid.New().String()); got != nil || err != errRecordedStatement {
		t.Errorf("FindByID over a failing database = %s, %v; want nil and the database's error as it came", rtJSON(got), err)
	}
	if got, err := repo.ListByProject(project); got != nil || err != errRecordedStatement {
		t.Errorf("ListByProject over a failing database = %s, %v; want nil and the database's error as it came", rtJSON(got), err)
	}

	rec.failOn = "INSERT INTO repo_connections"
	if err := repo.Save(rtRCOf(project, 1)); err != errRecordedStatement {
		t.Errorf("Save over a failing database = %v; want the database's error as it came", err)
	}
}
