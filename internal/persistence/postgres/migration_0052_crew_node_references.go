package postgres

import "database/sql"

// 0052: a crew points only at its own nodes (#379 bug 91). A crew's
// entry_node_id had no foreign key, so deleting the entry node (or the
// agent it placed) left the crew naming a node no row has, and an edge's
// two node keys referenced agent_team_nodes(id) alone, so an edge could
// join nodes of two crews.
//
// Existing rows first, so the constraints validate: a crew whose entry
// node is no node of its own (gone, or another crew's) has its entry node
// cleared, as a crew that never had one, which a launch refuses until one
// is set ("team has no entry node") where it refused before anyway ("entry
// node not found in team"); updated_at is left alone. An edge whose from or
// to node is another crew's is deleted. Every other row stays as it is.
//
// Then the entry node references agent_team_nodes(id) ON DELETE SET NULL,
// with an index for the delete's lookup, and each of an edge's node keys
// references the node by (team_id, id), unique on agent_team_nodes, under
// the constraint name the single-column key had: a node no row has is the
// same refusal it was, and a node of another crew is now that refusal too.
// ON DELETE CASCADE is kept, so deleting a node still takes its edges.
func m0052CrewNodeReferences(tx *sql.Tx) error {
	for _, stmt := range []string{
		`UPDATE agent_teams t SET entry_node_id = NULL
			WHERE t.entry_node_id IS NOT NULL
			  AND NOT EXISTS (SELECT 1 FROM agent_team_nodes n WHERE n.id = t.entry_node_id AND n.team_id = t.id)`,
		`DELETE FROM agent_team_edges e
			WHERE NOT EXISTS (SELECT 1 FROM agent_team_nodes n WHERE n.id = e.from_node_id AND n.team_id = e.team_id)
			   OR NOT EXISTS (SELECT 1 FROM agent_team_nodes n WHERE n.id = e.to_node_id AND n.team_id = e.team_id)`,
		`ALTER TABLE agent_team_nodes ADD CONSTRAINT agent_team_nodes_team_id_id_key UNIQUE (team_id, id)`,
		`ALTER TABLE agent_team_edges DROP CONSTRAINT IF EXISTS agent_team_edges_from_node_id_fkey`,
		`ALTER TABLE agent_team_edges ADD CONSTRAINT agent_team_edges_from_node_id_fkey
			FOREIGN KEY (team_id, from_node_id) REFERENCES agent_team_nodes (team_id, id) ON DELETE CASCADE`,
		`ALTER TABLE agent_team_edges DROP CONSTRAINT IF EXISTS agent_team_edges_to_node_id_fkey`,
		`ALTER TABLE agent_team_edges ADD CONSTRAINT agent_team_edges_to_node_id_fkey
			FOREIGN KEY (team_id, to_node_id) REFERENCES agent_team_nodes (team_id, id) ON DELETE CASCADE`,
		`ALTER TABLE agent_teams ADD CONSTRAINT agent_teams_entry_node_id_fkey
			FOREIGN KEY (entry_node_id) REFERENCES agent_team_nodes (id) ON DELETE SET NULL`,
		`CREATE INDEX idx_agent_teams_entry_node ON agent_teams (entry_node_id) WHERE entry_node_id IS NOT NULL`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
