package postgres

import "database/sql"

// 0045: the mirrored billing snapshot (docs/plans/billing-stripe.md).
//
// No money: the provider's object ids, the subscription's state and the
// numbers the Billing tab shows. plan_status decides entitlement beside
// the plan column (orgs.EntitledPlan); plan_grandfathered records the
// alpha promise the workspace's own limit overrides enforce. TIMESTAMP
// rather than TIMESTAMPTZ because that is what this table uses, and one
// row must not mix the two.
//
// The two unique partial indexes are the tenant-confusion defence: one
// provider customer or subscription can never entitle two workspaces,
// as a constraint rather than a code path.
func m0045OrganizationsBilling(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			ALTER TABLE organizations
				ADD COLUMN IF NOT EXISTS billing_customer_ref VARCHAR(255) NOT NULL DEFAULT '',
				ADD COLUMN IF NOT EXISTS billing_subscription_ref VARCHAR(255) NOT NULL DEFAULT '',
				ADD COLUMN IF NOT EXISTS billing_item_ref VARCHAR(255) NOT NULL DEFAULT '',
				ADD COLUMN IF NOT EXISTS billing_currency VARCHAR(3) NOT NULL DEFAULT '',
				ADD COLUMN IF NOT EXISTS plan_status VARCHAR(32) NOT NULL DEFAULT 'none',
				ADD COLUMN IF NOT EXISTS plan_interval VARCHAR(8) NOT NULL DEFAULT '',
				ADD COLUMN IF NOT EXISTS plan_seats INT NOT NULL DEFAULT 0,
				ADD COLUMN IF NOT EXISTS plan_cancel_at_period_end BOOLEAN NOT NULL DEFAULT FALSE,
				ADD COLUMN IF NOT EXISTS plan_grandfathered BOOLEAN NOT NULL DEFAULT FALSE,
				ADD COLUMN IF NOT EXISTS plan_period_end TIMESTAMP,
				ADD COLUMN IF NOT EXISTS plan_synced_at TIMESTAMP
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
			CREATE UNIQUE INDEX IF NOT EXISTS idx_orgs_billing_subscription
			ON organizations(billing_subscription_ref) WHERE billing_subscription_ref <> ''
		`); err != nil {
		return err
	}
	_, err := tx.Exec(`
			CREATE UNIQUE INDEX IF NOT EXISTS idx_orgs_billing_customer
			ON organizations(billing_customer_ref) WHERE billing_customer_ref <> ''
		`)
	return err
}
