package notify

import (
	"fmt"
	"log/slog"
	"time"

	domainevents "github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// Budget alert thresholds, as whole-percent-of-budget marks. An alert fires
// the first time month-to-date spend crosses each mark, once per month.
const (
	budgetThresholdWarn = 80  // 80% of budget consumed
	budgetThresholdOver = 100 // budget exhausted (or exceeded)
)

// BudgetOrgService is the slice of orgs.Service the budget monitor needs:
// load an org (for its budget), list its members (to find admins), and claim
// the per-threshold-per-month alert dedupe.
type BudgetOrgService interface {
	Get(id string) (*orgs.Org, error)
	ListMembers(orgID string) ([]*orgs.Member, error)
	ClaimBudgetAlert(orgID, month string, threshold int) (bool, error)
}

// BudgetSpendReader reads an org's month-to-date spend; agentruns.Service
// satisfies it.
type BudgetSpendReader interface {
	MonthlySpend(orgID string, monthStart time.Time) (float64, error)
}

// BudgetMonitor turns run-finished events into workspace budget alerts
// (issue #186). When a finishing run's cost pushes an org's month-to-date
// spend across 80% or 100% of its monthly budget, it notifies the org's
// admins — exactly once per threshold per month, deduped by the atomic
// ClaimBudgetAlert. Cost is only known at finish, so this rides RunFinished
// (the same event the failed-run notifier consumes). Warn-only: it never
// blocks launches.
type BudgetMonitor struct {
	orgs        BudgetOrgService
	spend       BudgetSpendReader
	store       notifications.Service
	broadcaster Broadcaster
	// email is an optional best-effort email side channel (issue #187); nil
	// means email is off. Dispatch is nil-safe.
	email *EmailDispatcher
	// now is injectable so tests can pin the month; defaults to time.Now.
	now func() time.Time
}

// NewBudgetMonitor creates a budget monitor. broadcaster may be nil
// (store-only mode, used in tests).
func NewBudgetMonitor(orgSvc BudgetOrgService, spend BudgetSpendReader, store notifications.Service, broadcaster Broadcaster) *BudgetMonitor {
	return &BudgetMonitor{orgs: orgSvc, spend: spend, store: store, broadcaster: broadcaster, now: time.Now}
}

// SetEmailDispatcher attaches an email side channel. Passing nil (or never
// calling this) leaves email off.
func (m *BudgetMonitor) SetEmailDispatcher(d *EmailDispatcher) *BudgetMonitor {
	m.email = d
	return m
}

// Start subscribes to the bus.
func (m *BudgetMonitor) Start(bus domainevents.Bus) {
	bus.Subscribe(m.Handle)
}

// Handle evaluates one event. Exported so tests can drive it without a bus.
// Everything but a RunFinished event is ignored; any lookup error is logged
// and swallowed so a budget hiccup can never wedge bus dispatch.
func (m *BudgetMonitor) Handle(e domainevents.Event) {
	if e.EventType != domainevents.RunFinished {
		return
	}
	orgID := e.OrgID
	if orgID == "" {
		return
	}
	org, err := m.orgs.Get(orgID)
	if err != nil {
		slog.Error("budget: failed to load org", "org_id", orgID, "error", err)
		return
	}
	if org == nil || org.MonthlyBudgetUSD == nil || *org.MonthlyBudgetUSD <= 0 {
		return // no budget set — warn-only default is a no-op
	}
	budget := *org.MonthlyBudgetUSD

	now := m.now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	spend, err := m.spend.MonthlySpend(orgID, monthStart)
	if err != nil {
		slog.Error("budget: failed to read monthly spend", "org_id", orgID, "error", err)
		return
	}

	threshold := crossedThreshold(spend, budget)
	if threshold == 0 {
		return
	}

	month := now.Format("2006-01")
	claimed, err := m.orgs.ClaimBudgetAlert(orgID, month, threshold)
	if err != nil {
		slog.Error("budget: failed to claim alert", "org_id", orgID, "error", err)
		return
	}
	if !claimed {
		return // already alerted this threshold this month
	}

	m.alertAdmins(orgID, month, threshold, spend, budget)
}

// crossedThreshold returns the highest budget threshold spend has reached
// (100 or 80), or 0 when spend is under 80% of budget. budget is assumed > 0.
func crossedThreshold(spend, budget float64) int {
	switch {
	case spend >= budget:
		return budgetThresholdOver
	case spend >= budget*0.8:
		return budgetThresholdWarn
	default:
		return 0
	}
}

// alertAdmins stores (and live-pushes) one budget notification per org admin.
func (m *BudgetMonitor) alertAdmins(orgID, month string, threshold int, spend, budget float64) {
	members, err := m.orgs.ListMembers(orgID)
	if err != nil {
		slog.Error("budget: failed to list org members", "org_id", orgID, "error", err)
		return
	}
	title, body := budgetMessage(threshold, spend, budget)
	ref := map[string]interface{}{
		"kind":      "org_usage",
		"org_id":    orgID,
		"threshold": threshold,
		"month":     month,
	}
	for _, mem := range members {
		if mem.Role != orgs.RoleAdmin {
			continue
		}
		n := notifications.New(orgID, mem.UserID, notifications.TypeBudgetThreshold, title, body, ref)
		if err := m.store.Create(n); err != nil {
			slog.Error("budget: failed to store notification", "org_id", orgID, "user_id", mem.UserID, "error", err)
			continue
		}
		if m.broadcaster != nil {
			m.broadcaster.BroadcastSession(StreamKey(mem.UserID), "notification", n)
		}
		// Best-effort email side channel; no-op unless SMTP is configured and
		// the admin is opted in.
		m.email.Dispatch(n)
	}
}

// budgetMessage renders the title/body for a threshold alert.
func budgetMessage(threshold int, spend, budget float64) (string, string) {
	if threshold >= budgetThresholdOver {
		return "Workspace over budget",
			fmt.Sprintf("This month's agent spend has reached $%.2f of the $%.2f budget (100%%).", spend, budget)
	}
	return "Workspace nearing budget",
		fmt.Sprintf("This month's agent spend has reached $%.2f of the $%.2f budget (80%%).", spend, budget)
}
