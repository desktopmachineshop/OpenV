package api

import (
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/snapshot"
)

// DiffBaseline computes the changes from the baseline in the path ("base")
// to the snapshot named by ?against= ("target"): another baseline ID from
// the same project, or "live" for the current project state. Added/removed/
// modified are therefore expressed in the direction base → target, so
// comparing an old baseline against live reads as "what changed since the
// baseline was captured".
func (h *Handler) DiffBaseline(w http.ResponseWriter, r *http.Request) {
	baselineID := mux.Vars(r)["id"]

	baseline, err := h.BaselineService.GetBaseline(baselineID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "baseline not found", err)
		return
	}
	if !h.requireProjectRoleFor(w, r, baseline.ProjectID, members.RoleViewer, missing("baseline not found")) {
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

	// The base is the baseline read above, decoded without reading it again.
	base, _, err := snapshot.Load(baseline.ProjectID, baselineID, heldBaseline(baseline))
	if err != nil {
		respondInternal(w, r, "failed to parse baseline snapshot", err)
		return
	}

	targetID := against
	if against == "live" {
		targetID = ""
	}
	target, other, err := snapshot.Load(baseline.ProjectID, targetID, h.snapshotSources())
	if err != nil {
		var bad *snapshot.DecodeError
		switch decode := errors.As(err, &bad); {
		case against == "live" && decode:
			respondInternal(w, r, "failed to parse project export", err)
		case against == "live":
			respondInternal(w, r, "failed to export project", err)
		case decode:
			respondInternal(w, r, "failed to parse baseline snapshot", err)
		default:
			// A baseline from another project (or another org's project) is
			// indistinguishable from a missing one: 404 either way.
			respondError(w, r, http.StatusNotFound, "comparison baseline not found", err)
		}
		return
	}
	targetRef := baselines.SnapshotRef{ID: "live", Name: "Live Project"}
	if other != nil {
		targetRef = baselines.SnapshotRef{ID: other.ID, Name: other.Name}
	}

	result := baselines.Diff(base, target)
	result.Base = baselines.SnapshotRef{ID: baseline.ID, Name: baseline.Name}
	result.Target = targetRef

	writeJSONOK(w, result)
}
