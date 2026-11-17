package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/domain/targetreleases"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// registerTargetReleaseRoutes serves a project's target releases ("MVP",
// "V1.0"): the list and its edits, and each release's progress against the
// requirements it targets (internal/domain/targetreleases).
func (h *Handler) registerTargetReleaseRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/projects/{id}/releases", h.ListTargetReleases).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/releases", h.CreateTargetRelease).Methods("POST")
	router.HandleFunc("/api/v1/projects/{id}/release-order", h.ReorderTargetReleases).Methods("PUT")
	router.HandleFunc("/api/v1/projects/{id}/releases/{releaseId}", h.UpdateTargetRelease).Methods("PUT")
	router.HandleFunc("/api/v1/projects/{id}/releases/{releaseId}", h.DeleteTargetRelease).Methods("DELETE")
	router.HandleFunc("/api/v1/projects/{id}/vv/releases", h.GetReleaseOverview).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/vv/releases/{releaseId}", h.GetReleaseProgress).Methods("GET")
}

// writeTargetReleaseError answers a release write's domain error.
func writeTargetReleaseError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, targetreleases.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "release not found")
	case errors.Is(err, targetreleases.ErrNameTaken), errors.Is(err, targetreleases.ErrInUse):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, targetreleases.ErrNameRequired), errors.Is(err, targetreleases.ErrInvalidStatus),
		errors.Is(err, targetreleases.ErrInvalidDate), errors.Is(err, targetreleases.ErrReorder):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		respondInternal(w, r, "failed to save the release", err)
	}
}

// targetReleasesReady guards a release route: the service is wired and the
// caller holds role on the project. A write also needs the feature on for
// the project's workspace.
func (h *Handler) targetReleasesReady(w http.ResponseWriter, r *http.Request, projectID, role string, write bool) bool {
	if h.TargetReleaseService == nil {
		writeJSONError(w, http.StatusNotFound, "releases are not available")
		return false
	}
	if !h.requireProjectRole(w, r, projectID, role) {
		return false
	}
	if write && !h.projectFeatureEnabled(r, projectID, release.FeatureTargetReleases) {
		writeJSONError(w, http.StatusForbidden, featureGateMessage)
		return false
	}
	return true
}

// projectRelease loads a release of the project, answering 404 for one
// that belongs to another project as for one no row has (I3).
func (h *Handler) projectRelease(w http.ResponseWriter, r *http.Request, projectID string) (*targetreleases.Release, bool) {
	rel, err := h.TargetReleaseService.Get(mux.Vars(r)["releaseId"])
	if errors.Is(err, targetreleases.ErrNotFound) || (err == nil && rel.ProjectID != projectID) {
		writeJSONError(w, http.StatusNotFound, "release not found")
		return nil, false
	}
	if err != nil {
		respondInternal(w, r, "failed to load the release", err)
		return nil, false
	}
	return rel, true
}

// ListTargetReleases lists the project's releases in order.
func (h *Handler) ListTargetReleases(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.targetReleasesReady(w, r, projectID, members.RoleViewer, false) {
		return
	}
	list, err := h.TargetReleaseService.List(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list releases", err)
		return
	}
	writeJSONOK(w, list)
}

// CreateTargetRelease adds a release at the end of the order.
func (h *Handler) CreateTargetRelease(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.targetReleasesReady(w, r, projectID, members.RoleEditor, true) {
		return
	}
	var req targetreleases.CreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	rel, err := h.TargetReleaseService.Create(projectID, req)
	if err != nil {
		writeTargetReleaseError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, rel)
}

// UpdateTargetRelease changes a release's name, description, date or status.
func (h *Handler) UpdateTargetRelease(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.targetReleasesReady(w, r, projectID, members.RoleEditor, true) {
		return
	}
	rel, ok := h.projectRelease(w, r, projectID)
	if !ok {
		return
	}
	var req targetreleases.UpdateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	updated, err := h.TargetReleaseService.Update(rel.ID, req)
	if err != nil {
		writeTargetReleaseError(w, r, err)
		return
	}
	writeJSONOK(w, updated)
}

// DeleteTargetRelease removes a release no artifact targets (409 otherwise).
func (h *Handler) DeleteTargetRelease(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.targetReleasesReady(w, r, projectID, members.RoleEditor, true) {
		return
	}
	rel, ok := h.projectRelease(w, r, projectID)
	if !ok {
		return
	}
	if err := h.TargetReleaseService.Delete(rel.ID); err != nil {
		writeTargetReleaseError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ReorderTargetReleases sets the order: {"ids": [...]} naming every release.
func (h *Handler) ReorderTargetReleases(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.targetReleasesReady(w, r, projectID, members.RoleEditor, true) {
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	list, err := h.TargetReleaseService.Reorder(projectID, req.IDs)
	if err != nil {
		writeTargetReleaseError(w, r, err)
		return
	}
	writeJSONOK(w, list)
}

// releaseInputs loads what progress is computed from: the live project, its
// releases and its coverage with the flow-down from child projects, the
// same coverage GET /vv/coverage answers.
func (h *Handler) releaseInputs(w http.ResponseWriter, r *http.Request, projectID string) (targetreleases.Inputs, bool) {
	export, err := h.projectExport(projectID, "")
	if err != nil {
		respondInternal(w, r, "failed to export project", err)
		return targetreleases.Inputs{}, false
	}
	latest, err := h.VVService.LatestResults(projectID)
	if err != nil {
		respondInternal(w, r, "failed to load latest test results", err)
		return targetreleases.Inputs{}, false
	}
	list, err := h.TargetReleaseService.List(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list releases", err)
		return targetreleases.Inputs{}, false
	}
	return targetreleases.Inputs{
		Artifacts: export.Artifacts,
		Releases:  list,
		Coverage:  h.flowDownCoverage(export, latest),
	}, true
}

// GetReleaseOverview answers every release's summary and the requirements
// no release targets.
func (h *Handler) GetReleaseOverview(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.targetReleasesReady(w, r, projectID, members.RoleViewer, false) {
		return
	}
	in, ok := h.releaseInputs(w, r, projectID)
	if !ok {
		return
	}
	writeJSONOK(w, targetreleases.Overview(in))
}

// GetReleaseProgress answers one release's progress: its summary, each of
// its requirements, and the burn-up from the result history.
func (h *Handler) GetReleaseProgress(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.targetReleasesReady(w, r, projectID, members.RoleViewer, false) {
		return
	}
	rel, ok := h.projectRelease(w, r, projectID)
	if !ok {
		return
	}
	in, ok := h.releaseInputs(w, r, projectID)
	if !ok {
		return
	}
	history, err := h.resultHistory(projectID)
	if err != nil {
		respondInternal(w, r, "failed to load the test result history", err)
		return
	}
	writeJSONOK(w, targetreleases.ForRelease(in, rel, history, time.Now().UTC()))
}

// resultHistory is every result the project's in-progress and completed
// runs hold, superseded ones included: the runs coverage reads its latest
// results from, so a burn-up's last day agrees with the coverage.
func (h *Handler) resultHistory(projectID string) ([]*vv.TestResult, error) {
	runs, err := h.VVService.ListRuns(projectID)
	if err != nil {
		return nil, err
	}
	out := []*vv.TestResult{}
	for _, run := range runs {
		if run.Status != vv.RunStatusInProgress && run.Status != vv.RunStatusCompleted {
			continue
		}
		rs, err := h.VVService.ListResultHistory(run.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, rs...)
	}
	return out, nil
}
