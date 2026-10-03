// OrgRepository's billing: the plan, the provider's subscription snapshot
// (migration 45), grandfathering, and the budget and hosted-minutes alert
// claims.

package postgres

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/lib/pq"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// SetBudget writes only monthly_budget_usd (a nil budget clears it), leaving
// the alert-dedupe columns untouched so this can never race the alert claim.
func (r *OrgRepository) SetBudget(orgID string, budget *float64) error {
	_, err := r.db.Exec(`
		UPDATE organizations SET monthly_budget_usd = $2, updated_at = NOW() WHERE id = $1
	`, orgID, budget)
	return err
}

// SetPlan writes plan (REQ-154), and the channel override keepNightlySQL writes.
func (r *OrgRepository) SetPlan(orgID, plan string) error {
	_, err := r.db.Exec(`UPDATE organizations SET plan = $2, `+fmt.Sprintf(keepNightlySQL, "$2", "$3")+`, updated_at = NOW() WHERE id = $1`, orgID, plan, pq.Array(orgs.ChoosablePlans))
	return err
}

// keepNightlySQL is orgs.ChannelOverrideAfterMove as the SET of an UPDATE, whose expressions see the row's OLD plan,
// formatted with the parameters of the new plan (cast to the plan column's type, as SetPlan also assigns it and
// Postgres deduces one type per parameter) and of orgs.ChoosablePlans. Every plan move writes it.
const keepNightlySQL = `release_channel = CASE WHEN COALESCE(release_channel, '') = '' AND NOT (plan = ANY(%[2]s)) AND %[1]s::varchar = ANY(%[2]s) THEN 'nightly' ELSE release_channel END`

// ClaimBudgetAlert atomically records that an alert for (month, threshold) is
// being sent and reports whether this caller won the claim. The conditional
// WHERE — a different recorded month OR a strictly higher threshold — makes the
// write fire exactly once per threshold per month, monotonically, even under
// concurrent finishers or multiple API replicas. A new month resets the
// recorded threshold to the crossed value.
func (r *OrgRepository) ClaimBudgetAlert(orgID, month string, threshold int) (bool, error) {
	res, err := r.db.Exec(`
		UPDATE organizations
		SET budget_alert_month = $2, budget_alert_threshold = $3, updated_at = NOW()
		WHERE id = $1
		  AND (COALESCE(budget_alert_month, '') <> $2 OR $3 > budget_alert_threshold)
	`, orgID, month, threshold)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ClaimMinutesAlert is ClaimBudgetAlert for the hosted-minutes allowance
// (migration 47): true once per (month, threshold) per workspace.
func (r *OrgRepository) ClaimMinutesAlert(orgID, month string, threshold int) (bool, error) {
	res, err := r.db.Exec(`
		UPDATE organizations
		SET minutes_alert_month = $2, minutes_alert_threshold = $3, updated_at = NOW()
		WHERE id = $1
		  AND (COALESCE(minutes_alert_month, '') <> $2 OR $3 > minutes_alert_threshold)
	`, orgID, month, threshold)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// SetBillingCustomer writes the provider's customer ref and the currency the
// customer is locked to.
func (r *OrgRepository) SetBillingCustomer(orgID, customerRef, currency string) error {
	_, err := r.db.Exec(`
		UPDATE organizations SET billing_customer_ref = $2, billing_currency = $3, updated_at = NOW() WHERE id = $1
	`, orgID, customerRef, currency)
	return err
}

// ApplyBillingState writes one subscription snapshot atomically.
//
// Three rules live in the SQL so no caller can forget one:
//
//   - The WHERE on plan_synced_at makes an older read lose to a newer one.
//     A slow refresh holding a snapshot from before the last reconcile
//     writes nothing, and reports so; that is a success.
//   - A granted plan (enterprise, open_source) keeps its plan column: a
//     grant wins over a subscription. Every other column still follows the
//     snapshot, so the tab can say what the provider believes.
//   - A workspace moving from a nightly-only plan onto a channel-choosing
//     one with no override gets 'nightly' written (keepNightlySQL, as by
//     SetPlan), or the flip would put a new subscriber on the stable channel
//     with no stable release, closing every gated feature, the Billing tab too.
func (r *OrgRepository) ApplyBillingState(orgID string, st orgs.BillingState) (bool, error) {
	var periodEnd interface{}
	if st.PeriodEnd != nil {
		periodEnd = st.PeriodEnd.UTC()
	}
	res, err := r.db.Exec(`
		UPDATE organizations SET
			plan = CASE WHEN plan = ANY($11) THEN plan ELSE $2 END,
			`+fmt.Sprintf(keepNightlySQL, "$2", "$12")+`,
			plan_status = $3,
			plan_interval = $4,
			plan_seats = $5,
			plan_period_end = $6,
			plan_cancel_at_period_end = $7,
			billing_subscription_ref = $8,
			billing_item_ref = $9,
			plan_synced_at = $10,
			updated_at = NOW()
		WHERE id = $1 AND (plan_synced_at IS NULL OR plan_synced_at <= $10)
	`, orgID, st.Plan, st.Status, st.Interval, st.Seats, periodEnd, st.CancelAtPeriodEnd,
		st.SubscriptionRef, st.ItemRef, st.ReadAt.UTC(),
		pq.Array([]string{orgs.PlanEnterprise, orgs.PlanOpenSource}), pq.Array(orgs.ChoosablePlans))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// SetBilledSeats writes only plan_seats.
func (r *OrgRepository) SetBilledSeats(orgID string, seats int) error {
	_, err := r.db.Exec(`UPDATE organizations SET plan_seats = $2, updated_at = NOW() WHERE id = $1`, orgID, seats)
	return err
}

// ClearBillingSubscription forgets the subscription, keeping the customer.
func (r *OrgRepository) ClearBillingSubscription(orgID string) error {
	_, err := r.db.Exec(`
		UPDATE organizations SET billing_subscription_ref = '', billing_item_ref = '',
			plan_status = $2, plan_seats = 0, plan_cancel_at_period_end = FALSE, plan_synced_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`, orgID, orgs.PlanStatusCanceled)
	return err
}

// SetGrandfathered writes only plan_grandfathered.
func (r *OrgRepository) SetGrandfathered(orgID string, on bool) error {
	_, err := r.db.Exec(`UPDATE organizations SET plan_grandfathered = $2, updated_at = NOW() WHERE id = $1`, orgID, on)
	return err
}

// GrandfatherBefore implements orgs.Repository. The overrides go UNDER the
// workspace's own limits (jsonb || keeps the right-hand side's keys), so an
// operator's per-workspace pin survives; the flag makes the step idempotent
// and is what the tab and the limits panel read. No deleted_at predicate on
// purpose: a workspace restored after the date comes back to the terms it
// left under.
func (r *OrgRepository) GrandfatherBefore(cutoff time.Time, overrides map[string]interface{}) (int64, error) {
	raw, err := json.Marshal(overrides)
	if err != nil {
		return 0, err
	}
	res, err := r.db.Exec(`
		UPDATE organizations
		SET limits = $2::jsonb || COALESCE(limits, '{}'::jsonb), plan_grandfathered = TRUE, updated_at = NOW()
		WHERE created_at < $1 AND plan_grandfathered = FALSE
	`, cutoff.UTC(), string(raw))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// FindOrgByBillingRef finds the workspace holding a provider ref, deleted
// ones included: a subscription on a deleted workspace is exactly the one
// the sync path needs to find so it can cancel it.
func (r *OrgRepository) FindOrgByBillingRef(kind, ref string) (*orgs.Org, error) {
	if ref == "" {
		return nil, nil
	}
	column := "billing_subscription_ref"
	if kind == orgs.BillingRefCustomer {
		column = "billing_customer_ref"
	}
	o, err := scanOrg(r.db.QueryRow(`SELECT `+orgColumns+` FROM organizations WHERE `+column+` = $1`, ref))
	if noRow(err) {
		return nil, nil
	}
	return o, err
}

// ListBillingOrgs lists workspaces with a subscription, least recently
// synced first, deleted ones included.
func (r *OrgRepository) ListBillingOrgs(limit int) ([]*orgs.Org, error) {
	rows, err := r.db.Query(`
		SELECT `+orgColumns+` FROM organizations
		WHERE COALESCE(billing_subscription_ref, '') <> ''
		ORDER BY plan_synced_at NULLS FIRST, created_at
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*orgs.Org
	for rows.Next() {
		o, err := scanOrg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
