package embeddings

import "errors"

// The read side of semantic search (issue #221). The infra PR (#220) shipped
// the store's write path and left NearestByEmbedding a documented stub; this is
// where the query path lands.

// ErrDisabled means embeddings are not configured (no provider / no store), so
// semantic search cannot run at all. Callers degrade to keyword search.
var ErrDisabled = errors.New("embeddings: semantic search disabled (no provider configured)")

// ErrVectorUnavailable means the provider is configured but the vector table
// is absent (the database had no pgvector extension at migration time) or the
// store does not implement the semantic read path. The endpoint degrades to
// the keyword (trigram) path rather than failing — semantic search is
// best-effort infrastructure and must never 500 a search request.
var ErrVectorUnavailable = errors.New("embeddings: vector store unavailable")

// DefaultDuplicateSimilarity is the cosine-similarity floor for a pair of
// requirements to count as candidate duplicates. 0.85 keeps the surface to
// near-restatements rather than merely topically-related items. Cosine
// distance (pgvector's <=>) is 1 - similarity, so the distance ceiling is
// 1 - this.
const DefaultDuplicateSimilarity = 0.85

// MaxDuplicatePairs bounds the duplicate-detection scan so a large project
// cannot turn one request into an unbounded amount of work.
const MaxDuplicatePairs = 100

// NearestHit is one nearest-neighbour semantic-search result: the matched
// artifact plus its cosine distance to the query vector (smaller is closer).
// Body is carried so the API layer can build the same snippet shape the
// keyword path returns.
type NearestHit struct {
	ArtifactID string
	ProjectID  string
	Type       string
	Title      string
	Body       string
	Distance   float64
}

// Similarity converts the stored cosine distance to a 0..1 similarity score
// (1 = identical direction). It is the value surfaced to the UI as "score".
func (h NearestHit) Similarity() float64 { return 1 - h.Distance }

// DuplicatePair is one candidate-duplicate pairing: an artifact and its nearest
// other artifact whose similarity clears the threshold.
type DuplicatePair struct {
	ArtifactID    string
	ArtifactTitle string
	ArtifactType  string
	OtherID       string
	OtherTitle    string
	OtherType     string
	Distance      float64
}

// Similarity converts the pair's cosine distance to a 0..1 similarity score.
func (p DuplicatePair) Similarity() float64 { return 1 - p.Distance }

// Searcher is the semantic read path, implemented by
// postgres.EmbeddingRepository. It is kept separate from Store (the write
// path) and type-asserted by the service so a store that predates it, or a
// database without the vector extension, degrades cleanly instead of forcing
// every Store implementation to carry a kNN query.
type Searcher interface {
	// NearestByEmbedding returns the artifacts in projectIDs closest to query
	// by cosine distance, nearest first. It returns ErrVectorUnavailable when
	// the vector table is absent so the caller can fall back to keyword search.
	NearestByEmbedding(projectIDs []string, query []float32, limit int) ([]NearestHit, error)
	// DuplicateCandidates returns, for each current requirement in the project,
	// its nearest other requirement whose cosine distance is within maxDistance,
	// nearest first, symmetric pairs deduplicated, capped at limit.
	DuplicateCandidates(projectID string, maxDistance float64, limit int) ([]DuplicatePair, error)
}
