package postgres

import "database/sql"

// clearProjectRunReferences clears, inside the transaction that deletes the
// project, what the project's runs name among the rows the delete removes:
// a card on its board, a guided session, an interview session, an
// automation, a crew and a crew's node of the project. None has a foreign
// key, so they went on naming rows no longer there (#379 bug 149), and the
// board hook logged an ERROR "failed to move card" whenever such a run
// changed status, as the cancel the delete announces makes it. What a run
// names outside the project, a workspace crew or a whole-workspace
// automation, stays. The runs are found by their project, so this runs
// before the project's row goes, as cancelProjectRuns does.
func clearProjectRunReferences(tx *sql.Tx, projectID string) error {
	_, err := tx.Exec(`
		UPDATE agent_runs r SET
			work_item_id = CASE WHEN r.work_item_id IN (SELECT id FROM work_items WHERE project_id = $1) THEN NULL ELSE r.work_item_id END,
			guided_session_id = CASE WHEN r.guided_session_id IN (SELECT id FROM guided_sessions WHERE project_id = $1) THEN NULL ELSE r.guided_session_id END,
			interview_session_id = CASE WHEN r.interview_session_id IN (SELECT s.id FROM interview_sessions s JOIN interviews i ON i.id = s.interview_id WHERE i.project_id = $1) THEN NULL ELSE r.interview_session_id END,
			automation_id = CASE WHEN r.automation_id IN (SELECT id FROM automations WHERE project_id = $1) THEN NULL ELSE r.automation_id END,
			team_id = CASE WHEN r.team_id IN (SELECT id FROM agent_teams WHERE project_id = $1) THEN NULL ELSE r.team_id END,
			team_node_id = CASE WHEN r.team_node_id IN (SELECT n.id FROM agent_team_nodes n JOIN agent_teams t ON t.id = n.team_id WHERE t.project_id = $1) THEN NULL ELSE r.team_node_id END
		WHERE r.project_id = $1
		  AND (r.work_item_id IS NOT NULL OR r.guided_session_id IS NOT NULL OR r.interview_session_id IS NOT NULL
		    OR r.automation_id IS NOT NULL OR r.team_id IS NOT NULL OR r.team_node_id IS NOT NULL)
	`, projectID)
	return err
}
