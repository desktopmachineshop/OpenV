package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"

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
