// Package embeddings computes and stores vector embeddings of artifact
// content for semantic (nearest-neighbour) search (issue #220, the phase-4
// AI-native infra half; the query endpoint is #221).
//
// The moving parts:
//
//   - Provider  — an AI backend that turns text into a float vector. The
//     OpenAI-compatible HTTP impl (see provider.go) POSTs to /embeddings.
//     It is ENV-GATED and DISABLED by default: with OPENV_EMBEDDING_API_KEY
//     unset the provider's Enabled() is false and every embed call is a silent
//     no-op, exactly like the SMTP mailer in internal/notify/email.go. The
//     whole feature therefore adds nothing to a default deployment.
//   - Store     — persistence for one embedding per artifact (upsert by
//     artifact_id), implemented by postgres.EmbeddingRepository.
//   - Service   — the backfill logic: it embeds an artifact only when its
//     content (title+body) hash or the embedding model has changed since the
//     last stored embedding, so unchanged content is never re-embedded.
//
// The artifact create/update path drives embedding through the
// artifacts.EmbeddingIndexer seam (best-effort, async, errors swallowed), and
// an admin reindex endpoint backfills a whole project on demand.
package embeddings

import (
	"crypto/sha256"
	"encoding/hex"
)

// Dimensions is the vector width the schema and the default model agree on.
// 1536 matches OpenAI text-embedding-3-small (the default model below) and is
// the safe default for the common OpenAI-compatible embedding backends. It is
// baked into the artifact_embeddings.embedding vector(N) column by migration
// 0016, so changing it is a schema change (a new migration), not a config
// tweak: a provider whose model emits a different width must not be pointed at
// a database built for this one.
const Dimensions = 1536

// DefaultModel is the embedding model used when OPENV_EMBEDDING_MODEL is unset.
// text-embedding-3-small emits Dimensions-wide vectors.
const DefaultModel = "text-embedding-3-small"

// ContentHash is the fingerprint of the embeddable content of an artifact —
// its title and body. A stored embedding records the hash of the content it
// was computed from; the backfill re-embeds only when the current hash differs
// (content changed) so unchanged artifacts are skipped cheaply. The NUL
// separator keeps title|body from colliding with a different title/body split
// at the same boundary.
func ContentHash(title, body string) string {
	sum := sha256.Sum256([]byte(title + "\x00" + body))
	return hex.EncodeToString(sum[:])
}

// EmbeddableText is the exact string handed to the provider for an artifact:
// the title and body joined by a blank line. Kept in one place so the hashed
// content and the embedded content never drift apart.
func EmbeddableText(title, body string) string {
	if title == "" {
		return body
	}
	if body == "" {
		return title
	}
	return title + "\n\n" + body
}

// Embedding is one artifact's stored vector, pinned to the content version it
// was computed from.
type Embedding struct {
	ArtifactID      string
	ArtifactVersion int
	Vector          []float32
	Model           string
	ContentHash     string
}

// Provider turns text into embedding vectors. Enabled() reports whether a real
// backend is configured; when false, callers must treat embedding as disabled
// and skip it (no error). Embed returns one vector per input string, in order.
type Provider interface {
	Enabled() bool
	Model() string
	Embed(texts []string) ([][]float32, error)
}

// Store persists artifact embeddings. GetByArtifact returns (nil, nil) when no
// embedding exists yet. Upsert replaces any existing row for the artifact_id.
//
// The read side of semantic search (nearest-neighbour lookup) is deliberately
// NOT part of this interface — that is issue #221. The seam it will fill lives
// as a documented stub on the postgres implementation
// (EmbeddingRepository.NearestByEmbedding).
type Store interface {
	Upsert(e *Embedding) error
	GetByArtifact(artifactID string) (*Embedding, error)
}
