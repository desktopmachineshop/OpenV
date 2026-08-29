package embeddings

import (
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
)

// ArtifactReader is the read side the backfill needs: the current artifacts of
// a project. artifacts.Service satisfies it.
type ArtifactReader interface {
	GetArtifactsByProject(projectID string) ([]*artifacts.Artifact, error)
}

// Service is the embedding backfill engine. It embeds an artifact only when
// the stored embedding is missing, was computed from different content (hash
// mismatch), or used a different model — so unchanged content is never
// re-embedded. It implements artifacts.EmbeddingIndexer (via IndexArtifact) so
// the artifact create/update path can drive it best-effort and async.
type Service struct {
	provider  Provider
	store     Store
	artifacts ArtifactReader

	// sem bounds the fire-and-forget IndexArtifact fan-out (issue #244). It is a
	// buffered channel used as a counting semaphore: a write burst can occupy at
	// most cap(sem) concurrent embedding goroutines, and further calls are
	// dropped (never blocked) so the artifact write path is never delayed by a
	// slow provider. A dropped index is harmless — the content is re-embedded on
	// the next edit or an admin reindex.
	sem chan struct{}

	// dimMismatch latches true when the provider returns a vector whose width
	// does not match the store's fixed Dimensions (issue #247). The store column
	// is vector(Dimensions), so a wrong-width vector makes every Upsert fail on
	// the same constraint forever; instead we detect it once, disable embedding
	// (Enabled() then reports false), and log a clear, actionable error so an
	// operator fixes the model/config rather than staring at repeated failures.
	dimMismatch atomic.Bool
}

// indexWorkerCap is the maximum number of concurrent background IndexArtifact
// embeddings. Small on purpose: embedding is best-effort and off the write
// path, so a handful of workers absorbs a normal burst while a pathological
// burst is shed rather than spawning unbounded goroutines.
const indexWorkerCap = 4

// NewService wires the backfill. Any of provider/store/artifacts may be nil in
// tests or a stripped-down deployment; the methods stay safe no-ops when the
// pieces they need are absent or the provider is disabled.
func NewService(provider Provider, store Store, reader ArtifactReader) *Service {
	return &Service{
		provider:  provider,
		store:     store,
		artifacts: reader,
		sem:       make(chan struct{}, indexWorkerCap),
	}
}

// Enabled reports whether embedding can actually run (a configured provider
// and a store, and no latched dimension mismatch — see dimMismatch).
func (s *Service) Enabled() bool {
	return s != nil && s.store != nil && s.provider != nil && s.provider.Enabled() && !s.dimMismatch.Load()
}

// IndexArtifact satisfies artifacts.EmbeddingIndexer. It is the hook the
// artifact create/update path calls: fire-and-forget, so a slow or failing
// provider never delays or fails the write. Errors are logged and swallowed.
func (s *Service) IndexArtifact(id string, version int, title, body string) {
	if !s.Enabled() {
		return
	}
	// Bound the fan-out: acquire a worker slot without blocking. If all
	// indexWorkerCap slots are busy (a write burst), drop this index rather than
	// spawning another goroutine or stalling the caller — the content is
	// re-embedded on the next edit or an admin reindex. (A nil sem — a Service
	// built without NewService — makes the send never ready, so every index is
	// dropped; NewService always allocates it.)
	select {
	case s.sem <- struct{}{}:
	default:
		slog.Debug("embeddings: index fan-out saturated; dropping best-effort index", "artifact_id", id)
		return
	}
	go func() {
		defer func() { <-s.sem }()
		if err := s.embed(id, version, title, body); err != nil {
			slog.Warn("embeddings: failed to index artifact", "artifact_id", id, "error", err)
		}
	}()
}

// EmbedArtifact embeds one artifact synchronously (used by the project
// reindex). It is a no-op when embedding is disabled. It skips the provider
// call and the write when the stored embedding already covers this content and
// model (content-hash match), so re-running a backfill is cheap.
func (s *Service) EmbedArtifact(a *artifacts.Artifact) error {
	if a == nil {
		return nil
	}
	return s.embed(a.ID, a.Version, a.Title, a.Body)
}

func (s *Service) embed(id string, version int, title, body string) error {
	if !s.Enabled() {
		return nil
	}
	hash := ContentHash(title, body)
	model := s.provider.Model()

	// Skip if the current embedding already matches this content and model.
	if existing, err := s.store.GetByArtifact(id); err == nil && existing != nil {
		if existing.ContentHash == hash && existing.Model == model {
			return nil
		}
	} else if err != nil {
		// A read failure (e.g. the artifact_embeddings table is absent on a
		// vector-less database) must not stop us — fall through and let the
		// write surface the real error, best-effort.
		slog.Debug("embeddings: could not read existing embedding; proceeding", "artifact_id", id, "error", err)
	}

	text := EmbeddableText(title, body)
	if strings.TrimSpace(text) == "" {
		return nil // nothing to embed
	}

	vectors, err := s.provider.Embed([]string{text})
	if err != nil {
		return err
	}
	if len(vectors) != 1 {
		return nil // disabled provider or empty result; nothing to store
	}

	// Validate the vector width against the store's fixed Dimensions before the
	// Upsert (issue #247). The embedding column is vector(Dimensions); a
	// wrong-width vector — a misconfigured model, or a provider whose default
	// dimensions differ — would otherwise fail every Upsert on the same
	// constraint indefinitely. Latch the service disabled on the first mismatch
	// and surface a clear, actionable error instead.
	if got := len(vectors[0]); got != Dimensions {
		s.dimMismatch.Store(true)
		err := fmt.Errorf("embeddings: provider returned %d-dimension vectors but the store expects %d; disabling embedding until the model/config is corrected (set OPENV_EMBEDDING_MODEL to a %d-dim model, or update the schema)", got, Dimensions, Dimensions)
		slog.Error(err.Error(), "provider_model", model)
		return err
	}

	return s.store.Upsert(&Embedding{
		ArtifactID:      id,
		ArtifactVersion: version,
		Vector:          vectors[0],
		Model:           model,
		ContentHash:     hash,
	})
}

// SemanticSearch embeds the query and returns the nearest artifacts within the
// given projects. It is the read path issue #221 adds on top of the #220 infra.
//
// Failure modes are distinguished so the API layer can degrade rather than
// error the whole search:
//
//   - ErrDisabled          — embeddings not configured; use keyword search.
//   - ErrVectorUnavailable — configured, but the vector table is absent or the
//     store has no read path; use keyword search.
//
// A real provider or query error is returned as-is.
func (s *Service) SemanticSearch(projectIDs []string, query string, limit int) ([]NearestHit, error) {
	if !s.Enabled() {
		return nil, ErrDisabled
	}
	searcher, ok := s.store.(Searcher)
	if !ok {
		return nil, ErrVectorUnavailable
	}
	if len(projectIDs) == 0 || strings.TrimSpace(query) == "" {
		return []NearestHit{}, nil
	}

	vectors, err := s.provider.Embed([]string{query})
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 || len(vectors[0]) == 0 {
		// A disabled or empty provider result: treat as "no semantic path"
		// so the caller falls back to keyword rather than returning nothing.
		return nil, ErrVectorUnavailable
	}
	return searcher.NearestByEmbedding(projectIDs, vectors[0], limit)
}

// DuplicatePairs returns candidate-duplicate requirement pairs for a project:
// each requirement paired with its nearest other requirement above the
// similarity threshold. It is a no-op-shaped ErrDisabled/ErrVectorUnavailable
// when embeddings are off or the vector table is absent, so the endpoint can
// answer "not available" rather than fail.
func (s *Service) DuplicatePairs(projectID string, limit int) ([]DuplicatePair, error) {
	if !s.Enabled() {
		return nil, ErrDisabled
	}
	searcher, ok := s.store.(Searcher)
	if !ok {
		return nil, ErrVectorUnavailable
	}
	if limit <= 0 || limit > MaxDuplicatePairs {
		limit = MaxDuplicatePairs
	}
	maxDistance := 1 - DefaultDuplicateSimilarity
	return searcher.DuplicateCandidates(projectID, maxDistance, limit)
}

// ReindexProject re-embeds every current artifact in a project whose stored
// embedding is missing or stale (content-hash / model mismatch). It returns
// the number of artifacts examined. A per-artifact failure is logged and
// skipped so one bad artifact does not abort the whole backfill. It is a no-op
// (0, nil) when embedding is disabled.
func (s *Service) ReindexProject(projectID string) (int, error) {
	if !s.Enabled() || s.artifacts == nil {
		return 0, nil
	}
	list, err := s.artifacts.GetArtifactsByProject(projectID)
	if err != nil {
		return 0, err
	}
	for _, a := range list {
		if err := s.EmbedArtifact(a); err != nil {
			slog.Warn("embeddings: reindex failed for artifact", "artifact_id", a.ID, "error", err)
		}
	}
	return len(list), nil
}
