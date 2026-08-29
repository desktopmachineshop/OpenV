package postgres

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/embeddings"
)

// EmbeddingRepository implements embeddings.Store on the artifact_embeddings
// table (migration 0016). Vectors are passed to Postgres as the pgvector text
// literal "[v1,v2,...]" cast to ::vector, so no pgvector Go driver is needed.
//
// The whole path is best-effort: on a database where the vector extension was
// unavailable at migration time, the table does not exist and these methods
// return the underlying "relation does not exist" error, which the caller (the
// async indexer) logs and swallows.
type EmbeddingRepository struct {
	db *sql.DB
}

// NewEmbeddingRepository creates a new embedding repository.
func NewEmbeddingRepository(db *sql.DB) *EmbeddingRepository {
	return &EmbeddingRepository{db: db}
}

// ErrNearestNotImplemented marks the semantic-search read path as the seam
// left for issue #221 (see NearestByEmbedding).
var ErrNearestNotImplemented = errors.New("embeddings: nearest-neighbour search not implemented (issue #221)")

// encodeVector renders a float32 slice as the pgvector text literal
// "[v1,v2,...]". strconv with -1 precision emits the shortest round-trippable
// decimal for each component.
func encodeVector(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// Upsert stores (or replaces) the embedding for an artifact, keyed by
// artifact_id so there is exactly one current embedding per artifact.
func (r *EmbeddingRepository) Upsert(e *embeddings.Embedding) error {
	if e == nil {
		return nil
	}
	ctx, cancel := stmtCtx()
	defer cancel()

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO artifact_embeddings (artifact_id, artifact_version, embedding, model, content_hash, created_at)
		VALUES ($1, $2, $3::vector, $4, $5, NOW())
		ON CONFLICT (artifact_id) DO UPDATE SET
			artifact_version = EXCLUDED.artifact_version,
			embedding = EXCLUDED.embedding,
			model = EXCLUDED.model,
			content_hash = EXCLUDED.content_hash,
			created_at = EXCLUDED.created_at
	`, e.ArtifactID, e.ArtifactVersion, encodeVector(e.Vector), e.Model, e.ContentHash)
	return err
}

// GetByArtifact returns the stored embedding metadata for an artifact, or
// (nil, nil) when none exists. The vector itself is not read back — the only
// caller (the backfill's staleness check) needs just the content hash, model,
// and version — so an absent vertex column parser is one less thing to
// maintain here. The #221 read path will select the embedding directly.
func (r *EmbeddingRepository) GetByArtifact(artifactID string) (*embeddings.Embedding, error) {
	ctx, cancel := stmtCtx()
	defer cancel()

	e := &embeddings.Embedding{}
	err := r.db.QueryRowContext(ctx, `
		SELECT artifact_id, artifact_version, model, content_hash
		FROM artifact_embeddings
		WHERE artifact_id = $1
	`, artifactID).Scan(&e.ArtifactID, &e.ArtifactVersion, &e.Model, &e.ContentHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return e, nil
}

// NearestByEmbedding is the seam issue #221 (the semantic-search query
// endpoint) will implement. It is intentionally a documented stub here so this
// PR ships infra + backfill only, with a clear place for the read path to
// land: a cosine-distance ORDER BY over the idx_artifact_embeddings_hnsw index,
// scoped to the caller's projects. Signature sketch (to be finalized in #221):
//
//	SELECT ae.artifact_id, a.project_id, a.title, a.type,
//	       ae.embedding <=> $query::vector AS distance
//	FROM artifact_embeddings ae
//	JOIN artifacts a ON a.id = ae.artifact_id AND a.valid_to IS NULL
//	WHERE a.project_id = ANY($projects)
//	ORDER BY distance
//	LIMIT $limit
func (r *EmbeddingRepository) NearestByEmbedding(projectIDs []string, query []float32, limit int) (any, error) {
	return nil, ErrNearestNotImplemented
}
