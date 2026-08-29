package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
)

// newTestArtifact builds a minimal current-version artifact for ref tests.
func newTestArtifact(projectID, typ, title string) *artifacts.Artifact {
	now := time.Now()
	return &artifacts.Artifact{
		ID:        uuid.New().String(),
		ProjectID: projectID,
		Type:      typ,
		Title:     title,
		Status:    "draft",
		Version:   1,
		ValidFrom: now,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// TestArtifactRefAssignment: Save mints sequential per-project, per-prefix
// refs; a new version keeps its ref; a caller-supplied ref (import path) is
// preserved and advances the counter past itself.
func TestArtifactRefAssignment(t *testing.T) {
	db := testDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	repo := NewArtifactRepository(db)
	projectID := uuid.New().String()

	r1 := newTestArtifact(projectID, artifacts.TypeRequirement, "First")
	r2 := newTestArtifact(projectID, artifacts.TypeRequirement, "Second")
	tc := newTestArtifact(projectID, artifacts.TypeTestCase, "Verify first")
	for _, a := range []*artifacts.Artifact{r1, r2, tc} {
		if err := repo.Save(a); err != nil {
			t.Fatalf("Save(%s): %v", a.Title, err)
		}
	}
	if r1.Ref != "REQ-1" || r2.Ref != "REQ-2" || tc.Ref != "TC-1" {
		t.Fatalf("assigned refs = %q, %q, %q; want REQ-1, REQ-2, TC-1", r1.Ref, r2.Ref, tc.Ref)
	}

	// Another project starts its own numbering.
	other := newTestArtifact(uuid.New().String(), artifacts.TypeRequirement, "Elsewhere")
	if err := repo.Save(other); err != nil {
		t.Fatalf("Save(other project): %v", err)
	}
	if other.Ref != "REQ-1" {
		t.Fatalf("other project ref = %q, want REQ-1", other.Ref)
	}

	// A new version keeps the ref, and reads round-trip it.
	loaded, err := repo.FindByID(r1.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if loaded.Ref != "REQ-1" {
		t.Fatalf("loaded ref = %q, want REQ-1", loaded.Ref)
	}
	loaded.Title = "First (edited)"
	loaded.Version = 2
	loaded.ValidFrom = time.Now()
	loaded.UpdatedAt = time.Now()
	if err := repo.Update(loaded); err != nil {
		t.Fatalf("Update: %v", err)
	}
	reloaded, err := repo.FindByID(r1.ID)
	if err != nil {
		t.Fatalf("FindByID after update: %v", err)
	}
	if reloaded.Ref != "REQ-1" || reloaded.Version != 2 {
		t.Fatalf("after update ref=%q version=%d, want REQ-1 v2", reloaded.Ref, reloaded.Version)
	}

	// Import path: a preset ref survives and pushes the counter past it.
	imported := newTestArtifact(projectID, artifacts.TypeRequirement, "Imported")
	imported.Ref = "REQ-40"
	if err := repo.Save(imported); err != nil {
		t.Fatalf("Save(imported): %v", err)
	}
	if imported.Ref != "REQ-40" {
		t.Fatalf("imported ref = %q, want REQ-40", imported.Ref)
	}
	next := newTestArtifact(projectID, artifacts.TypeRequirement, "After import")
	if err := repo.Save(next); err != nil {
		t.Fatalf("Save(after import): %v", err)
	}
	if next.Ref != "REQ-41" {
		t.Fatalf("ref after import = %q, want REQ-41", next.Ref)
	}
}

// TestArtifactRefBackfill: migration 0018 stamps refs onto a pre-existing
// database's current artifacts in stable tree order and seeds the counters
// so post-migration creates continue the numbering.
func TestArtifactRefBackfill(t *testing.T) {
	db := testDB(t)

	// Simulate a database from before the ref column existed.
	if err := InitSchema(db); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}
	projectID := uuid.New().String()
	// The frozen baseline schema predates the status (0003) and ref (0018)
	// columns, so the seed uses only baseline columns.
	insert := func(typ, title string, sortOrder int) string {
		id := uuid.New().String()
		if _, err := db.Exec(`
			INSERT INTO artifacts (id, project_id, type, title, body, sort_order, version)
			VALUES ($1, $2, $3, $4, '', $5, 1)
		`, id, projectID, typ, title, sortOrder); err != nil {
			t.Fatalf("seed %s: %v", title, err)
		}
		return id
	}
	reqA := insert("requirement", "A", 1)
	reqB := insert("requirement", "B", 2)
	tc := insert("test-case", "T", 3)

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	refOf := func(id string) string {
		var ref string
		if err := db.QueryRow(`SELECT ref FROM artifacts WHERE id = $1 AND valid_to IS NULL`, id).Scan(&ref); err != nil {
			t.Fatalf("ref of %s: %v", id, err)
		}
		return ref
	}
	if got := refOf(reqA); got != "REQ-1" {
		t.Fatalf("backfilled reqA = %q, want REQ-1", got)
	}
	if got := refOf(reqB); got != "REQ-2" {
		t.Fatalf("backfilled reqB = %q, want REQ-2", got)
	}
	if got := refOf(tc); got != "TC-1" {
		t.Fatalf("backfilled tc = %q, want TC-1", got)
	}

	// The counter continues where the backfill stopped.
	repo := NewArtifactRepository(db)
	fresh := newTestArtifact(projectID, artifacts.TypeRequirement, "C")
	if err := repo.Save(fresh); err != nil {
		t.Fatalf("Save after backfill: %v", err)
	}
	if fresh.Ref != "REQ-3" {
		t.Fatalf("post-backfill ref = %q, want REQ-3", fresh.Ref)
	}
}
