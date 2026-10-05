package api

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/snapshot"
)

// registerAIMapRoutes wires a project's outline for coding agents.
func (h *Handler) registerAIMapRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/projects/{id}/ai-map", h.ProjectAIMap).Methods("GET")
}

// ProjectAIMap serves GET /api/v1/projects/{id}/ai-map: the project's
// token-optimal outline for coding agents (see ai_map.go for the format).
// With ?baseline_id= the map is rendered from that baseline's snapshot
// instead of live state, so a release can ship a versioned map (e.g. saved
// into a code repo as .openv/requirements.md).
//
// The guard comes first, so that a project the caller cannot reach answers
// as one no row has (I3).
func (h *Handler) ProjectAIMap(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]

	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	project, err := h.ProjectService.GetProject(projectID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "project not found", err)
		return
	}

	var payload exports.ProjectExport
	source := "live state"

	// Any baseline_id names a baseline, "live" too: there is no baseline
	// "live", so it answers as one no row has.
	if baselineID := r.URL.Query().Get("baseline_id"); baselineID != "" {
		loaded, baseline, err := snapshot.Load(projectID, baselineID, h.snapshotSources())
		var bad *snapshot.DecodeError
		if errors.As(err, &bad) {
			respondInternal(w, r, "failed to parse baseline snapshot", err)
			return
		}
		if err != nil {
			writeJSONError(w, http.StatusNotFound, "baseline not found in this project")
			return
		}
		payload = *loaded
		source = fmt.Sprintf("baseline %q (%s, captured %s)",
			baseline.Name, baseline.ID, baseline.CreatedAt.UTC().Format(time.RFC3339))
	} else {
		arts, err := h.ArtifactService.GetArtifactsByProject(projectID)
		if err != nil {
			respondInternal(w, r, "failed to list artifacts", err)
			return
		}
		lks, err := h.LinkService.GetAllLinks(projectID)
		if err != nil {
			respondInternal(w, r, "failed to list links", err)
			return
		}
		payload.Artifacts = arts
		payload.Links = lks
	}

	text := buildAIMap(project.Name, source, h.qualityRuleSetFor(projectID).Describe(),
		payload.Artifacts, payload.Links, time.Now())
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Write([]byte(text))
}
