package api

// Feature gates and the stable-release preview (REQ-137, REQ-138).
//
// The shared service runs one build; what a workspace sees is decided here
// from its channel and the stable release it has turned on. A member of a
// company workspace may switch their own account to the next stable early.

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/release"
)

func (h *Handler) registerFeatureRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/orgs/{id}/features", h.GetOrgFeatures).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/members/me/preview", h.SetMyStablePreview).Methods("PUT")
}

// featuresResponse is what a client needs to gate its own UI.
type featuresResponse struct {
	Channel string `json:"channel"`
	// StableRelease is the release the gates were resolved against: the
	// workspace's turned-on stable, or the newest stable when the caller
	// previews it. Empty on the nightly channel or before any stable exists.
	StableRelease string `json:"stable_release"`
	// Preview reports whether the caller has switched early.
	Preview  bool            `json:"preview"`
	Features map[string]bool `json:"features"`
}

// resolveFeatures decides the stable release a member's gates resolve
// against, and the gates themselves.
func (h *Handler) resolveFeatures(org *orgs.Org, userID string) featuresResponse {
	resp := featuresResponse{Channel: org.ReleaseChannel, Features: map[string]bool{}}
	if org.ReleaseChannel == orgs.ChannelStable {
		resp.StableRelease = org.StableRelease
		if on, err := h.orgService.MemberPreview(org.ID, userID); err == nil && on {
			resp.Preview = true
			if h.releaseService != nil {
				if s := h.releaseService.CurrentStable(); s != nil && release.StableNewer(s.Version, resp.StableRelease) {
					resp.StableRelease = s.Version
				}
			}
		}
	}
	resp.Features = release.FeaturesFor(h.releaseService, org.ReleaseChannel, resp.StableRelease)
	return resp
}

// featureEnabled is the server-side gate: whether feature key is on for the
// calling member of workspace orgID. Unknown keys are off. Requests with no
// session (workers, agent runs) see everything, since they act for a
// workspace whose members may be on either channel.
func (h *Handler) featureEnabled(r *http.Request, orgID, key string) bool {
	user := CurrentUser(r)
	if user == nil {
		return true
	}
	org, err := h.orgService.Get(orgID)
	if err != nil || org == nil {
		return false
	}
	return h.resolveFeatures(org, user.ID).Features[key]
}

// GetOrgFeatures answers the caller's gates in one workspace.
func (h *Handler) GetOrgFeatures(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	org, err := h.orgService.Get(orgID)
	if err != nil || org == nil {
		writeJSONError(w, http.StatusNotFound, "workspace not found")
		return
	}
	json.NewEncoder(w).Encode(h.resolveFeatures(org, CurrentUser(r).ID))
}

// SetMyStablePreview turns the next stable release on or off early for the
// caller alone in a company workspace: {"enabled": true|false}. A plan
// that always runs nightly has nothing to preview (400).
func (h *Handler) SetMyStablePreview(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	org, err := h.orgService.Get(orgID)
	if err != nil || org == nil {
		writeJSONError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if !orgs.ChannelChoosable(org.Plan) {
		writeJSONError(w, http.StatusBadRequest, orgs.ErrChannelLocked.Error())
		return
	}
	if err := h.orgService.SetMemberPreview(orgID, CurrentUser(r).ID, req.Enabled); err != nil {
		if errors.Is(err, orgs.ErrNotMember) {
			writeJSONError(w, http.StatusForbidden, err.Error())
			return
		}
		respondInternal(w, r, "failed to update preview", err)
		return
	}
	json.NewEncoder(w).Encode(h.resolveFeatures(org, CurrentUser(r).ID))
}
