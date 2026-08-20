package postgres

import (
	"database/sql"
	"encoding/json"

	"github.com/openv/requirements-platform/internal/domain/teams"
)

// TeamRepository implements the teams.Repository interface over the
// agent_teams, agent_team_nodes, and agent_team_edges tables.
type TeamRepository struct {
	db *sql.DB
}

// NewTeamRepository creates a new team repository.
func NewTeamRepository(db *sql.DB) *TeamRepository {
	return &TeamRepository{db: db}
}

func marshalJSONMap(m map[string]interface{}) ([]byte, error) {
	if m == nil {
		m = map[string]interface{}{}
	}
	return json.Marshal(m)
}

func unmarshalJSONMap(data []byte) map[string]interface{} {
	m := map[string]interface{}{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &m); err != nil {
			return map[string]interface{}{}
		}
	}
	return m
}

// --- teams ---

func scanTeam(scan func(dest ...interface{}) error) (*teams.Team, error) {
	t := new(teams.Team)
	var projectID, entryNodeID string
	err := scan(
		&t.ID, &t.OrgID, &t.Name, &t.Description, &projectID, &entryNodeID,
		&t.IsDefault, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if projectID != "" {
		t.ProjectID = &projectID
	}
	if entryNodeID != "" {
		t.EntryNodeID = &entryNodeID
	}
	return t, nil
}

const teamColumns = `
	id, COALESCE(org_id::text, ''), name, description, COALESCE(project_id::text, ''), COALESCE(entry_node_id::text, ''),
	is_default, created_at, updated_at`

// SaveTeam inserts a new team.
func (r *TeamRepository) SaveTeam(t *teams.Team) error {
	_, err := r.db.Exec(`
		INSERT INTO agent_teams (id, org_id, name, description, project_id, entry_node_id, is_default, created_at, updated_at)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7, $8, $9)
	`, t.ID, t.OrgID, t.Name, t.Description, t.ProjectID, t.EntryNodeID, t.IsDefault, t.CreatedAt, t.UpdatedAt)
	return err
}

// UpdateTeam rewrites a team's mutable columns.
func (r *TeamRepository) UpdateTeam(t *teams.Team) error {
	_, err := r.db.Exec(`
		UPDATE agent_teams SET
			name = $2, description = $3, project_id = $4, entry_node_id = $5, is_default = $6, updated_at = $7
		WHERE id = $1
	`, t.ID, t.Name, t.Description, t.ProjectID, t.EntryNodeID, t.IsDefault, t.UpdatedAt)
	return err
}

// FindTeamByID retrieves a team, returning (nil, nil) when absent.
func (r *TeamRepository) FindTeamByID(id string) (*teams.Team, error) {
	row := r.db.QueryRow(`SELECT `+teamColumns+` FROM agent_teams WHERE id = $1`, id)
	t, err := scanTeam(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

// ListTeams returns an org's teams visible to a project: the project's own
// plus global-in-org (project_id IS NULL) teams. An empty projectID lists
// all the org's teams.
func (r *TeamRepository) ListTeams(orgID, projectID string) ([]*teams.Team, error) {
	rows, err := r.db.Query(`
		SELECT `+teamColumns+`
		FROM agent_teams
		WHERE org_id = NULLIF($1, '')::uuid
		  AND ($2 = '' OR project_id IS NULL OR project_id = NULLIF($2, '')::uuid)
		ORDER BY created_at
	`, orgID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*teams.Team
	for rows.Next() {
		t, err := scanTeam(rows.Scan)
		if err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

// DeleteTeam removes a team; nodes and edges cascade.
func (r *TeamRepository) DeleteTeam(id string) error {
	_, err := r.db.Exec(`DELETE FROM agent_teams WHERE id = $1`, id)
	return err
}

// MarkDefault flags a team as its org's seeded default.
func (r *TeamRepository) MarkDefault(teamID string) error {
	_, err := r.db.Exec(`UPDATE agent_teams SET is_default = TRUE WHERE id = $1`, teamID)
	return err
}

// FindDefaultTeam returns an org's default team, or (nil, nil) when none is set.
func (r *TeamRepository) FindDefaultTeam(orgID string) (*teams.Team, error) {
	row := r.db.QueryRow(`SELECT `+teamColumns+` FROM agent_teams WHERE org_id = NULLIF($1, '')::uuid AND is_default ORDER BY created_at LIMIT 1`, orgID)
	t, err := scanTeam(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

// --- nodes ---

func scanTeamNode(scan func(dest ...interface{}) error) (*teams.Node, error) {
	n := new(teams.Node)
	var position []byte
	var userID string
	err := scan(&n.ID, &n.TeamID, &n.NodeType, &n.AgentID, &userID, &n.Label, &n.Department, &position, &n.CreatedAt, &n.UserName, &n.UserAvatarURL)
	if err != nil {
		return nil, err
	}
	if userID != "" {
		n.UserID = &userID
	}
	n.Position = unmarshalJSONMap(position)
	return n, nil
}

// teamNodeColumns selects node rows joined with the users table so human
// nodes carry a denormalized display name and avatar.
const teamNodeColumns = `
	n.id, n.team_id, n.node_type, COALESCE(n.agent_id::text, ''), COALESCE(n.user_id::text, ''),
	n.label, n.department, n.position, n.created_at,
	COALESCE(u.name, ''), COALESCE(u.avatar_url, '')`

func nodeUserID(n *teams.Node) string {
	if n.UserID != nil {
		return *n.UserID
	}
	return ""
}

func nodeType(n *teams.Node) string {
	if n.NodeType == "" {
		return teams.NodeAgent
	}
	return n.NodeType
}

// SaveNode inserts a new team node.
func (r *TeamRepository) SaveNode(n *teams.Node) error {
	position, err := marshalJSONMap(n.Position)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		INSERT INTO agent_team_nodes (id, team_id, node_type, agent_id, user_id, label, department, position, created_at)
		VALUES ($1, $2, $3, NULLIF($4, '')::uuid, NULLIF($5, '')::uuid, $6, $7, $8, $9)
	`, n.ID, n.TeamID, nodeType(n), n.AgentID, nodeUserID(n), n.Label, n.Department, position, n.CreatedAt)
	return err
}

// UpdateNode rewrites a node's mutable columns.
func (r *TeamRepository) UpdateNode(n *teams.Node) error {
	position, err := marshalJSONMap(n.Position)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		UPDATE agent_team_nodes SET node_type = $2, agent_id = NULLIF($3, '')::uuid, user_id = NULLIF($4, '')::uuid, label = $5, department = $6, position = $7
		WHERE id = $1
	`, n.ID, nodeType(n), n.AgentID, nodeUserID(n), n.Label, n.Department, position)
	return err
}

// FindNodeByID retrieves a node, returning (nil, nil) when absent.
func (r *TeamRepository) FindNodeByID(id string) (*teams.Node, error) {
	row := r.db.QueryRow(`
		SELECT `+teamNodeColumns+`
		FROM agent_team_nodes n
		LEFT JOIN users u ON u.id = n.user_id
		WHERE n.id = $1
	`, id)
	n, err := scanTeamNode(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return n, nil
}

// ListNodesByTeam returns a team's nodes.
func (r *TeamRepository) ListNodesByTeam(teamID string) ([]*teams.Node, error) {
	rows, err := r.db.Query(`
		SELECT `+teamNodeColumns+`
		FROM agent_team_nodes n
		LEFT JOIN users u ON u.id = n.user_id
		WHERE n.team_id = $1
		ORDER BY n.created_at
	`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*teams.Node
	for rows.Next() {
		n, err := scanTeamNode(rows.Scan)
		if err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, rows.Err()
}

// DeleteNode removes a node; its edges cascade.
func (r *TeamRepository) DeleteNode(id string) error {
	_, err := r.db.Exec(`DELETE FROM agent_team_nodes WHERE id = $1`, id)
	return err
}

// --- edges ---

func scanTeamEdge(scan func(dest ...interface{}) error) (*teams.Edge, error) {
	e := new(teams.Edge)
	var config []byte
	err := scan(&e.ID, &e.TeamID, &e.FromNodeID, &e.ToNodeID, &e.EdgeType, &config, &e.CreatedAt)
	if err != nil {
		return nil, err
	}
	e.Config = unmarshalJSONMap(config)
	return e, nil
}

// SaveEdge inserts a new team edge.
func (r *TeamRepository) SaveEdge(e *teams.Edge) error {
	config, err := marshalJSONMap(e.Config)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		INSERT INTO agent_team_edges (id, team_id, from_node_id, to_node_id, edge_type, config, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, e.ID, e.TeamID, e.FromNodeID, e.ToNodeID, e.EdgeType, config, e.CreatedAt)
	return err
}

// UpdateEdge rewrites an edge's config.
func (r *TeamRepository) UpdateEdge(e *teams.Edge) error {
	config, err := marshalJSONMap(e.Config)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		UPDATE agent_team_edges SET edge_type = $2, config = $3
		WHERE id = $1
	`, e.ID, e.EdgeType, config)
	return err
}

// FindEdgeByID retrieves an edge, returning (nil, nil) when absent.
func (r *TeamRepository) FindEdgeByID(id string) (*teams.Edge, error) {
	row := r.db.QueryRow(`
		SELECT id, team_id, from_node_id, to_node_id, edge_type, config, created_at
		FROM agent_team_edges
		WHERE id = $1
	`, id)
	e, err := scanTeamEdge(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return e, nil
}

// ListEdgesByTeam returns a team's edges.
func (r *TeamRepository) ListEdgesByTeam(teamID string) ([]*teams.Edge, error) {
	rows, err := r.db.Query(`
		SELECT id, team_id, from_node_id, to_node_id, edge_type, config, created_at
		FROM agent_team_edges
		WHERE team_id = $1
		ORDER BY created_at
	`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*teams.Edge
	for rows.Next() {
		e, err := scanTeamEdge(rows.Scan)
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

// DeleteEdge removes an edge.
func (r *TeamRepository) DeleteEdge(id string) error {
	_, err := r.db.Exec(`DELETE FROM agent_team_edges WHERE id = $1`, id)
	return err
}
