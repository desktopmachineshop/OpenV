// The workspace's billing: a platform admin's plan move, the billing sync's
// writes and lookups, the monthly budget, and the budget and hosted-minutes
// alert claims.

package orgs

import "time"

// SetPlan implements Service.
func (s *DefaultService) SetPlan(id, plan string) (*Org, error) {
	if !ValidPlan(plan) {
		return nil, ErrInvalidPlan
	}
	org, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	// A grant and a subscription cannot both decide the plan. Moving a
	// workspace with a live subscription onto a granted plan would leave the
	// subscription billing for a tier the grant now overrides; the operator
	// cancels it in the billing provider first.
	if GrantedPlan(plan) && org.Billing.Live() {
		return nil, ErrBillingActive
	}
	if err := s.repo.SetPlan(id, plan); err != nil {
		return nil, err
	}
	org.ReleaseChannelOverride, org.BilledPlan = ChannelOverrideAfterMove(org.BilledPlan, plan, org.ReleaseChannelOverride), plan
	org.UpdatedAt = time.Now()
	org.ResolveReleaseChannel()
	return org, nil
}

// SetBillingCustomer implements Service.
func (s *DefaultService) SetBillingCustomer(orgID, customerRef, currency string) error {
	return s.repo.SetBillingCustomer(orgID, customerRef, currency)
}

// ApplyBillingState implements Service.
func (s *DefaultService) ApplyBillingState(orgID string, state BillingState) (bool, error) {
	return s.repo.ApplyBillingState(orgID, state)
}

// SetBilledSeats implements Service.
func (s *DefaultService) SetBilledSeats(orgID string, seats int) error {
	return s.repo.SetBilledSeats(orgID, seats)
}

// ClearBillingSubscription implements Service.
func (s *DefaultService) ClearBillingSubscription(orgID string) error {
	return s.repo.ClearBillingSubscription(orgID)
}

// SetGrandfathered implements Service.
func (s *DefaultService) SetGrandfathered(orgID string, on bool) error {
	return s.repo.SetGrandfathered(orgID, on)
}

// GrandfatherBefore implements Service: every workspace created before the
// announced date keeps the alpha terms (AlphaTerms), for good.
func (s *DefaultService) GrandfatherBefore(cutoff time.Time) (int64, error) {
	return s.repo.GrandfatherBefore(cutoff, AlphaTerms())
}

// FindOrgByBillingRef implements Service.
func (s *DefaultService) FindOrgByBillingRef(kind, ref string) (*Org, error) {
	return s.repo.FindOrgByBillingRef(kind, ref)
}

// ListBillingOrgs implements Service.
func (s *DefaultService) ListBillingOrgs(limit int) ([]*Org, error) {
	return s.repo.ListBillingOrgs(limit)
}

// ValidateMonthlyBudget is SetMonthlyBudget's refusal without its write:
// ErrInvalidBudget for a negative amount; nil clears the budget.
func ValidateMonthlyBudget(budget *float64) error {
	if budget != nil && *budget < 0 {
		return ErrInvalidBudget
	}
	return nil
}

// SetMonthlyBudget sets or clears (nil) the workspace's monthly spend budget.
// The write only touches monthly_budget_usd; the alert-dedupe columns are left
// to the atomic ClaimBudgetAlert path. A negative amount is rejected.
func (s *DefaultService) SetMonthlyBudget(id string, budget *float64) (*Org, error) {
	if err := ValidateMonthlyBudget(budget); err != nil {
		return nil, err
	}
	org, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SetBudget(id, budget); err != nil {
		return nil, err
	}
	org.MonthlyBudgetUSD = budget
	org.UpdatedAt = time.Now()
	return org, nil
}

// ClaimBudgetAlert delegates the atomic per-threshold-per-month dedupe claim
// to the repository (see Repository.ClaimBudgetAlert).
func (s *DefaultService) ClaimBudgetAlert(orgID, month string, threshold int) (bool, error) {
	return s.repo.ClaimBudgetAlert(orgID, month, threshold)
}

// ClaimMinutesAlert delegates the hosted-minutes alert dedupe claim.
func (s *DefaultService) ClaimMinutesAlert(orgID, month string, threshold int) (bool, error) {
	return s.repo.ClaimMinutesAlert(orgID, month, threshold)
}
