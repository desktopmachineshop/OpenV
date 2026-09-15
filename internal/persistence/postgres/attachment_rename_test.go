package postgres

import (
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/attachments"
)

// Renaming a figure is a version like a new image is (REQ-157): the title
// advances the version over the same file, the history records the title
// each version carried, and a later image keeps the title.
func TestRenameFigureIsATrackedVersion(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewAttachmentRepository(db)

	att := attachments.NewAttachment(attachments.CreateAttachmentRequest{
		ArtifactID:       uuid.New().String(),
		Filename:         "Screenshot 1234.png",
		OriginalFilename: "Screenshot 1234.png",
		MimeType:         "image/png",
		FilePath:         "/tmp/one.png",
		FileSize:         10,
	})
	if err := repo.SaveWithFigureRef(att, "REQ-9"); err != nil {
		t.Fatalf("save: %v", err)
	}

	by := uuid.New().String()
	next, err := repo.Rename(att.ID, "Pump curve", &by)
	if err != nil || next != 2 {
		t.Fatalf("rename = %d, %v; want version 2", next, err)
	}
	got, err := repo.FindByID(att.ID)
	if err != nil || got == nil {
		t.Fatalf("find: %v", err)
	}
	if got.Title != "Pump curve" || got.Version != 2 || got.FilePath != "/tmp/one.png" || got.Name() != "Pump curve" {
		t.Errorf("after rename: title=%q v%d path=%s name=%q", got.Title, got.Version, got.FilePath, got.Name())
	}

	// A new image over the renamed figure keeps the title.
	next, err = repo.AddVersion(att.ID, &attachments.Version{
		Filename: "two.png", OriginalFilename: "two.png", MimeType: "image/png", FilePath: "/tmp/two.png", FileSize: 20,
	})
	if err != nil || next != 3 {
		t.Fatalf("add version = %d, %v; want version 3", next, err)
	}
	got, _ = repo.FindByID(att.ID)
	if got.Title != "Pump curve" || got.FilePath != "/tmp/two.png" {
		t.Errorf("after new image: title=%q path=%s, want the title kept", got.Title, got.FilePath)
	}

	versions, err := repo.ListVersions(att.ID)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("versions = %d, want 3", len(versions))
	}
	// Newest first: v3 new image with the title, v2 the rename over the
	// first file, v1 the upload with no title.
	if versions[0].Version != 3 || versions[0].Title != "Pump curve" || versions[0].FilePath != "/tmp/two.png" {
		t.Errorf("v3 = %+v", versions[0])
	}
	if versions[1].Version != 2 || versions[1].Title != "Pump curve" || versions[1].FilePath != "/tmp/one.png" || versions[1].CreatedBy == nil || *versions[1].CreatedBy != by {
		t.Errorf("v2 = %+v, want the rename over the first file by its author", versions[1])
	}
	if versions[2].Version != 1 || versions[2].Title != "" || versions[2].FilePath != "/tmp/one.png" {
		t.Errorf("v1 = %+v", versions[2])
	}

	// Renaming a figure that does not exist writes nothing.
	if n, err := repo.Rename(uuid.New().String(), "x", nil); err != nil || n != 0 {
		t.Errorf("rename of a missing figure = %d, %v; want 0, nil", n, err)
	}
}
