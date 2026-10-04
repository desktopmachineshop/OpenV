package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/members"
)

// registerReviewRoutes wires a project's review queue.
func (h *Handler) registerReviewRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/projects/{id}/review-queue", h.ReviewQueue).Methods("GET")
}

// ReviewQueue handles GET /api/v1/projects/{id}/review-queue: the reviewer's
// daily driver (issue #183). It returns the two things awaiting a reviewer's
// attention in one round trip — the live suspect links touching the project
// (each enriched with its endpoints' titles/types so a reviewer can judge it
// without opening both artifacts) and the artifacts sitting in the in_review
// state. Read-only, so project viewer role suffices; the per-row actions the
// UI offers (confirm a link, open an artifact) carry their own editor gates.
func (h *Handler) ReviewQueue(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]

	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}

	suspectLinks, err := h.LinkService.ListSuspectByProject(projectID)
	if err != nil {
		respondInternal(w, r, "failed to load suspect links", err)
		return
	}
	inReview, err := h.ArtifactService.ListByStatus(projectID, artifacts.StatusInReview)
	if err != nil {
		respondInternal(w, r, "failed to load in-review artifacts", err)
		return
	}

	// Normalize nil slices to [] so the JSON body always has array-typed
	// fields the frontend can map over unconditionally.
	if suspectLinks == nil {
		suspectLinks = []*links.SuspectLink{}
	}
	if inReview == nil {
		inReview = []*artifacts.Artifact{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"suspect_links":       suspectLinks,
		"in_review_artifacts": inReview,
	})
}
