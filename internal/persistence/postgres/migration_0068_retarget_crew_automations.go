package postgres

import "database/sql"

// 0068: crews are gone (issue 732), so nothing may target one any more.
//
//   - An automation that targets a crew now targets the agent of the crew's
//     entry node, when the crew has one, and no longer names the crew. One
//     whose crew has no entry agent (or is gone) is switched off: it keeps
//     its team_id, which nothing reads, so its owner can still see what it
//     was for and pick an agent.
//   - A board card assigned to a crew is assigned to the crew's entry agent
//     the same way, or else left unassigned.
//
// The crew tables (agent_teams, agent_team_nodes, agent_team_edges) and the
// team_id and team_node_id columns are left in place: dropping them cannot
// be undone, so a later migration does it once this has shipped. A run that
// was a crew's keeps its team_id; it reads as a plain run.
func m0068RetargetCrewAutomations(tx *sql.Tx) error {
	for _, stmt := range []string{
		`UPDATE automations a
			SET agent_id = n.agent_id, team_id = NULL, updated_at = NOW()
			FROM agent_teams t
			JOIN agent_team_nodes n ON n.id = t.entry_node_id AND n.team_id = t.id
			WHERE a.team_id = t.id AND a.agent_id IS NULL AND n.agent_id IS NOT NULL`,
		`UPDATE automations SET team_id = NULL, updated_at = NOW()
			WHERE team_id IS NOT NULL AND agent_id IS NOT NULL`,
		`UPDATE automations SET enabled = FALSE, updated_at = NOW()
			WHERE team_id IS NOT NULL AND agent_id IS NULL AND enabled`,
		`UPDATE work_items w
			SET assignee_type = 'agent', assignee_id = n.agent_id, updated_at = NOW()
			FROM agent_teams t
			JOIN agent_team_nodes n ON n.id = t.entry_node_id AND n.team_id = t.id
			WHERE w.assignee_type = 'team' AND w.assignee_id = t.id AND n.agent_id IS NOT NULL`,
		`UPDATE work_items SET assignee_type = 'user', assignee_id = NULL, updated_at = NOW()
			WHERE assignee_type = 'team'`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
