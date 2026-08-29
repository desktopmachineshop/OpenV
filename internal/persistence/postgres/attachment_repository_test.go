package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/attachments"
)

// TestFindByArtifactIDsBatched exercises the batched attachment fetch that
// replaced the per-artifact N+1 in project export. It must (1) group results
// by artifact, (2) return newest-first within each artifact, (3) omit
// artifacts that have no attachments, and (4) equal what per-artifact lookups
// would have returned — all from a single query. Postgres-gated
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
	// observable; a2: one.
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

	// a1: newest first.
	if len(got[a1]) != 2 {
		t.Fatalf("a1 has %d attachments, want 2", len(got[a1]))
	}
	if got[a1][0].Filename != "a1-new.png" || got[a1][1].Filename != "a1-old.png" {
		t.Errorf("a1 ordering = [%s, %s], want newest-first [a1-new.png, a1-old.png]",
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
