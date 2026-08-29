package embeddings

import (
	"log/slog"
	"strings"

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
}

// NewService wires the backfill. Any of provider/store/artifacts may be nil in
// tests or a stripped-down deployment; the methods stay safe no-ops when the
// pieces they need are absent or the provider is disabled.
func NewService(provider Provider, store Store, reader ArtifactReader) *Service {
	return &Service{provider: provider, store: store, artifacts: reader}
}

// Enabled reports whether embedding can actually run (a configured provider
// and a store).
func (s *Service) Enabled() bool {
	return s != nil && s.store != nil && s.provider != nil && s.provider.Enabled()
}

// IndexArtifact satisfies artifacts.EmbeddingIndexer. It is the hook the
// artifact create/update path calls: fire-and-forget, so a slow or failing
// provider never delays or fails the write. Errors are logged and swallowed.
func (s *Service) IndexArtifact(id string, version int, title, body string) {
	if !s.Enabled() {
		return
	}
	go func() {
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

	return s.store.Upsert(&Embedding{
		ArtifactID:      id,
		ArtifactVersion: version,
		Vector:          vectors[0],
		Model:           model,
		ContentHash:     hash,
	})
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
