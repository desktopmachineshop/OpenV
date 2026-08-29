package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/embeddings"
	"github.com/openv/requirements-platform/internal/domain/members"
)

// ReindexEmbeddings handles POST /api/v1/projects/{id}/reindex-embeddings: an
// on-demand backfill that (re)computes semantic-search embeddings for every
// current artifact in the project whose stored embedding is missing or stale
// (issue #220). Embedding normally happens automatically on artifact
// create/update; this endpoint exists to seed a project that predates the
// feature, or to recover after the embedding provider was down.
//
// Admin action: requires project owner rights (org admins and platform admins
// pass the project ladder as owners). The work runs off the request path in a
// goroutine and the endpoint returns immediately — a large project must not
// hold the connection open, and embedding is best-effort infrastructure.
//
// When embeddings are not configured (no OPENV_EMBEDDING_API_KEY) or the
// vector extension was unavailable at migration time, the service is a no-op;
// the response says so plainly rather than pretending work was queued.
func (h *Handler) ReindexEmbeddings(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]

	if !h.requireProjectRole(w, r, projectID, members.RoleOwner) {
		return
	}

	if h.embeddingService == nil || !h.embeddingService.Enabled() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"enabled": false,
			"note":    "semantic-search embedding is not configured; nothing to reindex",
		})
		return
	}

	go func() {
		n, err := h.embeddingService.ReindexProject(projectID)
		if err != nil {
			slog.Warn("api: reindex-embeddings failed", "project_id", projectID, "error", err)
			return
		}
		slog.Info("api: reindex-embeddings complete", "project_id", projectID, "artifacts", n)
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"enabled": true,
		"note":    "reindex started; embeddings are being recomputed in the background",
	})
}

// duplicatePair is one candidate-duplicate pairing in the DuplicateCandidates
// response: a requirement and its nearest other requirement, with the 0..1
// cosine similarity between them (higher = more alike).
type duplicatePair struct {
	ArtifactID    string  `json:"artifact_id"`
	ArtifactTitle string  `json:"artifact_title"`
	ArtifactType  string  `json:"artifact_type"`
	OtherID       string  `json:"other_id"`
	OtherTitle    string  `json:"other_title"`
	OtherType     string  `json:"other_type"`
	Similarity    float64 `json:"similarity"`
}

// duplicatesResponse is the envelope for GET /api/v1/projects/{id}/duplicates.
// Enabled is false (with an explanatory note and an empty list) when semantic
// embeddings are unconfigured or the vector store is unavailable.
type duplicatesResponse struct {
	Enabled bool            `json:"enabled"`
	Note    string          `json:"note,omitempty"`
	Pairs   []duplicatePair `json:"pairs"`
}

// DuplicateCandidates handles GET /api/v1/projects/{id}/duplicates: for each
// current requirement in the project, its nearest other requirement above the
// similarity threshold, as candidate duplicate pairs ranked closest-first
// (issue #221). Read-only, viewer role.
//
// When semantic embeddings are not configured, or the vector extension was
// unavailable at migration time, the endpoint returns an empty list with
// enabled=false and a note rather than an error — duplicate detection is
// best-effort and must never 500 a project view.
func (h *Handler) DuplicateCandidates(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]

	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if h.embeddingService == nil || !h.embeddingService.Enabled() {
		json.NewEncoder(w).Encode(duplicatesResponse{
			Enabled: false,
			Note:    "semantic-search embeddings are not configured; duplicate detection is unavailable",
			Pairs:   []duplicatePair{},
		})
		return
	}

	found, err := h.embeddingService.DuplicatePairs(projectID, embeddings.MaxDuplicatePairs)
	if err != nil {
		if errors.Is(err, embeddings.ErrDisabled) || errors.Is(err, embeddings.ErrVectorUnavailable) {
			json.NewEncoder(w).Encode(duplicatesResponse{
				Enabled: false,
				Note:    "the vector store is unavailable on this database; duplicate detection is disabled",
				Pairs:   []duplicatePair{},
			})
			return
		}
		respondInternal(w, r, "duplicate detection failed", err)
		return
	}

	pairs := make([]duplicatePair, 0, len(found))
	for _, p := range found {
		pairs = append(pairs, duplicatePair{
			ArtifactID:    p.ArtifactID,
			ArtifactTitle: p.ArtifactTitle,
			ArtifactType:  p.ArtifactType,
			OtherID:       p.OtherID,
			OtherTitle:    p.OtherTitle,
			OtherType:     p.OtherType,
			Similarity:    p.Similarity(),
		})
	}
	json.NewEncoder(w).Encode(duplicatesResponse{Enabled: true, Pairs: pairs})
}
