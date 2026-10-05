package notify

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// MinutesOrgService is the slice of orgs.Service the minutes monitor needs.
type MinutesOrgService interface {
	Get(id string) (*orgs.Org, error)
	ListMembers(orgID string) ([]*orgs.Member, error)
	ClaimMinutesAlert(orgID, month string, threshold int) (bool, error)
}

// MinutesReader reads a workspace's leased minutes since a moment;
// runnersessions.Service satisfies it.
type MinutesReader interface {
	MinutesUsed(orgID string, since time.Time) (int, error)
}

// MinutesMonitor turns cloud-runner leases into allowance alerts: when a
// workspace's leased minutes this month cross 80% or 100% of its
// hosted_runner_minutes_month limit, its admins are told, once per threshold
// per month, deduped by the atomic ClaimMinutesAlert. The lease handlers
// call Check after a lease starts or extends; a workspace with no ceiling
// is never alerted. Warn-only: the ceiling itself is enforced at the lease.
type MinutesMonitor struct {
	orgs     MinutesOrgService
	minutes  MinutesReader
	delivery Delivery
	now      func() time.Time
}

// NewMinutesMonitor creates a monitor. broadcaster may be nil.
func NewMinutesMonitor(orgSvc MinutesOrgService, minutes MinutesReader, store notifications.Service, broadcaster Broadcaster) *MinutesMonitor {
	return &MinutesMonitor{orgs: orgSvc, minutes: minutes, delivery: Delivery{Store: store, Broadcaster: broadcaster}, now: time.Now}
}

// SetChannels attaches the email and web push side channels at once; a nil
// one is left off.
func (m *MinutesMonitor) SetChannels(c Channels) *MinutesMonitor {
	m.delivery.Channels = c
	return m
}

// SetEmailDispatcher attaches an email side channel; nil leaves it off.
func (m *MinutesMonitor) SetEmailDispatcher(d *EmailDispatcher) *MinutesMonitor {
	m.delivery.Email = d
	return m
}

// SetPushDispatcher attaches a web push side channel; nil leaves it off.
func (m *MinutesMonitor) SetPushDispatcher(d *PushDispatcher) *MinutesMonitor {
	m.delivery.Push = d
	return m
}

// Check evaluates one workspace now. Safe on a nil monitor, so a handler
// can call it without knowing whether alerts are wired.
func (m *MinutesMonitor) Check(orgID string) {
	if m == nil || orgID == "" {
		return
	}
	org, err := m.orgs.Get(orgID)
	if err != nil || org == nil {
		return
	}
	allowance, capped := orgs.Ceiling(org.EffectiveLimits(), orgs.LimitHostedRunnerMinutesMonth)
	if !capped {
		return
	}
	now := m.now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	used, err := m.minutes.MinutesUsed(orgID, monthStart)
	if err != nil {
		slog.Error("minutes: failed to read leased minutes", "org_id", orgID, "error", err)
		return
	}
	threshold := crossedThreshold(float64(used), float64(allowance))
	if threshold == 0 {
		return
	}
	month := now.Format("2006-01")
	claimed, err := m.orgs.ClaimMinutesAlert(orgID, month, threshold)
	if err != nil {
		slog.Error("minutes: failed to claim alert", "org_id", orgID, "error", err)
		return
	}
	if !claimed {
		return
	}
	m.alertAdmins(orgID, month, threshold, used, allowance)
}

func (m *MinutesMonitor) alertAdmins(orgID, month string, threshold, used, allowance int) {
	members, err := m.orgs.ListMembers(orgID)
	if err != nil {
		slog.Error("minutes: failed to list org members", "org_id", orgID, "error", err)
		return
	}
	title, body := minutesMessage(threshold, used, allowance)
	ref := map[string]interface{}{
		"kind":      "org_limits",
		"org_id":    orgID,
		"threshold": threshold,
		"month":     month,
	}
	ToOrgAdmins(members, func(userID string) {
		n := notifications.New(orgID, userID, notifications.TypeHostedMinutes, title, body, ref)
		if err := m.delivery.Deliver(n); err != nil {
			slog.Error("minutes: failed to store notification", "org_id", orgID, "user_id", userID, "error", err)
		}
	})
}

func minutesMessage(threshold, used, allowance int) (string, string) {
	if threshold >= budgetThresholdOver {
		return "Cloud runner minutes used up",
			fmt.Sprintf("This month's leased cloud runner time has reached %d of the %d minutes the workspace's plan allows. "+
				"No more cloud runners can be leased until next month; agents on your own machine through the Agent Connector are not affected. "+
				"A workspace admin can raise the allowance from the Billing tab.", used, allowance)
	}
	return "Cloud runner minutes nearly used up",
		fmt.Sprintf("This month's leased cloud runner time has reached %d of the %d minutes the workspace's plan allows.", used, allowance)
}
