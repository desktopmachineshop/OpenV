package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/members"
)

// registerBaselineRoutes wires baselines: a project's create and list, and
// one baseline's read, diff (baseline_diff_handlers.go) and delete.
func (h *Handler) registerBaselineRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/projects/{id}/baselines", h.CreateBaseline).Methods("POST")
	router.HandleFunc("/api/v1/projects/{id}/baselines", h.ListBaselines).Methods("GET")
	router.HandleFunc("/api/v1/baselines/{id}", h.GetBaseline).Methods("GET")
	router.HandleFunc("/api/v1/baselines/{id}/diff", h.DiffBaseline).Methods("GET")
	router.HandleFunc("/api/v1/baselines/{id}", h.DeleteBaseline).Methods("DELETE")
}

type createBaselineRequest struct {
	Name string `json:"name"`
}

// CreateBaseline captures a baseline snapshot for a project.
func (h *Handler) CreateBaseline(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]

	if !h.requireProjectRole(w, r, projectID, members.RoleEditor) {
		return
	}

	var req createBaselineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeJSONError(w, http.StatusBadRequest, invalidRequestBody)
		return
	}

	baseline, _, ok := h.captureBaseline(w, r, projectID, req.Name, false)
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, baseline)
}

// ListBaselines returns baselines for a project.
func (h *Handler) ListBaselines(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]

	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}

	baselines, err := h.BaselineService.ListBaselines(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list baselines", err)
		return
	}

	h.withPlanMoments(projectID, baselines)
	writeJSONOK(w, baselines)
}

// withPlanMoments puts on each baseline where the plan stood when it was
// taken, for the ones that recorded it. A plan that cannot be read leaves
// the baselines as they are.
func (h *Handler) withPlanMoments(projectID string, list []*baselines.Baseline) {
	if h.PlanService == nil || len(list) == 0 {
		return
	}
	moments, err := h.PlanService.BaselineMoments(projectID)
	if err != nil {
		return
	}
	for _, b := range list {
		if m := moments[b.ID]; m != nil {
			b.Plan, _ = json.Marshal(m)
		}
	}
}

// GetBaseline returns the snapshot JSON for a baseline.
func (h *Handler) GetBaseline(w http.ResponseWriter, r *http.Request) {
	baselineID := mux.Vars(r)["id"]

	baseline, err := h.BaselineService.GetBaseline(baselineID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "baseline not found", err)
		return
	}

	if !h.requireProjectRoleFor(w, r, baseline.ProjectID, members.RoleViewer, missing("baseline not found")) {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(baseline.Snapshot)
}

// DeleteBaseline deletes a baseline by ID: the project's owner alone, and the
// delete is published as baseline.deleted.
func (h *Handler) DeleteBaseline(w http.ResponseWriter, r *http.Request) {
	baselineID := mux.Vars(r)["id"]

	baseline, err := h.BaselineService.GetBaseline(baselineID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "baseline not found", err)
		return
	}
	if !h.requireProjectRoleFor(w, r, baseline.ProjectID, members.RoleOwner, missing("baseline not found")) {
		return
	}

	if err := h.BaselineService.DeleteBaseline(baselineID); err != nil {
		respondInternal(w, r, "failed to delete baseline", err)
		return
	}

	// A baseline is a project's record; its deletion is recorded in turn
	// (REQ-5), under the name it had, since nothing else keeps it.
	h.publish(r, events.BaselineDeleted, baseline.ProjectID, baseline.ID, map[string]interface{}{
		"name": baseline.Name,
	})

	w.WriteHeader(http.StatusNoContent)
}
