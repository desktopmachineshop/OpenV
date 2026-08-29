package postgres

import (
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/embeddings"
)

// TestMigrateArtifactEmbeddings verifies migration 0016 is recorded regardless
// of whether the vector extension is available, and that the
// artifact_embeddings table exists exactly when the extension does.
//
//   - On a plain postgres:16 (no vector extension, and the role cannot create
//     it) the migration must SKIP the table but still record itself in the
//     ledger, so boot never bricks. This is the DEFAULT path exercised by CI.
//   - On pgvector/pgvector:pg16 the extension is present, so the table and its
//     HNSW index are created.
func TestMigrateArtifactEmbeddings(t *testing.T) {
	db := testDB(t)

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// The 0016 ledger row must exist either way — boot must not brick on a
	// managed Postgres that forbids CREATE EXTENSION.
	if _, ok := ledgerRows(t, db)[16]; !ok {
		t.Fatal("migration 0016 was not recorded in the ledger")
	}

	vectorAvailable := extensionExists(t, db, "vector")
	tablePresent := tableExists(t, db, "artifact_embeddings")
	indexPresent := indexExists(t, db, "idx_artifact_embeddings_hnsw")

	if vectorAvailable {
		if !tablePresent {
			t.Error("vector extension present but artifact_embeddings table missing")
		}
		if !indexPresent {
			t.Error("vector extension present but HNSW index missing")
		}
	} else {
		if tablePresent {
			t.Error("artifact_embeddings table exists without the vector extension")
		}
		t.Logf("vector extension unavailable: graceful-skip path exercised (use pgvector/pgvector:pg16 to exercise the table path)")
	}

	// Second boot is clean and does not add ledger rows.
	if err := Migrate(db); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if tableExists(t, db, "artifact_embeddings") != tablePresent {
		t.Error("artifact_embeddings table presence changed across a second boot")
	}
}

// TestEmbeddingRepositoryUpsert exercises the store against a real database.
// It requires the vector extension (pgvector/pgvector:pg16) and skips cleanly
// on a plain postgres:16 where migration 0016 left no table.
func TestEmbeddingRepositoryUpsert(t *testing.T) {
	db := testDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if !tableExists(t, db, "artifact_embeddings") {
		t.Skip("artifact_embeddings table absent (no vector extension); skipping store test")
	}

	repo := NewEmbeddingRepository(db)
	id := uuid.New().String()

	// Absent → (nil, nil).
	if got, err := repo.GetByArtifact(id); err != nil || got != nil {
		t.Fatalf("GetByArtifact(absent) = (%v, %v), want (nil, nil)", got, err)
	}

	vec := make([]float32, embeddings.Dimensions)
	for i := range vec {
		vec[i] = float32(i%7) * 0.01
	}
	e := &embeddings.Embedding{
		ArtifactID:      id,
		ArtifactVersion: 2,
		Vector:          vec,
		Model:           "text-embedding-3-small",
		ContentHash:     embeddings.ContentHash("Title", "Body"),
	}
	if err := repo.Upsert(e); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := repo.GetByArtifact(id)
	if err != nil {
		t.Fatalf("GetByArtifact: %v", err)
	}
	if got == nil {
		t.Fatal("expected a stored embedding")
	}
	if got.ArtifactVersion != 2 || got.Model != "text-embedding-3-small" || got.ContentHash != e.ContentHash {
		t.Errorf("stored metadata mismatch: %+v", got)
	}

	// Upsert again with new content: replaces in place (still one row).
	e.ArtifactVersion = 3
	e.ContentHash = embeddings.ContentHash("Title", "Body v2")
	if err := repo.Upsert(e); err != nil {
		t.Fatalf("second Upsert: %v", err)
	}
	got, err = repo.GetByArtifact(id)
	if err != nil {
		t.Fatalf("GetByArtifact after re-upsert: %v", err)
	}
	if got.ArtifactVersion != 3 || got.ContentHash != e.ContentHash {
		t.Errorf("re-upsert did not update row: %+v", got)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM artifact_embeddings WHERE artifact_id = $1`, id).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected exactly one row after re-upsert, got %d", count)
	}
}
