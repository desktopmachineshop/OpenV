package api

// Feature gates and the stable-release preview (REQ-137, REQ-138).
//
// The shared service runs one build; what a workspace sees is decided here
// from its channel and the stable release it has turned on. A member of a
// company workspace may switch their own account to the next stable early.

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/help"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/release"
)

func (h *Handler) registerFeatureRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/orgs/{id}/features", h.GetOrgFeatures).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/members/me/preview", h.alwaysWritable(h.SetMyStablePreview)).Methods("PUT")
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
		if h.ReleaseService != nil {
			if s := h.ReleaseService.CurrentStable(); s != nil && release.Newer(s.Version, org.StableRelease) {
				resp.NextStableRelease = s.Version
				if cutOn, err := time.Parse("2006-01-02", s.Since); err == nil {
					at := orgs.UpgradeTimeFor(cutOn, org.UpgradeDay, org.UpgradeHour, org.UpgradeTimezone)
					resp.NextStableAt = &at
				}
			}
		}
		if on, err := h.OrgService.MemberPreview(org.ID, userID); err == nil && on {
			resp.Preview = true
			if h.ReleaseService != nil {
				if s := h.ReleaseService.CurrentStable(); s != nil && release.Newer(s.Version, resp.StableRelease) {
					resp.StableRelease = s.Version
				}
			}
		}
	}
	resp.Features = release.FeaturesFor(h.ReleaseService, org.ReleaseChannel, resp.StableRelease)
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
	org, err := h.OrgService.Get(orgID)
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
	if userID == nil || h.OrgService == nil {
		return true
	}
	org, err := h.OrgService.Get(orgID)
	if err != nil || org == nil {
		return false
	}
	return h.resolveFeatures(org, *userID).Features[key]
}

// projectFeatureEnabled is featureEnabled for a project: the gate is the
// workspace's. A handler with no workspace service (a test, a stripped
// deployment) has nothing to gate on and lets the feature through.
func (h *Handler) projectFeatureEnabled(r *http.Request, projectID, key string) bool {
	if h.OrgService == nil || h.ProjectService == nil {
		return true
	}
	project, err := h.ProjectService.GetProject(projectID)
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
	org, err := h.OrgService.Get(orgID)
	if err != nil || org == nil {
		writeJSONError(w, http.StatusNotFound, "workspace not found")
		return
	}
	writeJSONBare(w, h.resolveFeatures(org, CurrentUser(r).ID))
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
	if !decodeJSON(w, r, &req) {
		return
	}
	org, err := h.OrgService.Get(orgID)
	if err != nil || org == nil {
		writeJSONError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if !orgs.ChannelChoosable(org.ChannelPlan()) {
		writeJSONError(w, http.StatusBadRequest, orgs.ErrChannelLocked.Error())
		return
	}
	if err := h.OrgService.SetMemberPreview(orgID, CurrentUser(r).ID, req.Enabled); err != nil {
		if errors.Is(err, orgs.ErrNotMember) {
			writeJSONError(w, http.StatusForbidden, err.Error())
			return
		}
		respondInternal(w, r, "failed to update preview", err)
		return
	}
	writeJSONBare(w, h.resolveFeatures(org, CurrentUser(r).ID))
}

// In-app help from the user manual on OpenD (docs/plans/opend-help.md).
//
// The browser asks OpenV, never the docs site: these routes fetch the
// manual's topics for the workspace's profile — the docs line of the release
// its members are on, and its plan family — and answer them sanitized.
// Without OPENV_DOCS_URL and OPENV_DOCS_EMBED_SECRET every route answers
// 404 and the app keeps the help it ships.

func (h *Handler) registerHelpRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/orgs/{id}/help/context", h.GetHelpContext).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/help/topic", h.GetHelpTopic).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/help/search", h.SearchHelp).Methods("GET")
}

// helpProfile decides which docs the caller reads in workspace orgID, or
// answers the request itself and returns false.
func (h *Handler) helpProfile(w http.ResponseWriter, r *http.Request) (help.Profile, bool) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return help.Profile{}, false
	}
	// Off where no docs site is configured, and for a member whose channel
	// has not reached the feature yet: the panel then keeps its summary.
	if h.HelpService == nil || !h.featureEnabled(r, orgID, release.FeatureManualHelp) {
		writeJSONError(w, http.StatusNotFound, "help is not configured")
		return help.Profile{}, false
	}
	org, err := h.OrgService.Get(orgID)
	if err != nil || org == nil {
		writeJSONError(w, http.StatusNotFound, "workspace not found")
		return help.Profile{}, false
	}
	// The release whose features this member sees: the stable release on
	// the stable channel (the next one when they preview it), otherwise the
	// release this server runs.
	version := h.resolveFeatures(org, CurrentUser(r).ID).StableRelease
	if version == "" && h.ReleaseService != nil {
		if cur := h.ReleaseService.Current(); cur != nil {
			version = cur.Version
		}
	}
	line := help.Line(version)
	if line == "" {
		writeJSONError(w, http.StatusNotFound, "no release to show help for")
		return help.Profile{}, false
	}
	return help.Profile{Line: line, Plan: help.PlanFamily(org.BilledPlan)}, true
}

// writeHelpError answers a help failure: 404 when the manual has nothing
// for the request, 502 when the docs site could not answer.
func writeHelpError(w http.ResponseWriter, err error) {
	if errors.Is(err, help.ErrNotFound) {
		writeJSONError(w, http.StatusNotFound, "no help for this")
		return
	}
	writeJSONError(w, http.StatusBadGateway, "the help could not be loaded")
}

// GetHelpContext answers the topic for ?key= (a context key the page or a
// help link names) or, without one, for ?path= (the app path, which the
// manual's help-contexts.yaml maps to a key).
func (h *Handler) GetHelpContext(w http.ResponseWriter, r *http.Request) {
	profile, ok := h.helpProfile(w, r)
	if !ok {
		return
	}
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if key == "" && path == "" {
		writeJSONError(w, http.StatusBadRequest, "key or path is required")
		return
	}
	topic, err := h.HelpService.Context(r.Context(), profile, key, path)
	if err != nil {
		writeHelpError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSONBare(w, topic)
}

// GetHelpTopic answers one topic by its page id: ?id=topics/vv/test-runs.
func (h *Handler) GetHelpTopic(w http.ResponseWriter, r *http.Request) {
	profile, ok := h.helpProfile(w, r)
	if !ok {
		return
	}
	topic, err := h.HelpService.TopicByID(r.Context(), profile, r.URL.Query().Get("id"))
	if err != nil {
		writeHelpError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSONBare(w, topic)
}

// SearchHelp searches the topics the workspace may read: ?q=.
func (h *Handler) SearchHelp(w http.ResponseWriter, r *http.Request) {
	profile, ok := h.helpProfile(w, r)
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" || len(q) > 200 {
		writeJSONError(w, http.StatusBadRequest, "q must be 1 to 200 characters")
		return
	}
	results, err := h.HelpService.Search(r.Context(), profile, q)
	if err != nil {
		writeHelpError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSONBare(w, results)
}
