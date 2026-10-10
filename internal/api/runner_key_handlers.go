package api

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// registerRunnerKeyRoutes wires personal runner keys: every member manages
// their own.
func (h *Handler) registerRunnerKeyRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/orgs/{id}/my-runner-key", h.GetMyRunnerKey).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/my-runner-key", h.CreateMyRunnerKey).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/my-runner-key", h.alwaysWritable(h.RevokeMyRunnerKey)).Methods("DELETE")
}

// GetMyRunnerKey returns the caller's personal runner key metadata (no
// plaintext) plus online state.
func (h *Handler) GetMyRunnerKey(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	user := CurrentUser(r)
	key, err := h.WorkerKeyService.PersonalKey(orgID, user.ID)
	if err != nil {
		respondInternal(w, r, "failed to load personal runner key", err)
		return
	}
	if key == nil {
		writeJSONBare(w, map[string]interface{}{"key_record": nil, "online": false})
		return
	}
	online := key.LastUsedAt != nil && time.Since(*key.LastUsedAt) < workerOnlineWindow
	writeJSONBare(w, map[string]interface{}{"key_record": key, "online": online})
}

// CreateMyRunnerKey mints (or rotates) the caller's personal runner key.
func (h *Handler) CreateMyRunnerKey(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	user := CurrentUser(r)
	userID := user.ID
	name := "personal-runner"
	if user.Name != "" {
		name = user.Name + "'s runner"
	}
	key, plaintext, err := h.WorkerKeyService.Create(orgID, name, &userID, &userID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSONBareStatus(w, http.StatusCreated, map[string]interface{}{
		"key_record": key,
		"key":        plaintext,
	})
}

// RevokeMyRunnerKey revokes the caller's personal runner key.
func (h *Handler) RevokeMyRunnerKey(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	user := CurrentUser(r)
	key, err := h.WorkerKeyService.PersonalKey(orgID, user.ID)
	if err != nil {
		respondInternal(w, r, "failed to load personal runner key", err)
		return
	}
	if key == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.WorkerKeyService.Revoke(orgID, key.ID); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
