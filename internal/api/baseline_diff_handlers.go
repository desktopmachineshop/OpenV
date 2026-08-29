package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/members"
)

// DiffBaseline computes the changes from the baseline in the path ("base")
// to the snapshot named by ?against= ("target"): another baseline ID from
// the same project, or "live" for the current project state. Added/removed/
// modified are therefore expressed in the direction base → target, so
// comparing an old baseline against live reads as "what changed since the
// baseline was captured".
func (h *Handler) DiffBaseline(w http.ResponseWriter, r *http.Request) {
	baselineID := mux.Vars(r)["id"]

	baseline, err := h.baselineService.GetBaseline(baselineID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "baseline not found", err)
		return
	}
	if !h.requireProjectRole(w, r, baseline.ProjectID, members.RoleViewer) {
		return
	}

	against := r.URL.Query().Get("against")
	if against == "" {
		writeJSONError(w, http.StatusBadRequest, `against query parameter is required (a baseline id or "live")`)
		return
	}
	if against == baselineID {
		writeJSONError(w, http.StatusBadRequest, "cannot compare a baseline against itself")
		return
	}

	var base exports.ProjectExport
	if err := json.Unmarshal(baseline.Snapshot, &base); err != nil {
		respondInternal(w, r, "failed to parse baseline snapshot", err)
		return
	}

	var target exports.ProjectExport
	targetRef := baselines.SnapshotRef{ID: "live", Name: "Live Project"}
	if against == "live" {
		raw, _, err := h.exportService.ExportProject(baseline.ProjectID, exports.FormatJSON)
		if err != nil {
			respondInternal(w, r, "failed to export project", err)
			return
		}
		if err := json.Unmarshal(raw, &target); err != nil {
			respondInternal(w, r, "failed to parse project export", err)
			return
		}
	} else {
		// A baseline from another project (or another org's project) is
		// indistinguishable from a missing one: 404 either way.
		other, err := h.baselineService.GetProjectBaseline(baseline.ProjectID, against)
		if err != nil {
			respondError(w, r, http.StatusNotFound, "comparison baseline not found", err)
			return
		}
		if err := json.Unmarshal(other.Snapshot, &target); err != nil {
			respondInternal(w, r, "failed to parse baseline snapshot", err)
			return
		}
		targetRef = baselines.SnapshotRef{ID: other.ID, Name: other.Name}
	}

	result := baselines.Diff(&base, &target)
	result.Base = baselines.SnapshotRef{ID: baseline.ID, Name: baseline.Name}
	result.Target = targetRef

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
