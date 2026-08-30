package postgres

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

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

const orgColumns = `id, name, slug, org_type, plan, limits, created_by, created_at, updated_at, monthly_budget_usd, budget_alert_month, budget_alert_threshold, deleted_at`

// orgColumnsQualified disambiguates joined queries (org_members also has created_at).
const orgColumnsQualified = `o.id, o.name, o.slug, o.org_type, o.plan, o.limits, o.created_by, o.created_at, o.updated_at, o.monthly_budget_usd, o.budget_alert_month, o.budget_alert_threshold, o.deleted_at`

func scanOrg(row interface{ Scan(...interface{}) error }, extra ...interface{}) (*orgs.Org, error) {
	o := new(orgs.Org)
	var limits []byte
	var createdBy sql.NullString
	var budget sql.NullFloat64
	var alertMonth sql.NullString
	var deletedAt sql.NullTime
	dest := []interface{}{&o.ID, &o.Name, &o.Slug, &o.OrgType, &o.Plan, &limits, &createdBy, &o.CreatedAt, &o.UpdatedAt, &budget, &alertMonth, &o.BudgetAlertThreshold, &deletedAt}
	dest = append(dest, extra...)
	if err := row.Scan(dest...); err != nil {
		return nil, err
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
	if err := json.Unmarshal(limits, &o.Limits); err != nil || o.Limits == nil {
		o.Limits = map[string]interface{}{}
	}
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
	`, o.ID, o.Name, o.Slug, o.OrgType, o.Plan, limits, o.CreatedBy, o.CreatedAt, o.UpdatedAt)
	return err
}

// UpdateOrg rewrites mutable org fields.
func (r *OrgRepository) UpdateOrg(o *orgs.Org) error {
	limits, err := json.Marshal(o.Limits)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		UPDATE organizations SET name = $2, plan = $3, limits = $4, updated_at = $5 WHERE id = $1
	`, o.ID, o.Name, o.Plan, limits, o.UpdatedAt)
	return err
}

// SetBudget writes only monthly_budget_usd (a nil budget clears it), leaving
// the alert-dedupe columns untouched so this can never race the alert claim.
func (r *OrgRepository) SetBudget(orgID string, budget *float64) error {
	_, err := r.db.Exec(`
		UPDATE organizations SET monthly_budget_usd = $2, updated_at = NOW() WHERE id = $1
	`, orgID, budget)
	return err
}

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

// FindOrgByID returns an org, or nil.
func (r *OrgRepository) FindOrgByID(id string) (*orgs.Org, error) {
	o, err := scanOrg(r.db.QueryRow(`SELECT `+orgColumns+` FROM organizations WHERE id = $1`, id))
	if err == sql.ErrNoRows {
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
	if err == sql.ErrNoRows {
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

// UpsertMember adds or updates a membership.
func (r *OrgRepository) UpsertMember(orgID, userID, role string) error {
	_, err := r.db.Exec(`
		INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3)
		ON CONFLICT (org_id, user_id) DO UPDATE SET role = EXCLUDED.role
	`, orgID, userID, role)
	return err
}

// RemoveMember deletes a membership.
func (r *OrgRepository) RemoveMember(orgID, userID string) error {
	_, err := r.db.Exec(`DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`, orgID, userID)
	return err
}

// MemberRole returns the user's role in an org ("" when not a member). A
// soft-deleted org counts as no membership, which is what hides and locks a
// deleted workspace everywhere role checks gate access.
func (r *OrgRepository) MemberRole(orgID, userID string) (string, error) {
	var role string
	err := r.db.QueryRow(`
		SELECT m.role FROM org_members m
		JOIN organizations o ON o.id = m.org_id
		WHERE m.org_id = $1 AND m.user_id = $2 AND o.deleted_at IS NULL
	`, orgID, userID).Scan(&role)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return role, err
}

// MemberRoleAny is MemberRole without the deleted-org exclusion, for the
// restore path where an admin acts on their own deleted workspace.
func (r *OrgRepository) MemberRoleAny(orgID, userID string) (string, error) {
	var role string
	err := r.db.QueryRow(`SELECT role FROM org_members WHERE org_id = $1 AND user_id = $2`, orgID, userID).Scan(&role)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return role, err
}

// SoftDeleteOrg stamps deleted_at, hiding and locking the workspace.
func (r *OrgRepository) SoftDeleteOrg(id string, at time.Time) error {
	_, err := r.db.Exec(`UPDATE organizations SET deleted_at = $2, updated_at = $2 WHERE id = $1`, id, at)
	return err
}

// RestoreOrg clears deleted_at.
func (r *OrgRepository) RestoreOrg(id string) error {
	_, err := r.db.Exec(`UPDATE organizations SET deleted_at = NULL, updated_at = NOW() WHERE id = $1`, id)
	return err
}

// ListDeletedOrgsForUser returns the user's soft-deleted orgs with role.
func (r *OrgRepository) ListDeletedOrgsForUser(userID string) ([]*orgs.Org, error) {
	rows, err := r.db.Query(`
		SELECT `+orgColumnsQualified+`, m.role FROM organizations o
		JOIN org_members m ON m.org_id = o.id
		WHERE m.user_id = $1 AND o.deleted_at IS NOT NULL
		ORDER BY o.deleted_at DESC
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

// ListExpiredDeletedOrgIDs returns orgs soft-deleted before the cutoff.
func (r *OrgRepository) ListExpiredDeletedOrgIDs(before time.Time) ([]string, error) {
	rows, err := r.db.Query(`SELECT id FROM organizations WHERE deleted_at IS NOT NULL AND deleted_at < $1`, before)
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

// PurgeOrg hard-deletes an organization and everything it contains, in one
// transaction. Most org- and project-scoped tables predate foreign keys, so
// the dependents that don't cascade are deleted explicitly, children before
// the artifacts/projects they hang off. Attachment rows go with their
// artifacts; the files on disk are not touched here (same as artifact
// deletion elsewhere in the app).
func (r *OrgRepository) PurgeOrg(id string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	const orgProjects = `SELECT id FROM projects WHERE org_id = $1`
	const orgArtifacts = `SELECT DISTINCT id FROM artifacts WHERE project_id IN (` + orgProjects + `)`

	// artifact_embeddings only exists when the pgvector migration ran.
	var embeddingsTable sql.NullString
	if err := tx.QueryRow(`SELECT to_regclass('artifact_embeddings')::text`).Scan(&embeddingsTable); err != nil {
		return err
	}
	if embeddingsTable.Valid {
		if _, err := tx.Exec(`DELETE FROM artifact_embeddings WHERE artifact_id IN (`+orgArtifacts+`)`, id); err != nil {
			return err
		}
	}

	stmts := []string{
		`DELETE FROM chatter WHERE artifact_id IN (` + orgArtifacts + `)`,
		`DELETE FROM attachments WHERE artifact_id IN (` + orgArtifacts + `)`,
		`DELETE FROM link_artifacts WHERE artifact_id IN (` + orgArtifacts + `)`,
		`DELETE FROM links WHERE from_id IN (` + orgArtifacts + `) OR to_id IN (` + orgArtifacts + `)`,
		`DELETE FROM test_runs WHERE project_id IN (` + orgProjects + `)`,  // test_results cascade
		`DELETE FROM work_items WHERE project_id IN (` + orgProjects + `)`, // activity cascades
		`DELETE FROM interviews WHERE project_id IN (` + orgProjects + `)`, // invites/sessions/messages cascade
		`DELETE FROM attribute_definitions WHERE org_id = $1 OR project_id IN (` + orgProjects + `)`,
		`DELETE FROM artifact_ref_counters WHERE project_id IN (` + orgProjects + `)`,
		`DELETE FROM artifacts WHERE project_id IN (` + orgProjects + `)`,
		`DELETE FROM projects WHERE org_id = $1`, // baselines, product_profiles, repo_connections, project_members, team access cascade
		`DELETE FROM agent_runs WHERE org_id = $1`,
		`DELETE FROM agents WHERE org_id = $1`,      // remaining runs/proposals/team nodes cascade
		`DELETE FROM agent_teams WHERE org_id = $1`, // nodes/edges cascade
		`DELETE FROM automations WHERE org_id = $1`,
		`DELETE FROM guided_sessions WHERE org_id = $1`, // messages cascade
		`DELETE FROM domain_events WHERE org_id = $1`,
		`DELETE FROM notifications WHERE org_id = $1`,
		`DELETE FROM provider_settings WHERE org_id = $1`,
		`DELETE FROM provider_logins WHERE org_id = $1`,
		`DELETE FROM templates WHERE org_id = $1`,
		`DELETE FROM organizations WHERE id = $1`, // members, teams, worker keys, pairings, hosted workers cascade
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt, id); err != nil {
			return fmt.Errorf("purge org %s: %q: %w", id, stmt, err)
		}
	}
	return tx.Commit()
}

// ListMembers returns an org's members with display info.
func (r *OrgRepository) ListMembers(orgID string) ([]*orgs.Member, error) {
	rows, err := r.db.Query(`
		SELECT m.org_id, m.user_id, m.role, m.created_at, u.name, u.email, u.avatar_url
		FROM org_members m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1
		ORDER BY u.name, u.email
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectOrgMembers(rows)
}

func collectOrgMembers(rows *sql.Rows) ([]*orgs.Member, error) {
	var result []*orgs.Member
	for rows.Next() {
		m := new(orgs.Member)
		if err := rows.Scan(&m.OrgID, &m.UserID, &m.Role, &m.CreatedAt, &m.UserName, &m.UserEmail, &m.AvatarURL); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// CountAdmins counts an org's admins.
func (r *OrgRepository) CountAdmins(orgID string) (int, error) {
	var count int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM org_members WHERE org_id = $1 AND role = 'admin'`, orgID).Scan(&count)
	return count, err
}

// --- people-teams (orgs.TeamRepository) ---

// SaveTeam inserts a people-team.
func (r *OrgRepository) SaveTeam(t *orgs.OrgTeam) error {
	_, err := r.db.Exec(`
		INSERT INTO org_teams (id, org_id, name, description, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, t.ID, t.OrgID, t.Name, t.Description, t.CreatedBy, t.CreatedAt, t.UpdatedAt)
	return err
}

// UpdateTeam rewrites a people-team.
func (r *OrgRepository) UpdateTeam(t *orgs.OrgTeam) error {
	_, err := r.db.Exec(`
		UPDATE org_teams SET name = $2, description = $3, updated_at = $4 WHERE id = $1
	`, t.ID, t.Name, t.Description, t.UpdatedAt)
	return err
}

// FindTeamByID returns a people-team, or nil.
func (r *OrgRepository) FindTeamByID(id string) (*orgs.OrgTeam, error) {
	t := new(orgs.OrgTeam)
	var createdBy sql.NullString
	err := r.db.QueryRow(`
		SELECT id, org_id, name, description, created_by, created_at, updated_at
		FROM org_teams WHERE id = $1
	`, id).Scan(&t.ID, &t.OrgID, &t.Name, &t.Description, &createdBy, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if createdBy.Valid {
		v := createdBy.String
		t.CreatedBy = &v
	}
	return t, nil
}

// ListTeams returns an org's people-teams.
func (r *OrgRepository) ListTeams(orgID string) ([]*orgs.OrgTeam, error) {
	rows, err := r.db.Query(`
		SELECT id, org_id, name, description, created_by, created_at, updated_at
		FROM org_teams WHERE org_id = $1 ORDER BY name
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*orgs.OrgTeam
	for rows.Next() {
		t := new(orgs.OrgTeam)
		var createdBy sql.NullString
		if err := rows.Scan(&t.ID, &t.OrgID, &t.Name, &t.Description, &createdBy, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		if createdBy.Valid {
			v := createdBy.String
			t.CreatedBy = &v
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

// DeleteTeam removes a people-team.
func (r *OrgRepository) DeleteTeam(id string) error {
	_, err := r.db.Exec(`DELETE FROM org_teams WHERE id = $1`, id)
	return err
}

// AddTeamMember adds a user to a people-team.
func (r *OrgRepository) AddTeamMember(teamID, userID string) error {
	_, err := r.db.Exec(`
		INSERT INTO org_team_members (org_team_id, user_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, teamID, userID)
	return err
}

// RemoveTeamMember removes a user from a people-team.
func (r *OrgRepository) RemoveTeamMember(teamID, userID string) error {
	_, err := r.db.Exec(`DELETE FROM org_team_members WHERE org_team_id = $1 AND user_id = $2`, teamID, userID)
	return err
}

// ListTeamMembers returns a people-team's members with display info.
func (r *OrgRepository) ListTeamMembers(teamID string) ([]*orgs.Member, error) {
	rows, err := r.db.Query(`
		SELECT t.org_id, tm.user_id, om.role, tm.created_at, u.name, u.email, u.avatar_url
		FROM org_team_members tm
		JOIN org_teams t ON t.id = tm.org_team_id
		JOIN users u ON u.id = tm.user_id
		LEFT JOIN org_members om ON om.org_id = t.org_id AND om.user_id = tm.user_id
		WHERE tm.org_team_id = $1
		ORDER BY u.name, u.email
	`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*orgs.Member
	for rows.Next() {
		m := new(orgs.Member)
		var role sql.NullString
		if err := rows.Scan(&m.OrgID, &m.UserID, &role, &m.CreatedAt, &m.UserName, &m.UserEmail, &m.AvatarURL); err != nil {
			return nil, err
		}
		if role.Valid {
			m.Role = role.String
		}
		result = append(result, m)
	}
	return result, rows.Err()
}
