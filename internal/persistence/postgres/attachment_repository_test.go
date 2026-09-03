package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/attachments"
)

// TestFindByArtifactIDsBatched exercises the batched attachment fetch that
// replaced the per-artifact N+1 in project export. It must (1) group results
// by artifact, (2) order within each artifact by figure — numbered figures in
// figure order, unnumbered ones oldest-first behind them — (3) omit artifacts
// that have no attachments, and (4) equal what per-artifact lookups would have
// returned, all from a single query. Postgres-gated
// (OPENV_TEST_DATABASE_URL).
func TestFindByArtifactIDsBatched(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)

	repo := NewAttachmentRepository(db)

	a1 := uuid.New().String()
	a2 := uuid.New().String()
	a3 := uuid.New().String() // will have no attachments
	base := time.Now().Truncate(time.Millisecond)

	// a1: two attachments, inserted oldest-then-newest so ordering is
	// observable; a2: one. Saved through Save, so none carries a figure
	// number and all of them sort by age.
	mk := func(artifactID, filename string, created time.Time) *attachments.Attachment {
		att := attachments.NewAttachment(attachments.CreateAttachmentRequest{
			ArtifactID: artifactID,
			Filename:   filename,
			MimeType:   "image/png",
			FilePath:   "/tmp/" + filename,
			FileSize:   10,
		})
		att.CreatedAt = created
		return att
	}
	seed := []*attachments.Attachment{
		mk(a1, "a1-old.png", base.Add(-2*time.Hour)),
		mk(a1, "a1-new.png", base.Add(-1*time.Hour)),
		mk(a2, "a2.png", base),
	}
	for _, att := range seed {
		if err := repo.Save(att); err != nil {
			t.Fatalf("save %s: %v", att.Filename, err)
		}
	}

	got, err := repo.FindByArtifactIDs([]string{a1, a2, a3})
	if err != nil {
		t.Fatalf("FindByArtifactIDs: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("grouped map has %d artifacts, want 2 (a3 has none): %v", len(got), got)
	}
	if _, ok := got[a3]; ok {
		t.Error("artifact with no attachments must be absent from the map")
	}

	// a1: unnumbered images fall back to oldest-first, which is the order
	// they would have been given figure numbers in.
	if len(got[a1]) != 2 {
		t.Fatalf("a1 has %d attachments, want 2", len(got[a1]))
	}
	if got[a1][0].Filename != "a1-old.png" || got[a1][1].Filename != "a1-new.png" {
		t.Errorf("a1 ordering = [%s, %s], want oldest-first [a1-old.png, a1-new.png]",
			got[a1][0].Filename, got[a1][1].Filename)
	}
	if len(got[a2]) != 1 || got[a2][0].Filename != "a2.png" {
		t.Errorf("a2 attachments = %v, want single a2.png", got[a2])
	}

	// Equivalence with the per-artifact path it replaced.
	perArtifact, err := repo.FindByArtifactID(a1)
	if err != nil {
		t.Fatalf("FindByArtifactID(a1): %v", err)
	}
	if len(perArtifact) != len(got[a1]) {
		t.Fatalf("batched a1 count %d != per-artifact count %d", len(got[a1]), len(perArtifact))
	}
	for i := range perArtifact {
		if perArtifact[i].ID != got[a1][i].ID {
			t.Errorf("batched vs per-artifact mismatch at %d: %s != %s", i, got[a1][i].ID, perArtifact[i].ID)
		}
	}

	// Empty input is a cheap no-op, never a query error.
	empty, err := repo.FindByArtifactIDs(nil)
	if err != nil {
		t.Fatalf("FindByArtifactIDs(nil): %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("FindByArtifactIDs(nil) = %v, want empty", empty)
	}
}

// A figure number is a citation, so the promise is that it is allocated once
// and never issued twice — not even after the figure holding it is deleted.
// The counter is what makes that true, and it only means anything against a
// real database. Postgres-gated (OPENV_TEST_DATABASE_URL).
func TestFigureNumbersAreNeverReissued(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)

	repo := NewAttachmentRepository(db)
	artifactID := uuid.New().String()

	figure := func(name string) *attachments.Attachment {
		return attachments.NewAttachment(attachments.CreateAttachmentRequest{
			ArtifactID:       artifactID,
			Filename:         name,
			OriginalFilename: name,
			MimeType:         "image/png",
			FilePath:         "/tmp/" + name,
			FileSize:         10,
		})
	}

	first, second := figure("one.png"), figure("two.png")
	for _, f := range []*attachments.Attachment{first, second} {
		if err := repo.SaveWithFigureRef(f, "REQ-17"); err != nil {
			t.Fatalf("SaveWithFigureRef: %v", err)
		}
	}
	if first.FigureRef != "REQ-17-FIG-1" || second.FigureRef != "REQ-17-FIG-2" {
		t.Fatalf("figures = %q, %q; want REQ-17-FIG-1, REQ-17-FIG-2", first.FigureRef, second.FigureRef)
	}
	// The stored name follows the figure, not the upload.
	if first.Filename != "REQ-17-FIG-1.png" {
		t.Errorf("stored filename = %q, want REQ-17-FIG-1.png", first.Filename)
	}

	// Deleting Figure 2 must not put its number back in circulation.
	if err := repo.Delete(second.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	third := figure("three.png")
	if err := repo.SaveWithFigureRef(third, "REQ-17"); err != nil {
		t.Fatalf("SaveWithFigureRef after delete: %v", err)
	}
	if third.FigureRef != "REQ-17-FIG-3" {
		t.Errorf("figure after deleting FIG-2 = %q, want REQ-17-FIG-3 — the number was reissued",
			third.FigureRef)
	}

	// A different artifact numbers from its own one.
	other := attachments.NewAttachment(attachments.CreateAttachmentRequest{
		ArtifactID:       uuid.New().String(),
		Filename:         "x.png",
		OriginalFilename: "x.png",
		MimeType:         "image/png",
		FilePath:         "/tmp/x.png",
		FileSize:         10,
	})
	if err := repo.SaveWithFigureRef(other, "HAZ-3"); err != nil {
		t.Fatalf("SaveWithFigureRef (other artifact): %v", err)
	}
	if other.FigureRef != "HAZ-3-FIG-1" {
		t.Errorf("other artifact's first figure = %q, want HAZ-3-FIG-1", other.FigureRef)
	}
}

// A new version supersedes the figure's file while the figure keeps its
// reference, and the superseded version stays readable — that is the whole
// point of versioning a drawing rather than deleting and re-uploading it.
// Postgres-gated (OPENV_TEST_DATABASE_URL).
func TestFigureVersionsSupersedeAndKeepHistory(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)

	repo := NewAttachmentRepository(db)
	fig := attachments.NewAttachment(attachments.CreateAttachmentRequest{
		ArtifactID:       uuid.New().String(),
		Filename:         "first.png",
		OriginalFilename: "first.png",
		MimeType:         "image/png",
		FilePath:         "/tmp/first.png",
		FileSize:         10,
	})
	if err := repo.SaveWithFigureRef(fig, "REQ-17"); err != nil {
		t.Fatalf("SaveWithFigureRef: %v", err)
	}

	next, err := repo.AddVersion(fig.ID, &attachments.Version{
		OriginalFilename: "second.jpg",
		MimeType:         "image/jpeg",
		FilePath:         "/tmp/second.jpg",
		FileSize:         20,
	})
	if err != nil {
		t.Fatalf("AddVersion: %v", err)
	}
	if next != 2 {
		t.Fatalf("AddVersion returned %d, want 2", next)
	}

	current, err := repo.FindByID(fig.ID)
	if err != nil || current == nil {
		t.Fatalf("FindByID: %v", err)
	}
	if current.Version != 2 {
		t.Errorf("current version = %d, want 2", current.Version)
	}
	if current.FigureRef != "REQ-17-FIG-1" {
		t.Errorf("figure reference = %q, want it unchanged at REQ-17-FIG-1", current.FigureRef)
	}
	// The name follows the figure and the new extension.
	if current.Filename != "REQ-17-FIG-1.jpg" {
		t.Errorf("filename = %q, want REQ-17-FIG-1.jpg", current.Filename)
	}
	if current.FilePath != "/tmp/second.jpg" {
		t.Errorf("file path = %q, want the new version's file", current.FilePath)
	}

	history, err := repo.ListVersions(fig.ID)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(history) != 2 || history[0].Version != 2 || history[1].Version != 1 {
		t.Fatalf("history = %v, want versions 2 then 1", history)
	}
	// The superseded file is still addressable, which is what makes an older
	// drawing retrievable after it is replaced.
	if history[1].FilePath != "/tmp/first.png" {
		t.Errorf("version 1 file = %q, want the original still recorded", history[1].FilePath)
	}
	v1, err := repo.FindVersion(fig.ID, 1)
	if err != nil || v1 == nil {
		t.Fatalf("FindVersion(1) = %v, %v; want the superseded version", v1, err)
	}
	missing, err := repo.FindVersion(fig.ID, 99)
	if err != nil || missing != nil {
		t.Errorf("FindVersion(99) = %v, %v; want nil, nil", missing, err)
	}

	// A version upload against an attachment that is not there reports so
	// rather than inventing one.
	n, err := repo.AddVersion(uuid.New().String(), &attachments.Version{
		OriginalFilename: "ghost.png", MimeType: "image/png", FilePath: "/tmp/g.png", FileSize: 1,
	})
	if err != nil || n != 0 {
		t.Errorf("AddVersion on a missing attachment = %d, %v; want 0, nil", n, err)
	}
}
