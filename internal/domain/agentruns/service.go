// DefaultService, the run service: its constructor and the policies wired
// into it (retry, routing, budget), the link from a run to its kanban card,
// and the reads: a run, its tree and its logs, the queue and the usage
// rollups.

package agentruns

import (
	"time"

	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/tokens"
)

// DefaultService implements Service.
type DefaultService struct {
	repo         Repository
	agentService agents.Service
	bus          events.Bus
	subscribers  []Subscriber

	// Routing policy (optional): hasPersonalRunner reports whether the
	// launcher has an online personal runner; graceSeconds resolves the
	// org's reservation window.
	hasPersonalRunner func(orgID, userID string) bool
	graceSeconds      func(orgID string) int

	// Auto-retry policy: maxAttempts caps a run's attempt chain (original +
	// retries); autoRetry toggles the whole feature. A retryable terminal
	// failure re-enqueues a fresh attempt with backoff while attempts remain.
	maxAttempts int
	autoRetry   bool
	// budgetGuard (optional) soft-blocks a launch when the workspace is over
	// its monthly budget: it returns (true, reason) to reject. nil means no
	// enforcement (the warn-only default); wired only when budget enforcement
	// is enabled (issue #186).
	budgetGuard func(orgID string) (bool, string)
}

// NewDefaultService creates a run service with auto-retry enabled at the
// default attempt cap. Override with SetRetryPolicy.
func NewDefaultService(repo Repository, agentService agents.Service, bus events.Bus) *DefaultService {
	return &DefaultService{
		repo:         repo,
		agentService: agentService,
		bus:          bus,
		maxAttempts:  DefaultMaxAttempts,
		autoRetry:    true,
	}
}

// SetRetryPolicy configures bounded auto-retry (call during wiring only).
// maxAttempts <= 0 falls back to DefaultMaxAttempts; a maxAttempts of 1 (or
// autoRetry=false) disables auto-retry — the terminal failure simply stands.
func (s *DefaultService) SetRetryPolicy(maxAttempts int, autoRetry bool) {
	if maxAttempts <= 0 {
		maxAttempts = DefaultMaxAttempts
	}
	s.maxAttempts = maxAttempts
	s.autoRetry = autoRetry
}

// SetRoutingPolicy wires personal-runner first-refusal routing (call during
// wiring only).
func (s *DefaultService) SetRoutingPolicy(hasPersonalRunner func(orgID, userID string) bool, graceSeconds func(orgID string) int) {
	s.hasPersonalRunner = hasPersonalRunner
	s.graceSeconds = graceSeconds
}

// SetBudgetGuard wires the optional over-budget soft-block (call during wiring
// only). When set, Launch rejects a run whose workspace is over budget with
// ErrBudgetExceeded. Leaving it unset keeps the warn-only default.
func (s *DefaultService) SetBudgetGuard(guard func(orgID string) (bool, string)) {
	s.budgetGuard = guard
}

// AttachWorkItem links a run to the kanban card tracking it. The write is a
// targeted single-column update so it can never resurrect a status (or any
// other field) from a stale read.
func (s *DefaultService) AttachWorkItem(runID, workItemID string) error {
	return s.repo.UpdateWorkItemID(runID, workItemID)
}

// Get returns a run by id.
func (s *DefaultService) Get(id string) (*Run, error) {
	run, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, ErrNotFound
	}
	return run, nil
}

// GetByToken resolves a raw run token to its run.
func (s *DefaultService) GetByToken(token string) (*Run, error) {
	run, err := s.repo.FindByTokenHash(tokens.HashToken(token))
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, ErrNotFound
	}
	return run, nil
}

// List returns runs matching the filter.
func (s *DefaultService) List(filter ListFilter) ([]*Run, error) {
	return s.repo.List(filter)
}

// Tree returns the root run and all descendants (breadth-first).
func (s *DefaultService) Tree(rootID string) ([]*Run, error) {
	root, err := s.Get(rootID)
	if err != nil {
		return nil, err
	}
	result := []*Run{root}
	frontier := []string{rootID}
	for len(frontier) > 0 {
		next := []string{}
		for _, id := range frontier {
			children, err := s.repo.ListChildren(id)
			if err != nil {
				return nil, err
			}
			for _, c := range children {
				result = append(result, c)
				next = append(next, c.ID)
			}
		}
		frontier = next
	}
	return result, nil
}

// Logs returns persisted log entries after a sequence number.
func (s *DefaultService) Logs(runID string, afterSeq int) ([]LogEntry, error) {
	return s.repo.ListLogs(runID, afterSeq)
}

// CountRunsSince counts an automation's runs in a window (rate guard).
func (s *DefaultService) CountRunsSince(automationID string, since time.Time) (int, error) {
	return s.repo.CountRunsSince(automationID, since)
}

// QueueStats summarizes the org's queued runs.
func (s *DefaultService) QueueStats(orgID string) (QueueStats, error) {
	return s.repo.QueueStats(orgID)
}

// Usage rolls up the org's run usage since the given time. Totals are summed
// from the per-agent rollup (both groupings cover the same runs).
func (s *DefaultService) Usage(orgID string, since time.Time) (*UsageSummary, error) {
	byAgent, byDay, err := s.repo.Usage(orgID, since)
	if err != nil {
		return nil, err
	}
	summary := &UsageSummary{ByAgent: byAgent, ByDay: byDay}
	if summary.ByAgent == nil {
		summary.ByAgent = []AgentUsage{}
	}
	if summary.ByDay == nil {
		summary.ByDay = []DailyUsage{}
	}
	for _, a := range summary.ByAgent {
		summary.Totals.Runs += a.Runs
		summary.Totals.TokensIn += a.TokensIn
		summary.Totals.TokensOut += a.TokensOut
		summary.Totals.CostUSD += a.CostUSD
	}
	return summary, nil
}

// MonthlySpend returns the org's month-to-date spend since monthStart.
func (s *DefaultService) MonthlySpend(orgID string, monthStart time.Time) (float64, error) {
	return s.repo.MonthlySpend(orgID, monthStart)
}
