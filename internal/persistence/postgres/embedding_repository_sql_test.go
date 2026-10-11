package postgres

import (
	"database/sql/driver"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/embeddings"
)

// X13's literal equality for the embedding repository: GetByArtifact's statement,
// assembled from embeddingColumns, is the literal it replaced, byte for byte
// (spacing, line breaks and tabs included), and the two searches, whose
// statements X13 leaves as they were (a join, an alias, the <=> operator and a
// LATERAL subquery), still send theirs. The statements are read off the wire by
// the recorder in pgkit_test.go, so the test needs no server and no vector
// extension, and checks what each method sends, not a copy of it. The
// repository has no transaction: each method sends its one statement and
// nothing else. Upsert's statement is untouched and is not in this file.

// The statements the reads send, as the base (68d122d) wrote them, byte for
// byte.
const (
	// oldEmbeddingGetByArtifactSelect, GetByArtifact
	oldEmbeddingGetByArtifactSelect = `
		SELECT artifact_id, artifact_version, model, content_hash
		FROM artifact_embeddings
		WHERE artifact_id = $1
	`
	// oldEmbeddingNearestSelect, NearestByEmbedding
	oldEmbeddingNearestSelect = `
		SELECT ae.artifact_id, a.project_id, a.type, a.title, a.body,
		       ae.embedding <=> $2::vector AS distance
		FROM artifact_embeddings ae
		JOIN artifacts a ON a.id = ae.artifact_id AND a.valid_to IS NULL
		WHERE a.project_id = ANY($1)
		ORDER BY distance
		LIMIT $3
	`
	// oldEmbeddingDuplicatesSelect, DuplicateCandidates
	oldEmbeddingDuplicatesSelect = `
		SELECT a.id, a.title, a.type,
		       n.artifact_id, n.title, n.type, n.distance
		FROM artifact_embeddings ae
		JOIN artifacts a ON a.id = ae.artifact_id AND a.valid_to IS NULL
		JOIN LATERAL (
			SELECT ae2.artifact_id, a2.title, a2.type,
			       ae.embedding <=> ae2.embedding AS distance
			FROM artifact_embeddings ae2
			JOIN artifacts a2 ON a2.id = ae2.artifact_id AND a2.valid_to IS NULL
			WHERE a2.project_id = a.project_id
			  AND a2.type = 'requirement'
			  AND ae2.artifact_id <> ae.artifact_id
			ORDER BY ae.embedding <=> ae2.embedding
			LIMIT 1
		) n ON TRUE
		WHERE a.project_id = $1::uuid
		  AND a.type = 'requirement'
		  AND n.distance <= $2
		ORDER BY n.distance
		LIMIT $3
	`
)

// The list the base wrote out, on one line.
const oldEmbeddingColumns = `artifact_id, artifact_version, model, content_hash`

// The columns are the old list.
func TestEmbeddingColumnsAreTheOldList(t *testing.T) {
	if embeddingColumns != oldEmbeddingColumns {
		t.Errorf("embeddingColumns =\n  %q\nwant\n  %q", embeddingColumns, oldEmbeddingColumns)
	}
}

// Each read sends its old literal and nothing else, and answers as it did for
// no row: GetByArtifact (nil, nil), NearestByEmbedding an empty, non-nil list,
// DuplicateCandidates an empty, non-nil list.
func TestEmbeddingReadsSendTheirOldLiterals(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewEmbeddingRepository(db)
	id := uuid.New().String()

	if got, err := repo.GetByArtifact(id); got != nil || err != nil {
		t.Errorf("GetByArtifact with no row: %v, %v; want nil, nil", got, err)
	}
	wantEvents(t, "GetByArtifact", rec.take(), "QUERY "+oldEmbeddingGetByArtifactSelect)

	hits, err := repo.NearestByEmbedding([]string{id}, []float32{1, 0}, 5)
	if err != nil || hits == nil || len(hits) != 0 {
		t.Errorf("NearestByEmbedding with no rows: %#v, %v; want an empty, non-nil list and no error", hits, err)
	}
	wantEvents(t, "NearestByEmbedding", rec.take(), "QUERY "+oldEmbeddingNearestSelect)

	pairs, err := repo.DuplicateCandidates(id, 0.15, 5)
	if err != nil || pairs == nil || len(pairs) != 0 {
		t.Errorf("DuplicateCandidates with no rows: %#v, %v; want an empty, non-nil list and no error", pairs, err)
	}
	wantEvents(t, "DuplicateCandidates", rec.take(), "QUERY "+oldEmbeddingDuplicatesSelect)
}

// The reads that send no statement still send none, and answer as they did.
func TestEmbeddingReadsThatSendNothing(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewEmbeddingRepository(db)

	for name, call := range map[string]func() (interface{}, error){
		"NearestByEmbedding with no scope":    func() (interface{}, error) { return repo.NearestByEmbedding(nil, []float32{1}, 5) },
		"NearestByEmbedding with no query":    func() (interface{}, error) { return repo.NearestByEmbedding([]string{uuid.New().String()}, nil, 5) },
		"NearestByEmbedding, malformed ids":   func() (interface{}, error) { return repo.NearestByEmbedding([]string{"junk"}, []float32{1}, 5) },
		"DuplicateCandidates with no project": func() (interface{}, error) { return repo.DuplicateCandidates("", 0.15, 5) },
	} {
		got, err := call()
		if err != nil || got == nil {
			t.Errorf("%s: %#v, %v; want an empty, non-nil list", name, got, err)
		}
		wantEvents(t, name, rec.take())
	}
}

// The scans read their columns in the order of the lists, one row each, as the
// recorder answers a statement with a row of values: GetByArtifact's four, a
// hit's six and a pair's seven. (The recorder cannot answer a row a scan
// refuses: database/sql names the column from the rows' Columns, which the
// recorder empties once it has handed its row over. The columns' places in a
// failing scan are pinned by embedding_repository_columns_test.go.)
func TestEmbeddingScansReadTheirColumnsInOrder(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewEmbeddingRepository(db)
	id := uuid.New().String()

	rec.answers = []recAnswer{
		{"FROM artifact_embeddings\n\t\tWHERE artifact_id", []driver.Value{"art", int64(7), "the-model", "the-hash"}},
		{"JOIN LATERAL", []driver.Value{"a", "a title", "a type", "b", "b title", "b type", 0.5}},
		{"AS distance", []driver.Value{"art", "proj", "requirement", "the title", "the body", 0.25}},
	}
	got, err := repo.GetByArtifact(id)
	want := &embeddings.Embedding{ArtifactID: "art", ArtifactVersion: 7, Model: "the-model", ContentHash: "the-hash"}
	if err != nil || got == nil || !reflect.DeepEqual(got, want) {
		t.Errorf("GetByArtifact = %#v, %v; want %#v", got, err, want)
	}
	hits, err := repo.NearestByEmbedding([]string{id}, []float32{1}, 5)
	wantHit := embeddings.NearestHit{ArtifactID: "art", ProjectID: "proj", Type: "requirement", Title: "the title", Body: "the body", Distance: 0.25}
	if err != nil || len(hits) != 1 || hits[0] != wantHit {
		t.Errorf("NearestByEmbedding = %#v, %v; want [%#v]", hits, err, wantHit)
	}
	pairs, err := repo.DuplicateCandidates(id, 0.6, 5)
	wantPair := embeddings.DuplicatePair{ArtifactID: "a", ArtifactTitle: "a title", ArtifactType: "a type",
		OtherID: "b", OtherTitle: "b title", OtherType: "b type", Distance: 0.5}
	if err != nil || len(pairs) != 1 || pairs[0] != wantPair {
		t.Errorf("DuplicateCandidates = %#v, %v; want [%#v]", pairs, err, wantPair)
	}
}
