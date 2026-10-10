package api

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// registerWorkerKeyRoutes wires a workspace's worker keys (admin).
func (h *Handler) registerWorkerKeyRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/orgs/{id}/worker-keys", h.ListWorkerKeys).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/worker-keys", h.CreateWorkerKey).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/worker-keys/{keyId}", h.alwaysWritable(h.RevokeWorkerKey)).Methods("DELETE")
}

func (h *Handler) ListWorkerKeys(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	list, err := h.WorkerKeyService.List(orgID)
	if err != nil {
		respondInternal(w, r, "failed to list worker keys", err)
		return
	}
	writeJSONBare(w, list)
}

// CreateWorkerKey mints a key; the plaintext is returned once.
func (h *Handler) CreateWorkerKey(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	key, plaintext, err := h.WorkerKeyService.Create(orgID, req.Name, CurrentUserID(r), nil)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSONBareStatus(w, http.StatusCreated, map[string]interface{}{
		"key_record": key,
		"key":        plaintext,
	})
}

func (h *Handler) RevokeWorkerKey(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if !h.requireOrgRole(w, r, vars["id"], orgs.RoleAdmin) {
		return
	}
	if err := h.WorkerKeyService.Revoke(vars["id"], vars["keyId"]); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
