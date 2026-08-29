package postgres

import (
	"database/sql"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/lib/pq"

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

// undefinedTable is the SQLSTATE pgvector-less databases raise when the
// artifact_embeddings table was skipped at migration time (see migration
// 0016). The semantic read path maps it to embeddings.ErrVectorUnavailable so
// the search endpoint degrades to the trigram path instead of 500-ing.
const undefinedTable = "42P01"

// isUndefinedTable reports whether err is a Postgres "relation does not exist"
// error — i.e. the artifact_embeddings table is absent on this database.
func isUndefinedTable(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return string(pqErr.Code) == undefinedTable
	}
	return false
}

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

// NearestByEmbedding runs the cosine-distance kNN query behind semantic search
// (issue #221): the artifacts in projectIDs closest to the query vector,
// nearest first. It rides the idx_artifact_embeddings_hnsw HNSW index
// (vector_cosine_ops) created by migration 0016, joined to the current
// (valid_to IS NULL) artifacts and scoped to the given projects.
//
// On a database where the vector extension was unavailable at migration time
// the artifact_embeddings table does not exist; that surfaces as a Postgres
// "undefined table" error, which is mapped to embeddings.ErrVectorUnavailable
// so the caller degrades to the trigram/ILIKE path rather than failing.
func (r *EmbeddingRepository) NearestByEmbedding(projectIDs []string, query []float32, limit int) ([]embeddings.NearestHit, error) {
	if len(projectIDs) == 0 || len(query) == 0 {
		return []embeddings.NearestHit{}, nil
	}
	if limit <= 0 {
		limit = 20
	}

	ctx, cancel := stmtCtx()
	defer cancel()

	rows, err := r.db.QueryContext(ctx, `
		SELECT ae.artifact_id, a.project_id, a.type, a.title, a.body,
		       ae.embedding <=> $2::vector AS distance
		FROM artifact_embeddings ae
		JOIN artifacts a ON a.id = ae.artifact_id AND a.valid_to IS NULL
		WHERE a.project_id = ANY($1)
		ORDER BY distance
		LIMIT $3
	`, pq.Array(projectIDs), encodeVector(query), limit)
	if err != nil {
		if isUndefinedTable(err) {
			return nil, embeddings.ErrVectorUnavailable
		}
		return nil, err
	}
	defer rows.Close()

	hits := []embeddings.NearestHit{}
	for rows.Next() {
		var h embeddings.NearestHit
		if err := rows.Scan(&h.ArtifactID, &h.ProjectID, &h.Type, &h.Title, &h.Body, &h.Distance); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// DuplicateCandidates finds candidate-duplicate requirement pairs in a project
// (issue #221): for each current requirement, its single nearest OTHER current
// requirement within maxDistance (cosine), via a LATERAL kNN over the HNSW
// index. Symmetric pairs (A→B and B→A) are collapsed to one, keeping the
// closer distance, and the result is capped at limit.
//
// Like NearestByEmbedding, an absent artifact_embeddings table degrades to
// embeddings.ErrVectorUnavailable rather than an error.
func (r *EmbeddingRepository) DuplicateCandidates(projectID string, maxDistance float64, limit int) ([]embeddings.DuplicatePair, error) {
	if projectID == "" {
		return []embeddings.DuplicatePair{}, nil
	}
	if limit <= 0 || limit > embeddings.MaxDuplicatePairs {
		limit = embeddings.MaxDuplicatePairs
	}

	ctx, cancel := stmtCtx()
	defer cancel()

	// Scan up to 2*limit rows so symmetric-pair dedup (which can drop up to
	// half) still leaves a full page. Each source requirement contributes at
	// most one nearest-neighbour row.
	rows, err := r.db.QueryContext(ctx, `
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
	`, projectID, maxDistance, 2*limit)
	if err != nil {
		if isUndefinedTable(err) {
			return nil, embeddings.ErrVectorUnavailable
		}
		return nil, err
	}
	defer rows.Close()

	seen := map[string]bool{}
	pairs := []embeddings.DuplicatePair{}
	for rows.Next() {
		var p embeddings.DuplicatePair
		if err := rows.Scan(&p.ArtifactID, &p.ArtifactTitle, &p.ArtifactType,
			&p.OtherID, &p.OtherTitle, &p.OtherType, &p.Distance); err != nil {
			return nil, err
		}
		// Collapse symmetric pairs on the unordered {id,otherID} key. Rows are
		// distance-ordered, so the first occurrence is the closer one to keep.
		key := pairKey(p.ArtifactID, p.OtherID)
		if seen[key] {
			continue
		}
		seen[key] = true
		pairs = append(pairs, p)
		if len(pairs) >= limit {
			break
		}
	}
	return pairs, rows.Err()
}

// pairKey builds an order-independent key for a pair of artifact ids so A→B and
// B→A collapse to one entry.
func pairKey(a, b string) string {
	ids := []string{a, b}
	sort.Strings(ids)
	return ids[0] + "|" + ids[1]
}
