package postgres

import (
	"database/sql"
	"fmt"
	"log/slog"
)

// 0016: pgvector infrastructure for semantic artifact search (issue #220,
// phase-4 AI-native). Creates the `vector` extension and an
// artifact_embeddings table holding one embedding per artifact, pinned to
// the content version it was computed from.
//
// The whole migration is guarded exactly like the pg_trgm one (0009):
// CREATE EXTENSION needs a privilege some managed Postgres roles lack, so
// we probe for the extension and, when it is absent AND uncreatable, roll
// back to a savepoint, log a warning, and SKIP THE TABLE ENTIRELY — a
// vector(N) column cannot exist without the extension. The migration still
// returns nil so applyOnce records it in the ledger: boot must never brick
// on a vector-less managed database. Semantic search simply stays
// unavailable there (the existing trigram/ILIKE search is untouched), and
// the embedding provider is disabled by default anyway. If the extension
// is (or becomes) available, the table and its ANN index are created.
//
// The vector width is embeddings.Dimensions (1536 — OpenAI
// text-embedding-3-small); see that constant for why it is a schema
// concern, not config. PK on artifact_id keeps exactly one current
// embedding per artifact (the store upserts by artifact_id).
func m0016PgvectorArtifactEmbeddings(tx *sql.Tx) error {
	var haveVector bool
	if err := tx.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'vector')`,
	).Scan(&haveVector); err != nil {
		return err
	}
	if !haveVector {
		if _, err := tx.Exec(`SAVEPOINT vector_ext`); err != nil {
			return err
		}
		if _, err := tx.Exec(`CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
			if _, rbErr := tx.Exec(`ROLLBACK TO SAVEPOINT vector_ext`); rbErr != nil {
				return rbErr
			}
			slog.Warn("vector extension unavailable; skipping artifact_embeddings table and semantic search (existing trigram/ILIKE search is unaffected)",
				slog.Any("error", err))
			return nil
		}
		if _, err := tx.Exec(`RELEASE SAVEPOINT vector_ext`); err != nil {
			return err
		}
	}
	// The extension exists (pre-installed or just created): the vector
	// column is now legal.
	if _, err := tx.Exec(fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS artifact_embeddings (
				artifact_id UUID PRIMARY KEY,
				artifact_version INT NOT NULL,
				embedding vector(%d) NOT NULL,
				model VARCHAR NOT NULL,
				content_hash VARCHAR NOT NULL,
				created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)
		`, embeddingDimensions)); err != nil {
		return err
	}
	// Approximate-nearest-neighbour index for the cosine-distance query the
	// #221 search endpoint will run. HNSW is empty-table-cheap to build and
	// leaves that PR nothing to do but SELECT. IF NOT EXISTS so re-applying
	// is a no-op.
	_, err := tx.Exec(`
			CREATE INDEX IF NOT EXISTS idx_artifact_embeddings_hnsw
			ON artifact_embeddings USING hnsw (embedding vector_cosine_ops)
		`)
	return err
}
