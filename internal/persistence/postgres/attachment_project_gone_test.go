package postgres

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/attachments"
)

// figureRaceSeed is a project with one artifact, and a figure for it not
// yet saved.
func figureRaceSeed(t *testing.T, db *sql.DB) (projectID string, fig *attachments.Attachment) {
	t.Helper()
	w := pdSeedWorkspace(t, db)
	projectID, artifactID := uuid.New().String(), uuid.New().String()
	rtSeed(t, db, `INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'Doomed')`, projectID, w.org)
	rtSeed(t, db, `INSERT INTO artifacts (id, project_id, type, title, ref) VALUES ($1, $2, 'requirement', 'R', 'REQ-1')`, artifactID, projectID)
	fig = attachments.NewAttachment(attachments.CreateAttachmentRequest{
		ArtifactID: artifactID, Filename: "late.png", OriginalFilename: "late.png",
		MimeType: "image/png", FilePath: "/uploads/" + uuid.New().String() + "_late.png", FileSize: 1,
	})
	return projectID, fig
}

// assertNoFigureRows fails when a row of the figure, its first version or
// its artifact's figure counter is left.
func assertNoFigureRows(t *testing.T, db *sql.DB, fig *attachments.Attachment) {
	t.Helper()
	for _, c := range [][2]string{
		{"attachments", "id"}, {"attachment_versions", "attachment_id"}, {"attachment_figure_counters", "artifact_id"},
	} {
		value := fig.ID
		if c[1] == "artifact_id" {
			value = fig.ArtifactID
		}
		if n := countRows(t, db, c[0], c[1], value); n != 0 {
			t.Errorf("%d %s rows of the figure are left, with no project", n, c[0])
		}
	}
}

// A figure saved onto an artifact of a project deleted meanwhile, its file
// streamed in while the delete ran, is refused and stores nothing (#379 bug
// 152: nothing tied a figure to its project, so the insert went in and left
// an orphan row, figure counter and file).
func TestAFigureOfADeletedProjectIsRefused(t *testing.T) {
	db := rtDB(t)
	projectID, fig := figureRaceSeed(t, db)
	if _, err := NewProjectRepository(db).Delete(projectID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := NewAttachmentRepository(db).SaveWithFigureRef(fig, "REQ-1"); !errors.Is(err, attachments.ErrNoArtifact) {
		t.Errorf("SaveWithFigureRef onto a deleted project's artifact = %v, want attachments.ErrNoArtifact", err)
	}
	assertNoFigureRows(t, db, fig)
	for _, id := range []string{uuid.New().String(), "not-a-uuid"} {
		fig.ArtifactID = id
		if err := NewAttachmentRepository(db).SaveWithFigureRef(fig, "REQ-1"); !errors.Is(err, attachments.ErrNoArtifact) {
			t.Errorf("SaveWithFigureRef onto artifact %q no row has = %v, want attachments.ErrNoArtifact", id, err)
		}
	}
}

// A figure saved while its project's delete holds the project waits for the
// delete, and is then refused, storing nothing (#379 bug 152).
func TestAFigureSavedWhileItsProjectIsDeletedWaitsAndIsRefused(t *testing.T) {
	db := rtDB(t)
	projectID, fig := figureRaceSeed(t, db)

	// The delete, as ProjectRepository.Delete begins it: the project's row
	// held for update.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT id FROM projects WHERE id = $1 FOR UPDATE`, projectID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- NewAttachmentRepository(db).SaveWithFigureRef(fig, "REQ-1") }()
	waitForALockWait(t, db, 1, func() {
		select {
		case err := <-done:
			t.Fatalf("the figure did not wait for the project's delete: %v", err)
		default:
		}
	})
	if _, err := tx.Exec(`DELETE FROM projects WHERE id = $1`, projectID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, attachments.ErrNoArtifact) {
		t.Errorf("SaveWithFigureRef behind the project's delete = %v, want attachments.ErrNoArtifact", err)
	}
	assertNoFigureRows(t, db, fig)
}

// A figure saved just before its project's delete reaches the project's
// figures holds the project, so the delete waits for it, and then deletes it
// with the rest and answers its file (#379 bug 152: the delete read the
// figures before the new one committed, and it was left behind).
func TestAFigureSavedAsItsProjectIsDeletedGoesWithIt(t *testing.T) {
	db := rtDB(t)
	projectID, fig := figureRaceSeed(t, db)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := saveFigure(tx, fig, "REQ-1"); err != nil {
		t.Fatalf("saveFigure: %v", err)
	}
	removed := pdDeleteBehind(t, db, tx, projectID)
	found := false
	for _, f := range removed.Files {
		found = found || f == fig.FilePath
	}
	if !found {
		t.Errorf("Delete answered the files %q, without the figure saved as it began, %s", removed.Files, fig.FilePath)
	}
	assertNoFigureRows(t, db, fig)
}
