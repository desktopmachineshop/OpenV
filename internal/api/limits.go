package api

import (
	"context"
	"errors"
	"net/http"
	"time"

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
	// OverPlan names the limits it is past. Every write is refused with
	// plan_read_only until it upgrades or trims; reads and export never are.
	ReadOnly bool     `json:"read_only"`
	OverPlan []string `json:"over_plan,omitempty"`
}

// checkFlag refuses when the workspace's plan does not include a flag. A
// workspace that cannot be read is not refused, as with every limit.
func (h *Handler) checkFlag(orgID, key string) error {
	limits := h.effectiveLimits(orgID)
	if limits == nil {
		return nil
	}
	return orgs.CheckFlag(limits, key)
}

// overPlan names the count limits a workspace is already past. It counts
// only where a ceiling applies, so a workspace on the alpha terms, a paid
// plan or a self-hosted deployment costs one read and no counting.
func (h *Handler) overPlan(org *orgs.Org) []string {
	if org == nil || org.OrgType == orgs.TypePersonal {
		return nil
	}
	limits := org.EffectiveLimits()
	usage := map[string]int{}
	if _, capped := orgs.Ceiling(limits, orgs.LimitMaxMembers); capped {
		if n, err := h.countOrgSeats(org.ID); err == nil {
			usage[orgs.LimitMaxMembers] = n
		}
	}
	if _, capped := orgs.Ceiling(limits, orgs.LimitMaxProjects); capped && h.projectService != nil {
		if list, err := h.projectService.ListProjectsByOrg(org.ID); err == nil {
			usage[orgs.LimitMaxProjects] = len(list)
		}
	}
	return orgs.OverPlan(limits, usage)
}

// ctxAlwaysWritable marks a request for one of the few writes a read-only
// workspace may still make: the ones that bring it back under its plan or
// out of the platform (remove a member, revoke an invitation, delete a
// project or the workspace), the billing endpoints, and import.
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
	msg := "This workspace is read-only: it is over its plan's limit on " + joinAnd(labels) + ". " + orgs.ReadOnlyRemedy
	respondJSON(w, http.StatusForbidden, map[string]interface{}{
		"error":  msg,
		"code":   ErrCodePlanReadOnly,
		"over":   over,
		"remedy": orgs.ReadOnlyRemedy,
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

// hostedMinutes reads the workspace's monthly cloud-runner allowance and
// what it has used. capped is false where no ceiling applies or the usage
// cannot be read, and nothing is then enforced.
func (h *Handler) hostedMinutes(orgID string) (used, allowance int, capped bool) {
	limits := h.effectiveLimits(orgID)
	allowance, capped = orgs.Ceiling(limits, orgs.LimitHostedRunnerMinutesMonth)
	if !capped || h.runnerSessionService == nil {
		return 0, 0, false
	}
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	used, err := h.runnerSessionService.MinutesUsed(orgID, monthStart)
	if err != nil {
		return 0, 0, false
	}
	return used, allowance, true
}

// leaseMinutesAllowed fits a lease of `want` minutes under the month's
// remaining allowance: the whole lease where there is room, a shorter one
// where only part of it fits, and a limit refusal once nothing does. The
// allowance is hard: a lease never runs past it.
func (h *Handler) leaseMinutesAllowed(orgID string, want int) (int, error) {
	used, allowance, capped := h.hostedMinutes(orgID)
	if !capped {
		return want, nil
	}
	left := allowance - used
	if left <= 0 {
		return 0, orgs.NewLimitError(orgs.LimitHostedRunnerMinutesMonth, used, allowance).
			WithDetail("agents on your own machine through the Agent Connector are never counted")
	}
	if want > left {
		return left, nil
	}
	return want, nil
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

// checkSharedWorkspaceCount refuses creating another shared workspace once
// the person has created as many as their own plan allows.
//
// The count is of workspaces the person CREATED, not ones they belong to,
// and the ceiling is their personal workspace's — the one that is theirs to
// upgrade. Counting memberships instead would refuse somebody who did
// nothing but accept an invitation, and taking the most generous workspace
// they belong to would let one Business membership mint unlimited free
// workspaces for everybody in it. A person with no personal workspace (a
// service account, say) is not limited here.
func (h *Handler) checkSharedWorkspaceCount(userID string) error {
	if h.orgService == nil || userID == "" {
		return nil
	}
	list, err := h.orgService.ListForUser(userID)
	if err != nil {
		return nil
	}
	created := 0
	var personal *orgs.Org
	for _, org := range list {
		if org.OrgType == orgs.TypePersonal {
			personal = org
			continue
		}
		if org.CreatedBy != nil && *org.CreatedBy == userID {
			created++
		}
	}
	if personal == nil {
		return nil
	}
	return orgs.CheckCeiling(personal.EffectiveLimits(), orgs.LimitMaxSharedWorkspaces, created, 1)
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
		// The same reading the creation check makes: workspaces this
		// person created, not ones they were invited into.
		if org.CreatedBy == nil {
			return 0, false
		}
		list, err := h.orgService.ListForUser(*org.CreatedBy)
		if err != nil {
			return 0, false
		}
		created := 0
		for _, o := range list {
			if o.OrgType != orgs.TypePersonal && o.CreatedBy != nil && *o.CreatedBy == *org.CreatedBy {
				created++
			}
		}
		return created, true
	case orgs.LimitHostedRunnerMinutesMonth:
		if h.runnerSessionService == nil {
			return 0, false
		}
		now := time.Now().UTC()
		used, err := h.runnerSessionService.MinutesUsed(org.ID, time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC))
		if err != nil {
			return 0, false
		}
		return used, true
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
