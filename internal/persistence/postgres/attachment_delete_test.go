package postgres

import (
	"database/sql"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/attachments"
)

// seedFigureArtifact puts a project and one artifact in it, and answers the
// artifact's id: a figure is saved onto an artifact whose project exists
// (#379 bug 152).
func seedFigureArtifact(t *testing.T, db *sql.DB) string {
	t.Helper()
	projectID, artifactID := uuid.New().String(), uuid.New().String()
	seedProjects(t, db, projectID)
	if _, err := db.Exec(`INSERT INTO artifacts (id, project_id, type, title) VALUES ($1, $2, 'requirement', 'R')`,
		artifactID, projectID); err != nil {
		t.Fatalf("seed artifact: %v", err)
	}
	return artifactID
}

// countRows answers how many rows of table have column = value.
func countRows(t *testing.T, db *sql.DB, table, column, value string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE `+column+` = $1`, value).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// Deleting a figure deletes every version of it and answers the stored file
// of each, once (#379 bug 145: only the current version's file was removed,
// so every earlier version's stayed on disk). A restored version shares the
// file of the version it restored, and a rename the file of the version
// before it, so four versions answer two files. The figure counter stays:
// a figure's number is never issued again.
func TestDeletingAFigureAnswersEveryVersionsFile(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewAttachmentRepository(db)

	fig := attachments.NewAttachment(attachments.CreateAttachmentRequest{
		ArtifactID: seedFigureArtifact(t, db), Filename: "seal.png", OriginalFilename: "seal.png",
		MimeType: "image/png", FilePath: "/uploads/v1_seal.png", FileSize: 1,
	})
	if err := repo.SaveWithFigureRef(fig, "REQ-9"); err != nil {
		t.Fatalf("SaveWithFigureRef: %v", err)
	}
	if _, err := repo.AddVersion(fig.ID, &attachments.Version{OriginalFilename: "seal-b.png", MimeType: "image/png",
		FilePath: "/uploads/v2_seal-b.png", FileSize: 2}); err != nil {
		t.Fatalf("AddVersion: %v", err)
	}
	if _, err := repo.Restore(fig.ID, 1, nil); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if _, err := repo.Rename(fig.ID, "Seal", nil); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	other := attachments.NewAttachment(attachments.CreateAttachmentRequest{
		ArtifactID: fig.ArtifactID, Filename: "pump.png", OriginalFilename: "pump.png",
		MimeType: "image/png", FilePath: "/uploads/other_pump.png", FileSize: 1,
	})
	if err := repo.SaveWithFigureRef(other, "REQ-9"); err != nil {
		t.Fatalf("SaveWithFigureRef (another figure): %v", err)
	}

	files, err := repo.Delete(fig.ID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if want := []string{"/uploads/v1_seal.png", "/uploads/v2_seal-b.png"}; !reflect.DeepEqual(files, want) {
		t.Errorf("Delete answered the files %q, want every version's, each once: %q", files, want)
	}
	if n := countRows(t, db, "attachments", "id", fig.ID); n != 0 {
		t.Errorf("%d attachment rows after the delete, want 0", n)
	}
	if n := countRows(t, db, "attachment_versions", "attachment_id", fig.ID); n != 0 {
		t.Errorf("%d version rows after the delete, want 0", n)
	}
	if n := countRows(t, db, "attachment_versions", "attachment_id", other.ID); n != 1 {
		t.Errorf("the other figure has %d version rows after the delete, want 1", n)
	}
	if n := countRows(t, db, "attachment_figure_counters", "artifact_id", fig.ArtifactID); n != 1 {
		t.Errorf("%d figure counter rows after the delete, want the counter kept", n)
	}

	for _, id := range []string{fig.ID, uuid.New().String(), "not-a-uuid"} {
		if files, err := repo.Delete(id); err != nil || files != nil {
			t.Errorf("Delete(%q) of a figure no row has = %q, %v; want no file and no error", id, files, err)
		}
	}
}

// A version added while the figure is being deleted is answered with the
// rest: the delete of the figure's row waits for the new version to commit,
// and then answers the file the version moved the figure to.
func TestDeletingAFigureAnswersAVersionAddedMeanwhile(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewAttachmentRepository(db)
	fig := attachments.NewAttachment(attachments.CreateAttachmentRequest{
		ArtifactID: seedFigureArtifact(t, db), Filename: "seal.png", OriginalFilename: "seal.png",
		MimeType: "image/png", FilePath: "/uploads/v1_seal.png", FileSize: 1,
	})
	if err := repo.SaveWithFigureRef(fig, "REQ-9"); err != nil {
		t.Fatalf("SaveWithFigureRef: %v", err)
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE attachments SET version = 2, file_path = '/uploads/v2_seal.png' WHERE id = $1`, fig.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO attachment_versions (id, attachment_id, version, filename, mime_type, file_path, file_size)
		VALUES ($1, $2, 2, 'seal.png', 'image/png', '/uploads/v2_seal.png', 1)`, uuid.New().String(), fig.ID); err != nil {
		t.Fatal(err)
	}
	type answer struct {
		files []string
		err   error
	}
	done := make(chan answer, 1)
	go func() {
		files, err := repo.Delete(fig.ID)
		done <- answer{files, err}
	}()
	waitForALockWait(t, db, 1, func() {
		select {
		case a := <-done:
			t.Fatalf("the delete did not wait for the new version: %q, %v", a.files, a.err)
		default:
		}
	})
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	a := <-done
	if a.err != nil {
		t.Fatalf("Delete: %v", a.err)
	}
	if want := []string{"/uploads/v1_seal.png", "/uploads/v2_seal.png"}; !reflect.DeepEqual(a.files, want) {
		t.Errorf("Delete answered the files %q, want %q", a.files, want)
	}
}

// waitForALockWait waits until at least n sessions of this database wait on
// a lock, calling check between polls (to fail early when what should be
// waiting has finished instead).
func waitForALockWait(t *testing.T, db *sql.DB, n int, check func()) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= n {
			return
		}
		check()
		if time.Now().After(deadline) {
			t.Fatalf("fewer than %d sessions ever waited on a lock", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
