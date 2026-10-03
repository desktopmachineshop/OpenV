package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// GetMyRunnerKey returns the caller's personal runner key metadata (no
// plaintext) plus online state.
func (h *Handler) GetMyRunnerKey(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	user := CurrentUser(r)
	key, err := h.workerKeyService.PersonalKey(orgID, user.ID)
	if err != nil {
		respondInternal(w, r, "failed to load personal runner key", err)
		return
	}
	if key == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"key_record": nil, "online": false})
		return
	}
	online := key.LastUsedAt != nil && time.Since(*key.LastUsedAt) < 30*time.Second
	json.NewEncoder(w).Encode(map[string]interface{}{"key_record": key, "online": online})
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
	key, plaintext, err := h.workerKeyService.Create(orgID, name, &userID, &userID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
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
	key, err := h.workerKeyService.PersonalKey(orgID, user.ID)
	if err != nil {
		respondInternal(w, r, "failed to load personal runner key", err)
		return
	}
	if key == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.workerKeyService.Revoke(orgID, key.ID); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
