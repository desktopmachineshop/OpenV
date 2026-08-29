package postgres

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/embeddings"
)

// vecFromWeights builds a Dimensions-wide vector with the given non-zero
// components, the rest zero. Cosine distance depends only on direction, so a
// few set axes suffice to place vectors at known relative angles.
func vecFromWeights(weights map[int]float32) []float32 {
	v := make([]float32, embeddings.Dimensions)
	for i, w := range weights {
		v[i] = w
	}
	return v
}

// saveEmbeddedArtifact saves a current artifact and upserts its embedding,
// returning the artifact id.
func saveEmbeddedArtifact(t *testing.T, ar *ArtifactRepository, er *EmbeddingRepository, projectID, typ, title string, vec []float32) string {
	t.Helper()
	a := artifacts.NewArtifact(artifacts.CreateArtifactRequest{
		ProjectID: projectID,
		Type:      typ,
		Title:     title,
		Body:      title + " body",
	})
	if err := ar.Save(a); err != nil {
		t.Fatalf("save %q: %v", title, err)
	}
	if err := er.Upsert(&embeddings.Embedding{
		ArtifactID:      a.ID,
		ArtifactVersion: a.Version,
		Vector:          vec,
		Model:           "text-embedding-3-small",
		ContentHash:     embeddings.ContentHash(a.Title, a.Body),
	}); err != nil {
		t.Fatalf("upsert embedding %q: %v", title, err)
	}
	return a.ID
}

// TestNearestByEmbedding exercises the cosine-distance kNN read path against a
// real pgvector database: nearest-first ordering and project scoping. It
// requires the vector extension (pgvector/pgvector:pg16) and skips on a plain
// postgres:16 where migration 0016 left no table.
func TestNearestByEmbedding(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	if !tableExists(t, db, "artifact_embeddings") {
		t.Skip("artifact_embeddings table absent (no vector extension); use pgvector/pgvector:pg16")
	}

	ar := NewArtifactRepository(db)
	er := NewEmbeddingRepository(db)

	projA := uuid.New().String()
	projB := uuid.New().String()

	// query points along axis 0. near is almost parallel; far is orthogonal.
	near := saveEmbeddedArtifact(t, ar, er, projA, "requirement", "Near match", vecFromWeights(map[int]float32{0: 1, 1: 0.1}))
	far := saveEmbeddedArtifact(t, ar, er, projA, "requirement", "Far match", vecFromWeights(map[int]float32{5: 1}))
	// Same direction as the query but in a project outside the scope.
	saveEmbeddedArtifact(t, ar, er, projB, "requirement", "Out of scope", vecFromWeights(map[int]float32{0: 1}))

	query := vecFromWeights(map[int]float32{0: 1})

	t.Run("nearest first, scoped to the given projects", func(t *testing.T) {
		hits, err := er.NearestByEmbedding([]string{projA}, query, 20)
		if err != nil {
			t.Fatalf("NearestByEmbedding: %v", err)
		}
		if len(hits) != 2 {
			t.Fatalf("got %d hits, want 2 (projB excluded): %+v", len(hits), hits)
		}
		if hits[0].ArtifactID != near {
			t.Errorf("first hit = %q, want the near match %q", hits[0].ArtifactID, near)
		}
		if hits[1].ArtifactID != far {
			t.Errorf("second hit = %q, want the far match %q", hits[1].ArtifactID, far)
		}
		if !(hits[0].Distance < hits[1].Distance) {
			t.Errorf("distances not ascending: %v then %v", hits[0].Distance, hits[1].Distance)
		}
		if hits[0].Similarity() <= hits[1].Similarity() {
			t.Errorf("similarity not descending: %v then %v", hits[0].Similarity(), hits[1].Similarity())
		}
		if hits[0].Body == "" || hits[0].Type != "requirement" {
			t.Errorf("hit missing body/type for snippet: %+v", hits[0])
		}
	})

	t.Run("limit caps results", func(t *testing.T) {
		hits, err := er.NearestByEmbedding([]string{projA}, query, 1)
		if err != nil {
			t.Fatalf("NearestByEmbedding: %v", err)
		}
		if len(hits) != 1 || hits[0].ArtifactID != near {
			t.Fatalf("got %+v, want only the nearest match", hits)
		}
	})

	t.Run("empty scope returns nothing", func(t *testing.T) {
		hits, err := er.NearestByEmbedding(nil, query, 20)
		if err != nil {
			t.Fatalf("NearestByEmbedding: %v", err)
		}
		if len(hits) != 0 {
			t.Fatalf("got %+v, want none for empty scope", hits)
		}
	})
}

// TestNearestByEmbeddingVectorUnavailable locks in the graceful-degradation
// contract: on a database where the vector extension was unavailable (plain
// postgres:16, no artifact_embeddings table), the read path returns
// embeddings.ErrVectorUnavailable so the endpoint falls back to keyword search
// rather than 500-ing. It skips when the table IS present (pgvector image).
func TestNearestByEmbeddingVectorUnavailable(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	if tableExists(t, db, "artifact_embeddings") {
		t.Skip("artifact_embeddings table present; this path only exists without pgvector (use postgres:16)")
	}

	er := NewEmbeddingRepository(db)
	_, err := er.NearestByEmbedding([]string{uuid.New().String()}, vecFromWeights(map[int]float32{0: 1}), 10)
	if !errors.Is(err, embeddings.ErrVectorUnavailable) {
		t.Fatalf("NearestByEmbedding without table = %v, want ErrVectorUnavailable", err)
	}

	_, err = er.DuplicateCandidates(uuid.New().String(), 0.15, 10)
	if !errors.Is(err, embeddings.ErrVectorUnavailable) {
		t.Fatalf("DuplicateCandidates without table = %v, want ErrVectorUnavailable", err)
	}
}

// TestDuplicateCandidates verifies the duplicate-pair self-join: near-duplicate
// requirements are paired above the threshold, symmetric pairs are collapsed to
// one, unrelated requirements are excluded, and non-requirement artifacts do
// not participate. Requires pgvector/pgvector:pg16.
func TestDuplicateCandidates(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	if !tableExists(t, db, "artifact_embeddings") {
		t.Skip("artifact_embeddings table absent (no vector extension); use pgvector/pgvector:pg16")
	}

	ar := NewArtifactRepository(db)
	er := NewEmbeddingRepository(db)
	proj := uuid.New().String()

	// r1 and r2 are near-duplicates (cosine similarity ~0.999 > 0.85).
	r1 := saveEmbeddedArtifact(t, ar, er, proj, "requirement", "The system shall log in users", vecFromWeights(map[int]float32{0: 1}))
	r2 := saveEmbeddedArtifact(t, ar, er, proj, "requirement", "Users shall be able to log in", vecFromWeights(map[int]float32{0: 1, 1: 0.05}))
	// r3 is unrelated (orthogonal): its nearest neighbour is far past the floor.
	saveEmbeddedArtifact(t, ar, er, proj, "requirement", "Reports export to PDF", vecFromWeights(map[int]float32{9: 1}))
	// A non-requirement artifact identical to r1 must not create a pair.
	saveEmbeddedArtifact(t, ar, er, proj, "persona", "Admin persona", vecFromWeights(map[int]float32{0: 1}))

	maxDistance := 1 - embeddings.DefaultDuplicateSimilarity
	pairs, err := er.DuplicateCandidates(proj, maxDistance, embeddings.MaxDuplicatePairs)
	if err != nil {
		t.Fatalf("DuplicateCandidates: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("got %d pairs, want exactly 1 (r1↔r2, deduped): %+v", len(pairs), pairs)
	}
	p := pairs[0]
	got := map[string]bool{p.ArtifactID: true, p.OtherID: true}
	if !got[r1] || !got[r2] {
		t.Errorf("pair = {%s, %s}, want {r1=%s, r2=%s}", p.ArtifactID, p.OtherID, r1, r2)
	}
	if p.Similarity() < embeddings.DefaultDuplicateSimilarity {
		t.Errorf("similarity = %v, want >= %v", p.Similarity(), embeddings.DefaultDuplicateSimilarity)
	}
	if p.ArtifactType != "requirement" || p.OtherType != "requirement" {
		t.Errorf("pair types = %q/%q, want requirement/requirement", p.ArtifactType, p.OtherType)
	}
}

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
