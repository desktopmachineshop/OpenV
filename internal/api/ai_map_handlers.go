package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/members"
)

// ProjectAIMap serves GET /api/v1/projects/{id}/ai-map: the project's
// token-optimal outline for coding agents (see ai_map.go for the format).
// With ?baseline_id= the map is rendered from that baseline's snapshot
// instead of live state, so a release can ship a versioned map (e.g. saved
// into a code repo as .openv/requirements.md).
func (h *Handler) ProjectAIMap(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]

	project, err := h.projectService.GetProject(projectID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "project not found", err)
		return
	}
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}

	var payload exports.ProjectExport
	source := "live state"

	if baselineID := r.URL.Query().Get("baseline_id"); baselineID != "" {
		baseline, err := h.baselineService.GetProjectBaseline(projectID, baselineID)
		if err != nil {
			writeJSONError(w, http.StatusNotFound, "baseline not found in this project")
			return
		}
		if err := json.Unmarshal(baseline.Snapshot, &payload); err != nil {
			respondInternal(w, r, "failed to parse baseline snapshot", err)
			return
		}
		source = fmt.Sprintf("baseline %q (%s, captured %s)",
			baseline.Name, baseline.ID, baseline.CreatedAt.UTC().Format(time.RFC3339))
	} else {
		arts, err := h.artifactService.GetArtifactsByProject(projectID)
		if err != nil {
			respondInternal(w, r, "failed to list artifacts", err)
			return
		}
		lks, err := h.linkService.GetAllLinks(projectID)
		if err != nil {
			respondInternal(w, r, "failed to list links", err)
			return
		}
		payload.Artifacts = arts
		payload.Links = lks
	}

	text := buildAIMap(project.Name, source, payload.Artifacts, payload.Links, time.Now())
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Write([]byte(text))
}
