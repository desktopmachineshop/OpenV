package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/repoconns"
)

// registerRepoConnectionRoutes wires a project's repo connections and a
// member's own local path for one.
func (h *Handler) registerRepoConnectionRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/projects/{id}/repo-connections", h.ListRepoConnections).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/repo-connections", h.CreateRepoConnection).Methods("POST")
	router.HandleFunc("/api/v1/repo-connections/{id}", h.UpdateRepoConnection).Methods("PUT")
	router.HandleFunc("/api/v1/repo-connections/{id}", h.DeleteRepoConnection).Methods("DELETE")
	router.HandleFunc("/api/v1/repo-connections/{id}/my-path", h.SetMyRepoPath).Methods("PUT")
}

func (h *Handler) ListRepoConnections(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}

	// Personal runners get the claiming user's local paths: the checkout
	// lives somewhere different on every member's machine. The runner reads
	// a claimed run's connections with the run's token (the guard keeps it
	// to the run's own project, read only), which carries the member whose
	// personal runner key claimed it; a run a workspace key holds has none.
	claimant := WorkerUser(r)
	if run := CurrentRun(r); run != nil && run.ClaimedBy != nil {
		claimant = *run.ClaimedBy
	}
	if claimant != "" {
		list, err := h.RepoConnService.ListByProjectForUser(projectID, claimant)
		if err != nil {
			respondInternal(w, r, "failed to list repo connections", err)
			return
		}
		json.NewEncoder(w).Encode(list)
		return
	}

	// Users see the project's connections plus their own my_local_path.
	if user := CurrentUser(r); user != nil {
		list, err := h.RepoConnService.ListByProjectForUser(projectID, user.ID)
		if err != nil {
			respondInternal(w, r, "failed to list repo connections", err)
			return
		}
		json.NewEncoder(w).Encode(list)
		return
	}

	list, err := h.RepoConnService.ListByProject(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list repo connections", err)
		return
	}
	json.NewEncoder(w).Encode(list)
}

// SetMyRepoPath stores the caller's per-user local path for a repo
// connection (empty local_path clears it). Any project member may set their
// own path — it only affects runs on their own machine.
func (h *Handler) SetMyRepoPath(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	id := mux.Vars(r)["id"]
	conn, err := h.RepoConnService.Get(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "repo connection not found", err)
		return
	}
	if !h.requireProjectRoleFor(w, r, conn.ProjectID, members.RoleViewer, missing("repo connection not found")) {
		return
	}
	var req struct {
		LocalPath string `json:"local_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user := CurrentUser(r)
	if err := h.RepoConnService.SetMyPath(user.ID, id, strings.TrimSpace(req.LocalPath)); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	conn.MyLocalPath = strings.TrimSpace(req.LocalPath)
	json.NewEncoder(w).Encode(conn)
}

func (h *Handler) CreateRepoConnection(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleOwner) {
		return
	}
	var req repoconns.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.ProjectID = projectID
	conn, err := h.RepoConnService.Create(req)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(conn)
}

func (h *Handler) UpdateRepoConnection(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	conn, err := h.RepoConnService.Get(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "repo connection not found", err)
		return
	}
	if !h.requireProjectRoleFor(w, r, conn.ProjectID, members.RoleOwner, missing("repo connection not found")) {
		return
	}
	var req repoconns.UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updated, err := h.RepoConnService.Update(id, req)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(updated)
}

func (h *Handler) DeleteRepoConnection(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	conn, err := h.RepoConnService.Get(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "repo connection not found", err)
		return
	}
	if !h.requireProjectRoleFor(w, r, conn.ProjectID, members.RoleOwner, missing("repo connection not found")) {
		return
	}
	if err := h.RepoConnService.Delete(id); err != nil {
		respondInternal(w, r, "failed to delete repo connection", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
