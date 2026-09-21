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
	"time"

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
	// NextStableRelease and NextStableAt name a stable release that is cut
	// but not yet turned on for the workspace, and when it will be.
	NextStableRelease string     `json:"next_stable_release,omitempty"`
	NextStableAt      *time.Time `json:"next_stable_at,omitempty"`
}

// resolveFeatures decides the stable release a member's gates resolve
// against, and the gates themselves.
func (h *Handler) resolveFeatures(org *orgs.Org, userID string) featuresResponse {
	resp := featuresResponse{Channel: org.ReleaseChannel, Features: map[string]bool{}}
	if org.ReleaseChannel == orgs.ChannelStable {
		resp.StableRelease = org.StableRelease
		if h.releaseService != nil {
			if s := h.releaseService.CurrentStable(); s != nil && release.Newer(s.Version, org.StableRelease) {
				resp.NextStableRelease = s.Version
				if cutOn, err := time.Parse("2006-01-02", s.Since); err == nil {
					at := orgs.UpgradeTimeFor(cutOn, org.UpgradeDay, org.UpgradeHour, org.UpgradeTimezone)
					resp.NextStableAt = &at
				}
			}
		}
		if on, err := h.orgService.MemberPreview(org.ID, userID); err == nil && on {
			resp.Preview = true
			if h.releaseService != nil {
				if s := h.releaseService.CurrentStable(); s != nil && release.Newer(s.Version, resp.StableRelease) {
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

// memberFeatureEnabled is featureEnabled for work done on a member's behalf
// outside their request — an agent run launched for them. A turn nobody
// launched (a hook re-firing a parked nudge) has no member to gate on and
// sees everything, as workers do.
func (h *Handler) memberFeatureEnabled(orgID string, userID *string, key string) bool {
	if userID == nil || h.orgService == nil {
		return true
	}
	org, err := h.orgService.Get(orgID)
	if err != nil || org == nil {
		return false
	}
	return h.resolveFeatures(org, *userID).Features[key]
}

// projectFeatureEnabled is featureEnabled for a project: the gate is the
// workspace's. A handler with no workspace service (a test, a stripped
// deployment) has nothing to gate on and lets the feature through.
func (h *Handler) projectFeatureEnabled(r *http.Request, projectID, key string) bool {
	if h.orgService == nil || h.projectService == nil {
		return true
	}
	project, err := h.projectService.GetProject(projectID)
	if err != nil || project == nil {
		return false
	}
	return h.featureEnabled(r, project.OrgID, key)
}

// featureGateMessage is the answer to a write that a workspace's channel
// has not received yet.
const featureGateMessage = "this feature reaches stable-channel workspaces at their next stable release; switch the workspace to nightly, or preview the next release, in workspace settings"

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
	if !orgs.ChannelChoosable(org.BilledPlan) {
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
