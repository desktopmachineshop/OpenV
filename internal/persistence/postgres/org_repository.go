package postgres

import (
	"database/sql"
	"encoding/json"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// OrgRepository implements orgs.Repository and orgs.TeamRepository.
type OrgRepository struct {
	db *sql.DB
}

// NewOrgRepository creates a new org repository.
func NewOrgRepository(db *sql.DB) *OrgRepository {
	return &OrgRepository{db: db}
}

const orgColumns = `id, name, slug, org_type, plan, limits, created_by, created_at, updated_at, monthly_budget_usd, budget_alert_month, budget_alert_threshold, deleted_at, logo_path, logo_mime, COALESCE(release_channel, ''), COALESCE(stable_release, ''), COALESCE(upgrade_day, 0), COALESCE(upgrade_hour, 0), COALESCE(upgrade_timezone, ''), COALESCE(billing_customer_ref, ''), COALESCE(billing_subscription_ref, ''), COALESCE(billing_item_ref, ''), COALESCE(billing_currency, ''), COALESCE(plan_status, 'none'), COALESCE(plan_interval, ''), COALESCE(plan_seats, 0), COALESCE(plan_cancel_at_period_end, FALSE), COALESCE(plan_grandfathered, FALSE), plan_period_end, plan_synced_at`

// orgColumnsQualified disambiguates joined queries (org_members also has created_at).
const orgColumnsQualified = `o.id, o.name, o.slug, o.org_type, o.plan, o.limits, o.created_by, o.created_at, o.updated_at, o.monthly_budget_usd, o.budget_alert_month, o.budget_alert_threshold, o.deleted_at, o.logo_path, o.logo_mime, COALESCE(o.release_channel, ''), COALESCE(o.stable_release, ''), COALESCE(o.upgrade_day, 0), COALESCE(o.upgrade_hour, 0), COALESCE(o.upgrade_timezone, ''), COALESCE(o.billing_customer_ref, ''), COALESCE(o.billing_subscription_ref, ''), COALESCE(o.billing_item_ref, ''), COALESCE(o.billing_currency, ''), COALESCE(o.plan_status, 'none'), COALESCE(o.plan_interval, ''), COALESCE(o.plan_seats, 0), COALESCE(o.plan_cancel_at_period_end, FALSE), COALESCE(o.plan_grandfathered, FALSE), o.plan_period_end, o.plan_synced_at`

func scanOrg(row interface{ Scan(...interface{}) error }, extra ...interface{}) (*orgs.Org, error) {
	o := new(orgs.Org)
	var limits []byte
	var createdBy sql.NullString
	var budget sql.NullFloat64
	var alertMonth sql.NullString
	var deletedAt sql.NullTime
	var periodEnd, syncedAt sql.NullTime
	b := &o.Billing
	dest := []interface{}{&o.ID, &o.Name, &o.Slug, &o.OrgType, &o.BilledPlan, &limits, &createdBy, &o.CreatedAt, &o.UpdatedAt, &budget, &alertMonth, &o.BudgetAlertThreshold, &deletedAt, &o.LogoPath, &o.LogoMime, &o.ReleaseChannelOverride, &o.StableRelease, &o.UpgradeDay, &o.UpgradeHour, &o.UpgradeTimezone,
		// Billing columns (migration 45). Appended, never inserted: the
		// extra tail below is positional.
		&b.CustomerRef, &b.SubscriptionRef, &b.ItemRef, &b.Currency, &b.Status, &b.Interval, &b.Seats, &b.CancelAtPeriodEnd, &b.Grandfathered, &periodEnd, &syncedAt}
	dest = append(dest, extra...)
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	if periodEnd.Valid {
		v := periodEnd.Time
		b.PeriodEnd = &v
	}
	if syncedAt.Valid {
		v := syncedAt.Time
		b.SyncedAt = &v
	}
	if createdBy.Valid {
		v := createdBy.String
		o.CreatedBy = &v
	}
	if deletedAt.Valid {
		v := deletedAt.Time
		o.DeletedAt = &v
	}
	if budget.Valid {
		v := budget.Float64
		o.MonthlyBudgetUSD = &v
	}
	if alertMonth.Valid {
		o.BudgetAlertMonth = alertMonth.String
	}
	o.HasLogo = o.LogoPath != ""
	if err := json.Unmarshal(limits, &o.Limits); err != nil || o.Limits == nil {
		o.Limits = map[string]interface{}{}
	}
	o.ResolveReleaseChannel()
	return o, nil
}

// SaveOrg inserts an organization.
func (r *OrgRepository) SaveOrg(o *orgs.Org) error {
	limits, err := json.Marshal(o.Limits)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		INSERT INTO organizations (id, name, slug, org_type, plan, limits, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, o.ID, o.Name, o.Slug, o.OrgType, o.BilledPlan, limits, o.CreatedBy, o.CreatedAt, o.UpdatedAt)
	return err
}

// UpdateOrg rewrites the fields a workspace admin may change: the name.
//
// It used to rewrite plan and limits from the in-memory struct as well, a
// read-modify-write that could put a stale plan back over one the billing
// sync had just written. The plan column is written only by SetPlan and
// ApplyBillingState; limits by nothing here yet.
func (r *OrgRepository) UpdateOrg(o *orgs.Org) error {
	_, err := r.db.Exec(`
		UPDATE organizations SET name = $2, updated_at = $3 WHERE id = $1
	`, o.ID, o.Name, o.UpdatedAt)
	return err
}

// SetLogo writes only logo_path and logo_mime (empty strings clear them).
func (r *OrgRepository) SetLogo(orgID, path, mime string) error {
	_, err := r.db.Exec(`
		UPDATE organizations SET logo_path = $2, logo_mime = $3, updated_at = NOW() WHERE id = $1
	`, orgID, path, mime)
	return err
}

// FindOrgByID returns an org, or nil.
func (r *OrgRepository) FindOrgByID(id string) (*orgs.Org, error) {
	o, err := scanOrg(r.db.QueryRow(`SELECT `+orgColumns+` FROM organizations WHERE id = $1`, id))
	if noRow(err) {
		return nil, nil
	}
	return o, err
}

// ListOrgsForUser returns the user's orgs with role populated.
func (r *OrgRepository) ListOrgsForUser(userID string) ([]*orgs.Org, error) {
	rows, err := r.db.Query(`
		SELECT `+orgColumnsQualified+`, m.role FROM organizations o
		JOIN org_members m ON m.org_id = o.id
		WHERE m.user_id = $1 AND o.deleted_at IS NULL
		ORDER BY (o.org_type = 'personal') DESC, o.name
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*orgs.Org
	for rows.Next() {
		var role string
		o, err := scanOrg(rows, &role)
		if err != nil {
			return nil, err
		}
		o.Role = role
		result = append(result, o)
	}
	return result, rows.Err()
}

// FindPersonalOrgForUser returns the user's personal org, or nil.
func (r *OrgRepository) FindPersonalOrgForUser(userID string) (*orgs.Org, error) {
	o, err := scanOrg(r.db.QueryRow(`
		SELECT `+orgColumnsQualified+` FROM organizations o
		JOIN org_members m ON m.org_id = o.id
		WHERE m.user_id = $1 AND o.org_type = 'personal' AND o.deleted_at IS NULL
		ORDER BY o.created_at LIMIT 1
	`, userID))
	if noRow(err) {
		return nil, nil
	}
	return o, err
}

// ListAllOrgIDs returns every organization id, oldest first.
func (r *OrgRepository) ListAllOrgIDs() ([]string, error) {
	rows, err := r.db.Query(`SELECT id FROM organizations WHERE deleted_at IS NULL ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}
