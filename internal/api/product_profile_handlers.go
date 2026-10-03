package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/products"
)

// registerProductProfileRoutes wires reading and updating a project's
// product profile.
func (h *Handler) registerProductProfileRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/projects/{id}/profile", h.GetProductProfile).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/profile", h.UpdateProductProfile).Methods("PUT")
}

func (h *Handler) GetProductProfile(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	profile, err := h.productService.GetProfile(projectID)
	if err != nil {
		respondInternal(w, r, "failed to load product profile", err)
		return
	}
	json.NewEncoder(w).Encode(profile)
}

func (h *Handler) UpdateProductProfile(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleEditor) {
		return
	}
	var req products.UpdateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	profile, err := h.productService.UpdateProfile(projectID, req)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(profile)
}
