package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// Enforcing workspace limits.
//
// Every count limit is read through one place so that "unlimited", the
// off-by-one, and the wording of the refusal cannot drift apart between the
// three handlers that enforce them. That place is orgs.LimitEnforcer, which
// counts and decides; this file answers for the API: it fails open where
// the enforcer could not read, and holds the read-only gate, its
// exemptions and the limits endpoint's response.
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
	// Kind is the value's shape: a flag has no number, only Included.
	Kind orgs.Kind `json:"kind"`
	// Included is a flag's reading: whether the plan includes the thing.
	Included *bool `json:"included,omitempty"`
}

// limitsResponse is the whole picture of what a workspace may do.
type limitsResponse struct {
	OrgID string `json:"org_id"`
	// Plan is the billed plan; EntitledPlan is the one the limits below
	// were resolved from, which differs once a subscription lapses.
	Plan         string `json:"plan"`
	EntitledPlan string `json:"entitled_plan"`
	// PlanStatus lets a member see that there is a payment problem — "ask
	// an admin" — without seeing anything about money.
	PlanStatus string `json:"plan_status"`
	// Grandfathered marks a workspace that keeps the alpha terms.
	Grandfathered bool `json:"grandfathered"`
	// SelfHosted tells the UI which remedy to offer when something is full.
	SelfHosted bool         `json:"self_hosted"`
	Limits     []LimitUsage `json:"limits"`
	// ReadOnly is true while the workspace holds more than its plan allows;
	// OverPlan names the limits it is past. Every write but the few that
	// alwaysWritable marks is refused with plan_read_only until it upgrades
	// or trims; reads and export never are.
	ReadOnly bool     `json:"read_only"`
	OverPlan []string `json:"over_plan,omitempty"`
}

// limitsEnforcer is the workspace limits enforcer over the handler's services
// as they are at the call: it counts and decides, and the methods below
// answer for the API, failing open where it could not read.
func (h *Handler) limitsEnforcer() *orgs.LimitEnforcer {
	e := &orgs.LimitEnforcer{Orgs: h.OrgService}
	if h.InvitationService != nil {
		e.PendingInvitations = func(orgID string) (int, error) {
			pending, err := h.InvitationService.ListPending(orgID)
			if err != nil {
				return 0, err
			}
			return len(pending), nil
		}
	}
	if h.ProjectService != nil {
		e.Projects = func(orgID string) (int, error) {
			list, err := h.ProjectService.ListProjectsByOrg(orgID)
			if err != nil {
				return 0, err
			}
			return len(list), nil
		}
	}
	if h.RunnerSessionService != nil {
		e.RunnerMinutes = h.RunnerSessionService.MinutesUsed
	}
	if h.EvidenceService != nil {
		e.EvidenceBytes = h.EvidenceService.StorageUsedByOrg
	}
	return e
}

// failOpen is how every limit check answers the API: a refusal stands, and
// anything else the enforcer returns, a workspace or a count it could not
// read, refuses nothing. A limit check must never be the reason a
// legitimate action fails, and refusing on a database hiccup would turn a
// transient fault into a billing message, which is the worst of both.
func failOpen(err error) error {
	var limitErr *orgs.LimitError
	if errors.As(err, &limitErr) {
		return err
	}
	return nil
}

// checkFlag refuses when the workspace's plan does not include a flag. A
// workspace that cannot be read is not refused, as with every limit.
func (h *Handler) checkFlag(orgID, key string) error {
	return failOpen(h.limitsEnforcer().CheckFlag(orgID, key))
}

// overPlan names the count limits a workspace is already past
// (LimitEnforcer.OverPlan). A count that cannot be read names nothing.
func (h *Handler) overPlan(org *orgs.Org) []string {
	over, _ := h.limitsEnforcer().OverPlan(org)
	return over
}

// ctxAlwaysWritable marks a request for one of the few writes a read-only
// workspace may still make: the ones that bring it back under its plan or
// out of the platform (remove a member, revoke an invitation, delete a
// project or the workspace), the billing endpoints, and import; three
// revocations, which only take access away (a worker key, one's own runner
// key, a share link); three writes that touch only the caller's own session
// or lease (make the workspace the session's active one, turn one's own
// stable preview on or off, end one's cloud runner lease); and a run's
// cancel, for whoever may cancel it.
const ctxAlwaysWritable contextKey = "openv-always-writable"

// alwaysWritable wraps a handler whose write is never refused for being
// over plan.
func (h *Handler) alwaysWritable(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		next(w, r.WithContext(context.WithValue(r.Context(), ctxAlwaysWritable, true)))
	}
}

func mutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// planGate is the plan read-only gate requireProjectRole and requireOrgRole
// end with, once the caller's access is decided: requireWritable on the
// workspace orgID names. orgID is called for a write only, so a project
// guard looks the project's workspace up for nothing else.
func (h *Handler) planGate(w http.ResponseWriter, r *http.Request, orgID func() string) bool {
	if !mutating(r.Method) {
		return true
	}
	return h.requireWritable(w, r, orgID())
}

// requireWritable refuses a mutating request scoped to a workspace that
// holds more than its plan allows, with 403 plan_read_only and the remedy.
// Reads pass untouched, as do the requests alwaysWritable marks. A
// workspace that cannot be read is writable: a limit check must never be
// the reason a legitimate action fails.
func (h *Handler) requireWritable(w http.ResponseWriter, r *http.Request, orgID string) bool {
	if orgID == "" || !mutating(r.Method) {
		return true
	}
	if on, _ := r.Context().Value(ctxAlwaysWritable).(bool); on {
		return true
	}
	over := h.overPlan(h.orgForLimits(orgID))
	if len(over) == 0 {
		return true
	}
	labels := make([]string, 0, len(over))
	for _, key := range over {
		if def, ok := orgs.Describe(key); ok {
			labels = append(labels, def.Label)
		} else {
			labels = append(labels, key)
		}
	}
	// A self-hosted deployment has no plan to be over: its limits are the
	// operator's (OPENV_LIMITS), as the remedy says.
	whose := "its plan's"
	if orgs.SelfHosted() {
		whose = "this deployment's"
	}
	remedy := orgs.ReadOnlyRemedy(over)
	msg := "This workspace is read-only: it is over " + whose + " limit on " + joinAnd(labels) + ". " + remedy
	respondJSON(w, http.StatusForbidden, map[string]interface{}{
		"error":  msg,
		"code":   ErrCodePlanReadOnly,
		"over":   over,
		"remedy": remedy,
	})
	return false
}

func joinAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	out := ""
	for i, item := range items {
		switch {
		case i == 0:
			out = item
		case i == len(items)-1:
			out += " and " + item
		default:
			out += ", " + item
		}
	}
	return out
}

// leaseMinutesAllowed fits a lease of `want` minutes under the month's
// remaining allowance (LimitEnforcer.LeaseMinutesAllowed). A month whose
// usage cannot be read is not enforced: the whole lease.
func (h *Handler) leaseMinutesAllowed(orgID string, want int) (int, error) {
	minutes, err := h.limitsEnforcer().LeaseMinutesAllowed(orgID, want)
	if err != nil && failOpen(err) == nil {
		return want, nil
	}
	return minutes, err
}

// orgForLimits reads the workspace a limit check applies to, or nil when it
// cannot be read. Callers that need more than the numbers — whether the
// workspace is a personal one, say — use this.
func (h *Handler) orgForLimits(orgID string) *orgs.Org {
	org, err := h.limitsEnforcer().Org(orgID)
	if err != nil {
		return nil
	}
	return org
}

// countOrgSeats counts the people a workspace's member limit applies to
// (LimitEnforcer.CountSeats), errors included: it is the billing path's
// seat count as well as the limit's.
func (h *Handler) countOrgSeats(orgID string) (int, error) {
	return h.limitsEnforcer().CountSeats(orgID)
}

// checkOrgSeats refuses when the workspace has no room for `adding` more
// people. A nil error means there is room, or that no limit applies.
func (h *Handler) checkOrgSeats(orgID string, adding int) error {
	return failOpen(h.limitsEnforcer().CheckSeats(orgID, adding))
}

// checkProjectCount refuses when a workspace is already holding as many
// projects as it may.
func (h *Handler) checkProjectCount(orgID string) error {
	return failOpen(h.limitsEnforcer().CheckProjectCount(orgID))
}

// checkSharedWorkspaceCount refuses creating another shared workspace once
// the person has created as many as their own plan allows
// (LimitEnforcer.CheckSharedWorkspaceCount).
func (h *Handler) checkSharedWorkspaceCount(userID string) error {
	return failOpen(h.limitsEnforcer().CheckSharedWorkspaceCount(userID))
}

// buildLimitsResponse renders every catalogued limit for one workspace, with
// usage where usage can be counted.
func (h *Handler) buildLimitsResponse(orgID string) (*limitsResponse, error) {
	org, err := h.OrgService.Get(orgID)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, orgs.ErrNotFound
	}
	limits := org.EffectiveLimits()

	out := &limitsResponse{
		OrgID:         orgID,
		Plan:          org.BilledPlan,
		EntitledPlan:  org.EntitledPlan(),
		PlanStatus:    org.Billing.Status,
		Grandfathered: org.Billing.Grandfathered,
		SelfHosted:    orgs.SelfHosted(),
	}
	if out.PlanStatus == "" {
		out.PlanStatus = orgs.PlanStatusNone
	}
	out.OverPlan = h.overPlan(org)
	out.ReadOnly = len(out.OverPlan) > 0
	for _, def := range orgs.Catalog() {
		if def.Kind == orgs.KindFlag {
			included := orgs.Allowed(limits, def.Key)
			out.Limits = append(out.Limits, LimitUsage{
				Key: def.Key, Label: def.Label, Description: def.Description, Unit: def.Unit,
				Kind: def.Kind, Included: &included, Unlimited: included,
			})
			continue
		}
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
			Kind:        def.Kind,
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

// countFor measures one limit's current usage (LimitEnforcer.Used). The
// second result is false for a limit this deployment cannot measure, or
// whose usage cannot be read, which the response then simply omits rather
// than reporting as zero.
func (h *Handler) countFor(key string, org *orgs.Org) (int, bool) {
	used, err := h.limitsEnforcer().Used(key, org)
	if err != nil {
		return 0, false
	}
	return used, true
}
