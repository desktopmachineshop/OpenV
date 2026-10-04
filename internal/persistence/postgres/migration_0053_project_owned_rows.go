package postgres

import "database/sql"

// 0053: what belongs to a project goes with it (#379 bug 86). Deleting a
// project deleted its row and what referenced it by a foreign key
// (memberships, team grants, share links, baselines, evidence bundles,
// product profile, repository connections), and left everything else that
// named it by a bare project_id: its artifacts with every version, their
// links, chatter and attachments, its work items, crews, test runs,
// interviews, guided sessions, project attributes, automations and agent
// proposals.
//
// Existing rows first, so the constraints validate. A row is an orphan when
// the project it names has no row, and is deleted with what hangs off it:
//
//   - the orphaned artifacts' embeddings, chatter, attachments (their
//     versions cascade), figure counters, links in either direction (a link
//     from a live project's artifact to an orphaned one included) and the
//     version records of those links, then every version of the artifacts;
//   - reference counters, work items (activity cascades), crews (nodes and
//     edges cascade), test runs (results and evidence citations cascade),
//     interviews (invites, sessions and messages cascade), guided sessions
//     (messages cascade), project attribute definitions, project-scoped
//     automations and agent proposals naming a project no row has.
//
// An agent run naming a project no row has keeps its row, for the
// workspace's usage and budget, with its project cleared, as a run launched
// outside a project. Rows with no project (a workspace's crews, automations,
// attribute definitions, guided sessions, runs) and every row of a live
// project stay as they are. domain_events, the activity log, keeps its rows
// and gets no foreign key: an event names the project it happened in after
// the project is gone, and an insert must not fail for it.
//
// Then each of those project_id columns references projects(id), ON DELETE
// CASCADE but agent_runs' ON DELETE SET NULL, with an index on crews' and
// automations' project_id for the delete's lookup (the others have one).
func m0053ProjectOwnedRows(tx *sql.Tx) error {
	const orphanArtifacts = `SELECT DISTINCT a.id FROM artifacts a WHERE NOT EXISTS (SELECT 1 FROM projects p WHERE p.id = a.project_id)`
	var embeddings sql.NullString
	if err := tx.QueryRow(`SELECT to_regclass('artifact_embeddings')::text`).Scan(&embeddings); err != nil {
		return err
	}
	var stmts []string
	if embeddings.Valid {
		stmts = append(stmts, `DELETE FROM artifact_embeddings WHERE artifact_id IN (`+orphanArtifacts+`)`)
	}
	stmts = append(stmts,
		`DELETE FROM chatter WHERE artifact_id IN (`+orphanArtifacts+`)`,
		`DELETE FROM attachments WHERE artifact_id IN (`+orphanArtifacts+`)`,
		`DELETE FROM attachment_figure_counters WHERE artifact_id IN (`+orphanArtifacts+`)`,
		`DELETE FROM link_artifacts WHERE artifact_id IN (`+orphanArtifacts+`)
			OR link_id IN (SELECT id FROM links WHERE from_id IN (`+orphanArtifacts+`) OR to_id IN (`+orphanArtifacts+`))`,
		`DELETE FROM links WHERE from_id IN (`+orphanArtifacts+`) OR to_id IN (`+orphanArtifacts+`)`,
		`DELETE FROM artifacts a WHERE NOT EXISTS (SELECT 1 FROM projects p WHERE p.id = a.project_id)`,
	)
	for _, table := range []string{"artifact_ref_counters", "work_items", "agent_teams", "test_runs", "interviews",
		"guided_sessions", "attribute_definitions", "automations", "agent_proposals"} {
		stmts = append(stmts, `DELETE FROM `+table+` x
			WHERE x.project_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM projects p WHERE p.id = x.project_id)`)
	}
	stmts = append(stmts,
		`UPDATE agent_runs r SET project_id = NULL
			WHERE r.project_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM projects p WHERE p.id = r.project_id)`,
		`CREATE INDEX idx_agent_teams_project ON agent_teams (project_id)`,
		`CREATE INDEX idx_automations_project ON automations (project_id)`,
	)
	for _, table := range []string{"artifacts", "artifact_ref_counters", "work_items", "agent_teams", "test_runs", "interviews",
		"guided_sessions", "attribute_definitions", "automations", "agent_proposals"} {
		stmts = append(stmts, `ALTER TABLE `+table+` ADD CONSTRAINT `+table+`_project_id_fkey
			FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE CASCADE`)
	}
	stmts = append(stmts, `ALTER TABLE agent_runs ADD CONSTRAINT agent_runs_project_id_fkey
			FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE SET NULL`)
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
