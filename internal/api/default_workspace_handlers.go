package api

// The workspace a member's sign-in lands in (REQ-156).
//
// OpenV always opened in the personal workspace, which is right for someone
// working alone and wrong for almost everyone at a company, who signs in to
// work in the shared one. The choice is the member's own — keyed on the
// authenticated user, never settable for another account — and it is a
// choice, not a grant: the resolver re-checks membership every time it is
// used, so leaving a workspace quietly returns the sign-in to the personal
// one.

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/release"
)

func (h *Handler) registerDefaultWorkspaceRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/me/default-workspace", h.GetDefaultWorkspace).Methods("GET")
	router.HandleFunc("/api/v1/me/default-workspace", h.SetDefaultWorkspace).Methods("PUT")
}

// defaultWorkspace is the wire shape: the chosen workspace's id, or "" for
// the personal workspace.
type defaultWorkspace struct {
	OrgID string `json:"org_id"`
}

// GetDefaultWorkspace answers the caller's own choice.
func (h *Handler) GetDefaultWorkspace(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(defaultWorkspace{OrgID: user.DefaultOrgID})
}

// SetDefaultWorkspace records the caller's choice: a workspace they belong
// to, or "" to land in the personal one again. The gate is the chosen
// workspace's: a stable-channel workspace whose release predates the
// feature cannot yet be chosen, and the answer says so.
func (h *Handler) SetDefaultWorkspace(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req defaultWorkspace
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	orgID := strings.TrimSpace(req.OrgID)
	if orgID != "" {
		org, err := h.orgService.Get(orgID)
		if err != nil || org == nil {
			if err != nil && !errors.Is(err, orgs.ErrNotFound) {
				respondInternal(w, r, "failed to load workspace", err)
				return
			}
			writeJSONError(w, http.StatusNotFound, "workspace not found")
			return
		}
		if ok, err := h.orgService.IsMember(orgID, user.ID); err != nil || !ok {
			// Not a member: the same answer as an unknown workspace, so
			// the endpoint cannot be used to probe which workspaces exist.
			writeJSONError(w, http.StatusNotFound, "workspace not found")
			return
		}
		// The personal workspace is what "" means; storing its id would
		// only make the choice go stale if the workspace were ever renamed
		// or re-provisioned.
		if org.OrgType == orgs.TypePersonal {
			orgID = ""
		} else if !h.featureEnabled(r, orgID, release.FeatureDefaultWorkspace) {
			writeJSONError(w, http.StatusForbidden, featureGateMessage)
			return
		}
	}
	if err := h.userService.SetDefaultOrg(user.ID, orgID); err != nil {
		respondInternal(w, r, "failed to save the default workspace", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(defaultWorkspace{OrgID: orgID})
}
