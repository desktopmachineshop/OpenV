// OrgRepository's people-teams (orgs.TeamRepository).

package postgres

import (
	"database/sql"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

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
	if noRow(err) {
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
	if malformedID(err) {
		return nil, nil
	}
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
	return matchedNone(err)
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
	if malformedID(err) {
		return nil, nil
	}
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
