package postgres

import (
	"database/sql"

	"github.com/openv/requirements-platform/internal/domain/members"
)

// MemberRepository implements members.Repository.
type MemberRepository struct {
	db *sql.DB
}

// NewMemberRepository creates a new member repository.
func NewMemberRepository(db *sql.DB) *MemberRepository {
	return &MemberRepository{db: db}
}

// Upsert inserts or updates a membership.
func (r *MemberRepository) Upsert(m *members.Member) error {
	_, err := r.db.Exec(`
		INSERT INTO project_members (project_id, user_id, role, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (project_id, user_id) DO UPDATE SET role = EXCLUDED.role
	`, m.ProjectID, m.UserID, m.Role, m.CreatedAt)
	return err
}

// Remove deletes a membership.
func (r *MemberRepository) Remove(projectID, userID string) error {
	_, err := r.db.Exec(`DELETE FROM project_members WHERE project_id = $1 AND user_id = $2`, projectID, userID)
	return err
}

// Find returns a membership, or nil.
func (r *MemberRepository) Find(projectID, userID string) (*members.Member, error) {
	m := new(members.Member)
	err := r.db.QueryRow(`
		SELECT project_id, user_id, role, created_at
		FROM project_members WHERE project_id = $1 AND user_id = $2
	`, projectID, userID).Scan(&m.ProjectID, &m.UserID, &m.Role, &m.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

// ListByProject returns members with display info joined from users.
func (r *MemberRepository) ListByProject(projectID string) ([]*members.Member, error) {
	rows, err := r.db.Query(`
		SELECT pm.project_id, pm.user_id, pm.role, pm.created_at, u.name, u.email, u.avatar_url
		FROM project_members pm
		JOIN users u ON u.id = pm.user_id
		WHERE pm.project_id = $1
		ORDER BY u.name, u.email
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*members.Member
	for rows.Next() {
		m := new(members.Member)
		if err := rows.Scan(&m.ProjectID, &m.UserID, &m.Role, &m.CreatedAt, &m.UserName, &m.UserEmail, &m.AvatarURL); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// ListProjectIDsForUser returns ids of projects the user can access via
// direct membership or people-team grants.
func (r *MemberRepository) ListProjectIDsForUser(userID string) ([]string, error) {
	rows, err := r.db.Query(`
		SELECT project_id FROM project_members WHERE user_id = $1
		UNION
		SELECT pta.project_id FROM project_team_access pta
		JOIN org_team_members otm ON otm.org_team_id = pta.org_team_id
		WHERE otm.user_id = $1
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// RolesFor returns every role the user holds on a project (direct + teams).
func (r *MemberRepository) RolesFor(projectID, userID string) ([]string, error) {
	rows, err := r.db.Query(`
		SELECT role FROM project_members WHERE project_id = $1 AND user_id = $2
		UNION ALL
		SELECT pta.role FROM project_team_access pta
		JOIN org_team_members otm ON otm.org_team_id = pta.org_team_id
		WHERE pta.project_id = $1 AND otm.user_id = $2
	`, projectID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

// UpsertTeamGrant adds or updates a people-team's project role.
func (r *MemberRepository) UpsertTeamGrant(g *members.TeamGrant) error {
	_, err := r.db.Exec(`
		INSERT INTO project_team_access (project_id, org_team_id, role, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (project_id, org_team_id) DO UPDATE SET role = EXCLUDED.role
	`, g.ProjectID, g.OrgTeamID, g.Role, g.CreatedAt)
	return err
}

// RemoveTeamGrant deletes a team grant.
func (r *MemberRepository) RemoveTeamGrant(projectID, orgTeamID string) error {
	_, err := r.db.Exec(`DELETE FROM project_team_access WHERE project_id = $1 AND org_team_id = $2`, projectID, orgTeamID)
	return err
}

// ListTeamGrants returns a project's team grants with team names.
func (r *MemberRepository) ListTeamGrants(projectID string) ([]*members.TeamGrant, error) {
	rows, err := r.db.Query(`
		SELECT pta.project_id, pta.org_team_id, pta.role, pta.created_at, t.name
		FROM project_team_access pta
		JOIN org_teams t ON t.id = pta.org_team_id
		WHERE pta.project_id = $1
		ORDER BY t.name
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*members.TeamGrant
	for rows.Next() {
		g := new(members.TeamGrant)
		if err := rows.Scan(&g.ProjectID, &g.OrgTeamID, &g.Role, &g.CreatedAt, &g.TeamName); err != nil {
			return nil, err
		}
		result = append(result, g)
	}
	return result, rows.Err()
}
