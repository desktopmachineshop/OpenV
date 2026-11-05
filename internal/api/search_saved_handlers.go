package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/domain/savedsearches"
)

// registerSavedSearchRoutes wires a member's saved searches (issue #722):
// the named chip sets a page's search bar puts back. Its call is the last in
// RegisterRoutes; no earlier template serves /me/saved-searches.
func (h *Handler) registerSavedSearchRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/me/saved-searches", h.ListSavedSearches).Methods("GET")
	router.HandleFunc("/api/v1/me/saved-searches", h.CreateSavedSearch).Methods("POST")
	router.HandleFunc("/api/v1/me/saved-searches/{id}", h.alwaysWritable(h.DeleteSavedSearch)).Methods("DELETE")
}

// savedSearchRequest is the body of POST /me/saved-searches.
type savedSearchRequest struct {
	Scope  string          `json:"scope"`
	Name   string          `json:"name"`
	Value  json.RawMessage `json:"value"`
	Shared bool            `json:"shared"`
}

// writeSavedSearches serializes a list, normalising nil to an empty array.
func writeSavedSearches(w http.ResponseWriter, list []*savedsearches.SavedSearch) {
	if list == nil {
		list = []*savedsearches.SavedSearch{}
	}
	writeJSONOK(w, map[string]interface{}{"saved_searches": list})
}

// ListSavedSearches answers GET /me/saved-searches?scope=: the caller's own
// searches in that scope in their active workspace, then those other members
// of the workspace shared. A caller with no active workspace has none.
func (h *Handler) ListSavedSearches(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireHumanUser(w, r)
	if !ok {
		return
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if !savedsearches.ValidScope(scope) {
		writeJSONError(w, http.StatusBadRequest, savedsearches.ErrScope.Error())
		return
	}
	org := ActiveOrg(r)
	if h.SavedSearchService == nil || org == "" {
		writeSavedSearches(w, nil)
		return
	}
	list, err := h.SavedSearchService.List(userID, org, scope)
	if err != nil {
		respondInternal(w, r, "failed to load saved searches", err)
		return
	}
	writeSavedSearches(w, list)
}

// CreateSavedSearch stores one of the caller's searches in their active
// workspace, answering 201 with the stored row. Saving under a name the
// caller already used in that scope replaces it, keeping its id. Gated on
// FeatureChipSearch: a workspace that has not received the search bar has
// nothing to save from.
func (h *Handler) CreateSavedSearch(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireHumanUser(w, r)
	if !ok {
		return
	}
	org := ActiveOrg(r)
	if org == "" {
		writeJSONError(w, http.StatusBadRequest, "no active workspace")
		return
	}
	if !h.featureEnabled(r, org, release.FeatureChipSearch) {
		writeJSONError(w, http.StatusForbidden, "saved searches are not available on this workspace's release channel yet")
		return
	}
	if h.SavedSearchService == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "saved searches are not available on this server")
		return
	}
	var req savedSearchRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	search, err := savedsearches.New(userID, org, req.Scope, req.Name, req.Value, req.Shared)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.SavedSearchService.Save(search); err != nil {
		if errors.Is(err, savedsearches.ErrTooMany) {
			writeJSONError(w, http.StatusConflict, err.Error())
			return
		}
		respondInternal(w, r, "failed to save the search", err)
		return
	}
	writeJSON(w, http.StatusCreated, search)
}

// DeleteSavedSearch removes one of the caller's own searches in their
// active workspace: 204, or 404 for an id that names none of theirs (another
// member's shared search answers as one that does not exist).
func (h *Handler) DeleteSavedSearch(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireHumanUser(w, r)
	if !ok {
		return
	}
	org := ActiveOrg(r)
	if h.SavedSearchService == nil || org == "" {
		writeJSONError(w, http.StatusNotFound, "saved search not found")
		return
	}
	n, err := h.SavedSearchService.Delete(userID, org, mux.Vars(r)["id"])
	if err != nil {
		respondInternal(w, r, "failed to delete the saved search", err)
		return
	}
	if n == 0 {
		writeJSONError(w, http.StatusNotFound, "saved search not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
