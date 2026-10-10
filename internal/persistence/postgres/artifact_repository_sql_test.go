package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
)

// X13a's literal equality: every statement the artifact repository builds
// from artifactColumns is the literal it replaced, byte for byte (spacing and
// line breaks included), and Save and Update, now on withTx, begin, commit
// and roll back exactly where they did. The statements are read off the
// wire by the recorder in pgkit_test.go, so the test needs no server and
// checks what each method sends, not a copy of it.

// The eight statements X13a assembles from artifactColumns, as the base
// (480d1dd) wrote them, byte for byte: each is the literal the method held.
const (
	// oldSaveInsert, Save
	oldSaveInsert = `
		INSERT INTO artifacts (id, project_id, parent_id, type, ref, title, body, sort_order, status, attributes, version, valid_from, valid_to, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`
	// oldFindByIDSelect, FindByID
	oldFindByIDSelect = `
		SELECT id, project_id, parent_id, type, ref, title, body, sort_order, status, attributes, version, valid_from, valid_to, created_at, updated_at
		FROM artifacts
		WHERE id = $1 AND valid_to IS NULL
	`
	// oldFindByProjectIDSelect, FindByProjectID
	oldFindByProjectIDSelect = `
		SELECT id, project_id, parent_id, type, ref, title, body, sort_order, status, attributes, version, valid_from, valid_to, created_at, updated_at
		FROM artifacts
		WHERE project_id = $1 AND valid_to IS NULL
		ORDER BY parent_id NULLS FIRST, sort_order ASC, created_at ASC
	`
	// oldFindByProjectAndTypeSelect, FindByProjectAndType
	oldFindByProjectAndTypeSelect = `
		SELECT id, project_id, parent_id, type, ref, title, body, sort_order, status, attributes, version, valid_from, valid_to, created_at, updated_at
		FROM artifacts
		WHERE project_id = $1 AND type = $2 AND valid_to IS NULL
		ORDER BY parent_id NULLS FIRST, sort_order ASC, created_at ASC
	`
	// oldFindByProjectAndStatusSelect, FindByProjectAndStatus
	oldFindByProjectAndStatusSelect = `
		SELECT id, project_id, parent_id, type, ref, title, body, sort_order, status, attributes, version, valid_from, valid_to, created_at, updated_at
		FROM artifacts
		WHERE project_id = $1 AND status = $2 AND valid_to IS NULL
		ORDER BY updated_at DESC, id ASC
	`
	// oldFindPageByProjectSelect, FindPageByProject
	oldFindPageByProjectSelect = `
		SELECT id, project_id, parent_id, type, ref, title, body, sort_order, status, attributes, version, valid_from, valid_to, created_at, updated_at
		FROM artifacts
		WHERE project_id = $1 AND valid_to IS NULL
		AND ($2 = '' OR type = $2)
		AND ($3 = '' OR attributes->>'owner' = $3)
		ORDER BY parent_id NULLS FIRST, sort_order ASC, created_at ASC, id ASC
		LIMIT $4 OFFSET $5
	`
	// oldUpdateInsert, Update
	oldUpdateInsert = `
		INSERT INTO artifacts (id, project_id, parent_id, type, ref, title, body, sort_order, status, attributes, version, valid_from, valid_to, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`
	// oldFindVersionsByIDSelect, FindVersionsByID
	oldFindVersionsByIDSelect = `
		SELECT id, project_id, parent_id, type, ref, title, body, sort_order, status, attributes, version, valid_from, valid_to, created_at, updated_at
		FROM artifacts
		WHERE id = $1
		ORDER BY version DESC
	`
)

// The statements around them that X13a does not change, also the base's:
// ensureRef's two counter writes and Update's close of the current version.
const (
	oldRefMint = `
			INSERT INTO artifact_ref_counters (project_id, prefix, next_num)
			VALUES ($1, $2, 2)
			ON CONFLICT (project_id, prefix)
			DO UPDATE SET next_num = artifact_ref_counters.next_num + 1
			RETURNING next_num - 1
		`
	oldRefAdvance = `
			INSERT INTO artifact_ref_counters (project_id, prefix, next_num)
			VALUES ($1, $2, $3)
			ON CONFLICT (project_id, prefix)
			DO UPDATE SET next_num = GREATEST(artifact_ref_counters.next_num, $3)
		`
	oldArchive = `UPDATE artifacts SET valid_to = $1 WHERE id = $2 AND valid_to IS NULL`
)

func recordedArtifact() *artifacts.Artifact {
	now := time.Date(2026, 3, 14, 9, 26, 53, 0, time.UTC)
	return &artifacts.Artifact{
		ID: uuid.New().String(), ProjectID: uuid.New().String(), Type: artifacts.TypeRequirement,
		Title: "Recorded", Status: "draft", Version: 1, ValidFrom: now, CreatedAt: now, UpdatedAt: now,
	}
}

// Each read sends its old literal and nothing else.
func TestArtifactReadsSendTheirOldLiterals(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewArtifactRepository(db)
	id := uuid.New().String()

	if got, err := repo.FindByID(id); got != nil || !errors.Is(err, artifacts.ErrNotFound) {
		t.Errorf("FindByID with no row: %v, %v; want artifacts.ErrNotFound", got, err)
	}
	wantEvents(t, "FindByID", rec.take(), "QUERY "+oldFindByIDSelect)

	reads := []struct {
		name  string
		read  func() ([]*artifacts.Artifact, error)
		query string
	}{
		{"FindByProjectID", func() ([]*artifacts.Artifact, error) { return repo.FindByProjectID(id) }, oldFindByProjectIDSelect},
		{"FindByProjectAndType", func() ([]*artifacts.Artifact, error) {
			return repo.FindByProjectAndType(id, artifacts.TypeRequirement)
		}, oldFindByProjectAndTypeSelect},
		{"FindByProjectAndStatus", func() ([]*artifacts.Artifact, error) { return repo.FindByProjectAndStatus(id, "draft") },
			oldFindByProjectAndStatusSelect},
		{"FindPageByProject", func() ([]*artifacts.Artifact, error) { return repo.FindPageByProject(id, "", "", 10, 0) },
			oldFindPageByProjectSelect},
		{"FindVersionsByID", func() ([]*artifacts.Artifact, error) { return repo.FindVersionsByID(id) }, oldFindVersionsByIDSelect},
	}
	for _, r := range reads {
		list, err := r.read()
		if list != nil || err != nil {
			t.Errorf("%s with no rows: %#v, %v; want nil and no error", r.name, list, err)
		}
		wantEvents(t, r.name, rec.take(), "QUERY "+r.query)
	}
}

// Save sends ensureRef's counter write and its old insert in one
// transaction, begun with no options under the statement deadline, and
// commits; any statement that fails rolls it back, a failed begin or commit
// is answered as it came.
func TestArtifactSaveSendsItsOldLiteralInOneTransaction(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewArtifactRepository(db)

	if err := repo.Save(recordedArtifact()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	wantEvents(t, "Save", rec.take(), beginDefaultDeadline, "QUERY "+oldRefMint, "EXEC "+oldSaveInsert, "COMMIT")

	imported := recordedArtifact()
	imported.Ref = "REQ-40"
	if err := repo.Save(imported); err != nil {
		t.Fatalf("Save with a ref: %v", err)
	}
	wantEvents(t, "Save with a ref", rec.take(), beginDefaultDeadline, "EXEC "+oldRefAdvance, "EXEC "+oldSaveInsert, "COMMIT")

	rec.failOn = "INSERT INTO artifacts ("
	if err := repo.Save(recordedArtifact()); err != errRecordedStatement {
		t.Errorf("Save whose insert fails: %v, want the insert's error as it came", err)
	}
	wantEvents(t, "Save whose insert fails", rec.take(), beginDefaultDeadline, "QUERY "+oldRefMint, "EXEC "+oldSaveInsert, "ROLLBACK")

	rec.failOn = "artifact_ref_counters"
	if err := repo.Save(recordedArtifact()); err != errRecordedStatement {
		t.Errorf("Save whose counter write fails: %v, want its error as it came", err)
	}
	wantEvents(t, "Save whose counter write fails", rec.take(), beginDefaultDeadline, "QUERY "+oldRefMint, "ROLLBACK")
	rec.failOn = ""

	rec.failCommit = true
	if err := repo.Save(recordedArtifact()); err != errRecordedCommit {
		t.Errorf("Save whose commit fails: %v, want the commit's error as it came", err)
	}
	wantEvents(t, "Save whose commit fails", rec.take(), beginDefaultDeadline, "QUERY "+oldRefMint, "EXEC "+oldSaveInsert, "COMMIT")
	rec.failCommit = false

	rec.failBegin = true
	if err := repo.Save(recordedArtifact()); err != errRecordedBegin {
		t.Errorf("Save whose begin fails: %v, want the begin's error as it came", err)
	}
	wantEvents(t, "Save whose begin fails", rec.take(), beginDefaultDeadline)
}

// Update sends ensureRef's counter write, the close and its old insert in
// one transaction, begun as Save's is, and commits; any statement that
// fails rolls it back, a failed begin or commit is answered as it came.
func TestArtifactUpdateSendsItsOldLiteralInOneTransaction(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewArtifactRepository(db)
	withRef := func() *artifacts.Artifact {
		a := recordedArtifact()
		a.Ref, a.Version = "REQ-1", 2
		return a
	}

	if err := repo.Update(withRef()); err != nil {
		t.Fatalf("Update: %v", err)
	}
	wantEvents(t, "Update", rec.take(),
		beginDefaultDeadline, "EXEC "+oldRefAdvance, "EXEC "+oldArchive, "EXEC "+oldUpdateInsert, "COMMIT")

	if err := repo.Update(recordedArtifact()); err != nil {
		t.Fatalf("Update with its ref cleared: %v", err)
	}
	wantEvents(t, "Update with its ref cleared", rec.take(),
		beginDefaultDeadline, "QUERY "+oldRefMint, "EXEC "+oldArchive, "EXEC "+oldUpdateInsert, "COMMIT")

	rec.failOn = "INSERT INTO artifacts ("
	if err := repo.Update(withRef()); err != errRecordedStatement {
		t.Errorf("Update whose insert fails: %v, want the insert's error as it came", err)
	}
	wantEvents(t, "Update whose insert fails", rec.take(),
		beginDefaultDeadline, "EXEC "+oldRefAdvance, "EXEC "+oldArchive, "EXEC "+oldUpdateInsert, "ROLLBACK")

	rec.failOn = "UPDATE artifacts SET valid_to"
	if err := repo.Update(withRef()); err != errRecordedStatement {
		t.Errorf("Update whose close fails: %v, want the close's error as it came", err)
	}
	wantEvents(t, "Update whose close fails", rec.take(), beginDefaultDeadline, "EXEC "+oldRefAdvance, "EXEC "+oldArchive, "ROLLBACK")

	rec.failOn = "artifact_ref_counters"
	if err := repo.Update(withRef()); err != errRecordedStatement {
		t.Errorf("Update whose counter write fails: %v, want its error as it came", err)
	}
	wantEvents(t, "Update whose counter write fails", rec.take(), beginDefaultDeadline, "EXEC "+oldRefAdvance, "ROLLBACK")
	rec.failOn = ""

	rec.failCommit = true
	if err := repo.Update(withRef()); err != errRecordedCommit {
		t.Errorf("Update whose commit fails: %v, want the commit's error as it came", err)
	}
	wantEvents(t, "Update whose commit fails", rec.take(),
		beginDefaultDeadline, "EXEC "+oldRefAdvance, "EXEC "+oldArchive, "EXEC "+oldUpdateInsert, "COMMIT")
	rec.failCommit = false

	rec.failBegin = true
	if err := repo.Update(withRef()); err != errRecordedBegin {
		t.Errorf("Update whose begin fails: %v, want the begin's error as it came", err)
	}
	wantEvents(t, "Update whose begin fails", rec.take(), beginDefaultDeadline)
}

// artifactColumns is the column list each old literal spelled out.
func TestArtifactColumnsIsTheOldList(t *testing.T) {
	const old = `id, project_id, parent_id, type, ref, title, body, sort_order, status, attributes, version, valid_from, valid_to, created_at, updated_at`
	if artifactColumns != old {
		t.Errorf("artifactColumns = %q, want %q", artifactColumns, old)
	}
}
