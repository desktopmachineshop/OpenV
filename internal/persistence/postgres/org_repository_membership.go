// OrgRepository's memberships (org_members): roles, the member list and the
// admin count.

package postgres

import (
	"database/sql"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// UpsertMember adds or updates a membership.
func (r *OrgRepository) UpsertMember(orgID, userID, role string) error {
	_, err := r.db.Exec(`
		INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3)
		ON CONFLICT (org_id, user_id) DO UPDATE SET role = EXCLUDED.role
	`, orgID, userID, role)
	return err
}

// RemoveMember deletes a membership, answering orgs.ErrNotMember when there
// was none to delete, so of two concurrent removals only one succeeds.
func (r *OrgRepository) RemoveMember(orgID, userID string) error {
	res, err := r.db.Exec(`DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`, orgID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return orgs.ErrNotMember
	}
	return nil
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
	if noRow(err) {
		return "", nil
	}
	return role, err
}

// MemberRoleAny is MemberRole without the deleted-org exclusion, for the
// restore path where an admin acts on their own deleted workspace.
func (r *OrgRepository) MemberRoleAny(orgID, userID string) (string, error) {
	var role string
	err := r.db.QueryRow(`SELECT role FROM org_members WHERE org_id = $1 AND user_id = $2`, orgID, userID).Scan(&role)
	if noRow(err) {
		return "", nil
	}
	return role, err
}

// ListMembers returns an org's members with display info.
func (r *OrgRepository) ListMembers(orgID string) ([]*orgs.Member, error) {
	rows, err := r.db.Query(`
		SELECT m.org_id, m.user_id, m.role, m.created_at, u.name, u.email, u.avatar_url
		FROM org_members m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1
		ORDER BY u.name, u.email
	`, orgID)
	if malformedID(err) {
		return nil, nil
	}
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
