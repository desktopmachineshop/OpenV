// The limits enforcer: what a workspace has used of its limits, and whether
// an action would take it past one.

package orgs

import (
	"errors"
	"time"
)

// LimitEnforcer counts what each limit counts and decides each limit check,
// through the reads it is given. A refusal is a *LimitError. A read it could
// not make is returned as that read's error, never as a refusal: what to do
// about it is the caller's to decide. The API does not refuse on one (a
// limit check must never be the reason a legitimate action fails), and the
// billing path's seat count passes it on.
//
// It resolves a workspace's limits through Org.EffectiveLimits, so under the
// package default DeploymentPolicy as it stands at the call; it holds no copy
// of it. A port left nil is a read this deployment does not make, as each
// port says.
type LimitEnforcer struct {
	// Orgs reads workspaces and their members. Nil: no workspace is read,
	// so no check refuses anything.
	Orgs WorkspaceReads
	// PendingInvitations counts a workspace's invitations not yet accepted.
	// Nil: a workspace's seats are its members alone.
	PendingInvitations func(orgID string) (int, error)
	// Projects counts a workspace's projects. Nil: the project limit is
	// neither enforced nor measured.
	Projects func(orgID string) (int, error)
	// RunnerMinutes reads the cloud-runner minutes a workspace has leased
	// since a time. Nil, on a deployment with no runner pool: the monthly
	// allowance is neither enforced nor measured.
	RunnerMinutes func(orgID string, since time.Time) (int, error)
	// EvidenceBytes reads the size of a workspace's uploaded test evidence,
	// in bytes. Nil: evidence storage is not measured.
	EvidenceBytes func(orgID string) (int64, error)
}

// WorkspaceReads are the workspace reads a LimitEnforcer makes. Service
// satisfies it.
type WorkspaceReads interface {
	Get(id string) (*Org, error)
	ListMembers(orgID string) ([]*Member, error)
	ListForUser(userID string) ([]*Org, error)
}

// ErrNotMeasured is LimitEnforcer.Used's answer for a limit this deployment
// cannot measure.
var ErrNotMeasured = errors.New("this limit is not measured on this deployment")

// Org reads the workspace a limit check applies to. It answers nil and no
// error when there is no workspace to read (no workspace reads, or no id),
// and the read's error when the read fails.
func (e *LimitEnforcer) Org(orgID string) (*Org, error) {
	if e.Orgs == nil || orgID == "" {
		return nil, nil
	}
	return e.Orgs.Get(orgID)
}

// effectiveLimits resolves a workspace's limits, or nil when there is no
// workspace to read. Nil limits mean nothing is enforced.
func (e *LimitEnforcer) effectiveLimits(orgID string) (map[string]interface{}, error) {
	org, err := e.Org(orgID)
	if err != nil || org == nil {
		return nil, err
	}
	return org.EffectiveLimits(), nil
}

// CheckFlag refuses when the workspace's plan does not include a flag.
func (e *LimitEnforcer) CheckFlag(orgID, key string) error {
	limits, err := e.effectiveLimits(orgID)
	if err != nil || limits == nil {
		return err
	}
	return CheckFlag(limits, key)
}

// OverPlan names the count limits a workspace is already past. It counts
// only where a ceiling applies, so a workspace on the alpha terms, a paid
// plan or a self-hosted deployment costs no counting. A count it cannot
// read is no usage reading, so that limit is not named; the error joins
// those reads' errors.
func (e *LimitEnforcer) OverPlan(org *Org) ([]string, error) {
	if org == nil || org.OrgType == TypePersonal {
		return nil, nil
	}
	limits := org.EffectiveLimits()
	usage := map[string]int{}
	var unread []error
	if _, capped := Ceiling(limits, LimitMaxMembers); capped {
		if n, err := e.CountSeats(org.ID); err == nil {
			usage[LimitMaxMembers] = n
		} else {
			unread = append(unread, err)
		}
	}
	if _, capped := Ceiling(limits, LimitMaxProjects); capped && e.Projects != nil {
		if n, err := e.Projects(org.ID); err == nil {
			usage[LimitMaxProjects] = n
		} else {
			unread = append(unread, err)
		}
	}
	return OverPlan(limits, usage), errors.Join(unread...)
}

// monthStart is the first instant, in UTC, of the calendar month now falls
// in: where a month's cloud-runner minutes are counted from.
func monthStart(now time.Time) time.Time {
	now = now.UTC()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// hostedMinutes reads the workspace's monthly cloud-runner allowance and
// what it has used. capped is false where no ceiling applies, and nothing
// is then enforced.
func (e *LimitEnforcer) hostedMinutes(orgID string) (used, allowance int, capped bool, err error) {
	limits, err := e.effectiveLimits(orgID)
	if err != nil {
		return 0, 0, false, err
	}
	allowance, capped = Ceiling(limits, LimitHostedRunnerMinutesMonth)
	if !capped || e.RunnerMinutes == nil {
		return 0, 0, false, nil
	}
	used, err = e.RunnerMinutes(orgID, monthStart(time.Now()))
	if err != nil {
		return 0, 0, false, err
	}
	return used, allowance, true, nil
}

// LeaseMinutesAllowed fits a lease of `want` minutes under the month's
// remaining allowance: the whole lease where there is room, a shorter one
// where only part of it fits, and a limit refusal once nothing does. The
// allowance is hard: a lease never runs past it.
func (e *LimitEnforcer) LeaseMinutesAllowed(orgID string, want int) (int, error) {
	used, allowance, capped, err := e.hostedMinutes(orgID)
	if err != nil {
		return 0, err
	}
	if !capped {
		return want, nil
	}
	left := allowance - used
	if left <= 0 {
		return 0, NewLimitError(LimitHostedRunnerMinutesMonth, used, allowance).
			WithDetail("agents on your own machine through the Agent Connector are never counted")
	}
	if want > left {
		return left, nil
	}
	return want, nil
}

// CountSeats counts the people a workspace's member limit applies to:
// current members plus invitations still waiting to be accepted.
//
// Pending invitations count deliberately. Without that an admin could issue
// fifty invitations against five seats, and the refusal would land on the
// sixth person to click their link rather than on the admin who caused it —
// the error arriving for somebody who cannot act on it.
func (e *LimitEnforcer) CountSeats(orgID string) (int, error) {
	members, err := e.Orgs.ListMembers(orgID)
	if err != nil {
		return 0, err
	}
	seats := len(members)
	if e.PendingInvitations != nil {
		pending, err := e.PendingInvitations(orgID)
		if err != nil {
			return 0, err
		}
		seats += pending
	}
	return seats, nil
}

// CheckSeats refuses when the workspace has no room for `adding` more
// people. A nil error means there is room, or that no limit applies.
func (e *LimitEnforcer) CheckSeats(orgID string, adding int) error {
	org, err := e.Org(orgID)
	if err != nil || org == nil {
		return err
	}
	if org.OrgType == TypePersonal {
		// A personal workspace's single seat is enforced by AddMember and
		// the invitation path, which refuse more specifically than a seat
		// count can and say so as a 400. Refusing here too would only
		// replace that with a vaguer 403 about a ceiling nothing raises.
		// The limit is still reported, so the panel says "1 of 1" rather
		// than claiming a workspace nobody can join has no limit at all.
		return nil
	}
	limits := org.EffectiveLimits()
	if _, capped := Ceiling(limits, LimitMaxMembers); !capped {
		return nil
	}
	seats, err := e.CountSeats(orgID)
	if err != nil {
		return err
	}
	if err := CheckCeiling(limits, LimitMaxMembers, seats, adding); err != nil {
		var limitErr *LimitError
		if errors.As(err, &limitErr) {
			return limitErr.WithDetail("including invitations not yet accepted")
		}
		return err
	}
	return nil
}

// CheckProjectCount refuses when a workspace is already holding as many
// projects as it may.
func (e *LimitEnforcer) CheckProjectCount(orgID string) error {
	limits, err := e.effectiveLimits(orgID)
	if err != nil {
		return err
	}
	if _, capped := Ceiling(limits, LimitMaxProjects); !capped {
		return nil
	}
	if e.Projects == nil {
		return nil
	}
	held, err := e.Projects(orgID)
	if err != nil {
		return err
	}
	return CheckCeiling(limits, LimitMaxProjects, held, 1)
}

// sharedCreatedBy reads the workspaces a person belongs to (list) for the
// shared-workspace limit: how many shared ones userID created, and the
// personal one among them, nil when there is none.
func sharedCreatedBy(list []*Org, userID string) (created int, personal *Org) {
	for _, org := range list {
		if org.OrgType == TypePersonal {
			personal = org
			continue
		}
		if org.CreatedBy != nil && *org.CreatedBy == userID {
			created++
		}
	}
	return created, personal
}

// CheckSharedWorkspaceCount refuses creating another shared workspace once
// the person has created as many as their own plan allows.
//
// The count is of workspaces the person CREATED, not ones they belong to,
// and the ceiling is their personal workspace's — the one that is theirs to
// upgrade. Counting memberships instead would refuse somebody who did
// nothing but accept an invitation, and taking the most generous workspace
// they belong to would let one Business membership mint unlimited free
// workspaces for everybody in it. A person with no personal workspace (a
// service account, say) is not limited here.
func (e *LimitEnforcer) CheckSharedWorkspaceCount(userID string) error {
	if e.Orgs == nil || userID == "" {
		return nil
	}
	list, err := e.Orgs.ListForUser(userID)
	if err != nil {
		return err
	}
	created, personal := sharedCreatedBy(list, userID)
	if personal == nil {
		return nil
	}
	return CheckCeiling(personal.EffectiveLimits(), LimitMaxSharedWorkspaces, created, 1)
}

// Used measures one limit's current usage in workspace org. It answers
// ErrNotMeasured for a limit this deployment cannot measure, and a read's
// error when the read fails.
func (e *LimitEnforcer) Used(key string, org *Org) (int, error) {
	switch key {
	case LimitMaxMembers:
		return e.CountSeats(org.ID)
	case LimitMaxProjects:
		if e.Projects == nil {
			return 0, ErrNotMeasured
		}
		return e.Projects(org.ID)
	case LimitMaxSharedWorkspaces:
		// The same reading the creation check makes: workspaces this
		// person created, not ones they were invited into.
		if org.CreatedBy == nil {
			return 0, ErrNotMeasured
		}
		list, err := e.Orgs.ListForUser(*org.CreatedBy)
		if err != nil {
			return 0, err
		}
		created, _ := sharedCreatedBy(list, *org.CreatedBy)
		return created, nil
	case LimitHostedRunnerMinutesMonth:
		if e.RunnerMinutes == nil {
			return 0, ErrNotMeasured
		}
		return e.RunnerMinutes(org.ID, monthStart(time.Now()))
	case LimitEvidenceStorageMB:
		if e.EvidenceBytes == nil {
			return 0, ErrNotMeasured
		}
		bytes, err := e.EvidenceBytes(org.ID)
		if err != nil {
			return 0, err
		}
		// Reported in the limit's own unit so the two numbers compare.
		return int(bytes / (1024 * 1024)), nil
	}
	return 0, ErrNotMeasured
}
