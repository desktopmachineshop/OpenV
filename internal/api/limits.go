package api

import (
	"errors"
	"net/http"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// Enforcing workspace limits.
//
// Every count limit is read through one place so that "unlimited", the
// off-by-one, and the wording of the refusal cannot drift apart between the
// three handlers that enforce them.
//
// A refusal is a 403 rather than a 402. Payment Required would be wrong on a
// self-hosted deployment, where there is no plan and nobody to pay — the
// operator simply has a setting to change — and the same code has to serve
// both deployments.

// LimitUsage is one limit rendered for a reader: what it is, what it allows,
// and how much of it is gone. It is what the limits endpoint returns and what
// the settings panel draws.
type LimitUsage struct {
	Key         string    `json:"key"`
	Label       string    `json:"label"`
	Description string    `json:"description"`
	Unit        orgs.Unit `json:"unit"`
	// Limit is the ceiling; 0 means unlimited.
	Limit int `json:"limit"`
	// Unlimited is stated rather than implied, so a client never has to know
	// that 0 is special.
	Unlimited bool `json:"unlimited"`
	// Used is the current count, present only for limits that can be
	// measured.
	Used *int `json:"used,omitempty"`
	// Fixed marks a ceiling nothing raises — no plan, no setting. A personal
	// workspace's single seat is the only one today. The panel reads it as
	// "this is how it is" rather than warning that the workspace is full,
	// which is the difference between a fact and an alarm.
	Fixed bool `json:"fixed,omitempty"`
}

// limitsResponse is the whole picture of what a workspace may do.
type limitsResponse struct {
	OrgID string `json:"org_id"`
	Plan  string `json:"plan"`
	// SelfHosted tells the UI which remedy to offer when something is full.
	SelfHosted bool         `json:"self_hosted"`
	Limits     []LimitUsage `json:"limits"`
}

// writeLimitError answers a limit refusal with the numbers and the remedy
// attached, so the frontend can show a person what stopped them and what to do
// without parsing prose.
func (h *Handler) writeLimitError(w http.ResponseWriter, err error) bool {
	var limitErr *orgs.LimitError
	if !errors.As(err, &limitErr) {
		return false
	}
	respondJSON(w, http.StatusForbidden, map[string]interface{}{
		"error":   limitErr.Error(),
		"code":    ErrCodeLimitReached,
		"limit":   limitErr.Key,
		"label":   limitErr.Label,
		"used":    limitErr.Used,
		"allowed": limitErr.Allowed,
		"remedy":  limitErr.Remedy(),
	})
	return true
}

// effectiveLimits resolves a workspace's limits, or nil when the workspace
// cannot be read. Nil limits mean nothing is enforced: a limit check must
// never be the reason a legitimate action fails.
func (h *Handler) effectiveLimits(orgID string) map[string]interface{} {
	org := h.orgForLimits(orgID)
	if org == nil {
		return nil
	}
	return org.EffectiveLimits()
}

// orgForLimits reads the workspace a limit check applies to, or nil when it
// cannot be read. Callers that need more than the numbers — whether the
// workspace is a personal one, say — use this rather than effectiveLimits.
func (h *Handler) orgForLimits(orgID string) *orgs.Org {
	if h.orgService == nil || orgID == "" {
		return nil
	}
	org, err := h.orgService.Get(orgID)
	if err != nil {
		return nil
	}
	return org
}

// countOrgSeats counts the people a workspace's member limit applies to:
// current members plus invitations still waiting to be accepted.
//
// Pending invitations count deliberately. Without that an admin could issue
// fifty invitations against five seats, and the refusal would land on the
// sixth person to click their link rather than on the admin who caused it —
// the error arriving for somebody who cannot act on it.
func (h *Handler) countOrgSeats(orgID string) (int, error) {
	members, err := h.orgService.ListMembers(orgID)
	if err != nil {
		return 0, err
	}
	seats := len(members)
	if h.invitationService != nil {
		pending, err := h.invitationService.ListPending(orgID)
		if err != nil {
			return 0, err
		}
		seats += len(pending)
	}
	return seats, nil
}

// checkOrgSeats refuses when the workspace has no room for `adding` more
// people. A nil error means there is room, or that no limit applies.
func (h *Handler) checkOrgSeats(orgID string, adding int) error {
	org := h.orgForLimits(orgID)
	if org == nil {
		return nil
	}
	if org.OrgType == orgs.TypePersonal {
		// A personal workspace's single seat is enforced in the domain, by
		// AddMember and the invitation path, which refuse more specifically
		// than a seat count can and say so as a 400. Refusing here too would
		// only replace that with a vaguer 403 about a ceiling nothing raises.
		// The limit is still reported, so the panel says "1 of 1" rather than
		// claiming a workspace nobody can join has no limit at all.
		return nil
	}
	limits := org.EffectiveLimits()
	if _, capped := orgs.Ceiling(limits, orgs.LimitMaxMembers); !capped {
		return nil
	}
	seats, err := h.countOrgSeats(orgID)
	if err != nil {
		// A limit that cannot be counted is not enforced. Refusing on a
		// database hiccup would turn a transient fault into a billing
		// message, which is the worst of both.
		return nil
	}
	if err := orgs.CheckCeiling(limits, orgs.LimitMaxMembers, seats, adding); err != nil {
		var limitErr *orgs.LimitError
		if errors.As(err, &limitErr) {
			return limitErr.WithDetail("including invitations not yet accepted")
		}
		return err
	}
	return nil
}

// checkProjectCount refuses when a workspace is already holding as many
// projects as it may.
func (h *Handler) checkProjectCount(orgID string) error {
	limits := h.effectiveLimits(orgID)
	if _, capped := orgs.Ceiling(limits, orgs.LimitMaxProjects); !capped {
		return nil
	}
	if h.projectService == nil {
		return nil
	}
	list, err := h.projectService.ListProjectsByOrg(orgID)
	if err != nil {
		return nil
	}
	return orgs.CheckCeiling(limits, orgs.LimitMaxProjects, len(list), 1)
}

// checkSharedWorkspaceCount refuses when this account has created as many
// shared workspaces as its limit allows.
//
// The account's own limits come from the workspaces it is already in: there is
// no per-user plan, so the most generous workspace the person belongs to
// decides. That is the forgiving reading, and the right one — somebody who has
// been given a seat in a paid workspace should not be held to the free tier's
// ceiling by their personal workspace.
func (h *Handler) checkSharedWorkspaceCount(userID string) error {
	if h.orgService == nil || userID == "" {
		return nil
	}
	list, err := h.orgService.ListForUser(userID)
	if err != nil {
		return nil
	}
	shared := 0
	best := map[string]interface{}(nil)
	bestCap := -1
	for _, org := range list {
		if org.OrgType != orgs.TypePersonal {
			shared++
		}
		limits := org.EffectiveLimits()
		cap, capped := orgs.Ceiling(limits, orgs.LimitMaxSharedWorkspaces)
		if !capped {
			// One unlimited workspace is enough to lift the ceiling.
			return nil
		}
		if cap > bestCap {
			bestCap, best = cap, limits
		}
	}
	if best == nil {
		return nil
	}
	return orgs.CheckCeiling(best, orgs.LimitMaxSharedWorkspaces, shared, 1)
}

// buildLimitsResponse renders every catalogued limit for one workspace, with
// usage where usage can be counted.
func (h *Handler) buildLimitsResponse(orgID string) (*limitsResponse, error) {
	org, err := h.orgService.Get(orgID)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, orgs.ErrNotFound
	}
	limits := org.EffectiveLimits()

	out := &limitsResponse{OrgID: orgID, Plan: org.Plan, SelfHosted: orgs.SelfHosted()}
	for _, def := range orgs.Catalog() {
		cap, capped := orgs.Ceiling(limits, def.Key)
		description, fixed := def.Description, false
		if def.Key == orgs.LimitMaxMembers && org.OrgType == orgs.TypePersonal {
			// Otherwise this reads as a ceiling somebody could raise, and the
			// panel shows a full bar with no explanation of why it can never
			// be anything else.
			description = "A personal workspace is only ever you. " +
				"Create a shared workspace to work with other people."
			fixed = true
		}
		usage := LimitUsage{
			Key:         def.Key,
			Label:       def.Label,
			Description: description,
			Unit:        def.Unit,
			Limit:       cap,
			Unlimited:   !capped,
			Fixed:       fixed,
		}
		if def.Countable {
			if used, ok := h.countFor(def.Key, org); ok {
				usage.Used = &used
			}
		}
		out.Limits = append(out.Limits, usage)
	}
	return out, nil
}

// countFor measures one limit's current usage. The second result is false for
// a limit this deployment cannot measure, which the response then simply
// omits rather than reporting as zero.
func (h *Handler) countFor(key string, org *orgs.Org) (int, bool) {
	switch key {
	case orgs.LimitMaxMembers:
		seats, err := h.countOrgSeats(org.ID)
		if err != nil {
			return 0, false
		}
		return seats, true
	case orgs.LimitMaxProjects:
		if h.projectService == nil {
			return 0, false
		}
		list, err := h.projectService.ListProjectsByOrg(org.ID)
		if err != nil {
			return 0, false
		}
		return len(list), true
	case orgs.LimitMaxSharedWorkspaces:
		if org.CreatedBy == nil {
			return 0, false
		}
		list, err := h.orgService.ListForUser(*org.CreatedBy)
		if err != nil {
			return 0, false
		}
		shared := 0
		for _, o := range list {
			if o.OrgType != orgs.TypePersonal {
				shared++
			}
		}
		return shared, true
	case orgs.LimitEvidenceStorageMB:
		if h.evidenceService == nil {
			return 0, false
		}
		bytes, err := h.evidenceService.StorageUsedByOrg(org.ID)
		if err != nil {
			return 0, false
		}
		// Reported in the limit's own unit so the two numbers compare.
		return int(bytes / (1024 * 1024)), true
	}
	return 0, false
}
