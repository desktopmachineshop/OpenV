package postgres

import "database/sql"

// 0008: hot-path indexes (issue #182, the data-volume pass). Every index
// serves a query that currently seq-scans or scans a broader index than it
// needs at scale; all are IF NOT EXISTS so the migration is a no-op on any
// database that already grew them by hand.
func m0008HotPathIndexes(tx *sql.Tx) error {
	stmts := []string{
		`CREATE INDEX IF NOT EXISTS idx_artifacts_project_active
				ON artifacts(project_id) WHERE valid_to IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_agent_runs_automation
				ON agent_runs(automation_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_runs_queued_claim
				ON agent_runs(org_id, priority DESC, created_at) WHERE status = 'queued'`,
		`CREATE INDEX IF NOT EXISTS idx_agent_team_edges_from
				ON agent_team_edges(from_node_id, edge_type)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_proposals_project
				ON agent_proposals(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_runs_launched_by
				ON agent_runs(launched_by)`,
		`CREATE INDEX IF NOT EXISTS idx_interview_sessions_invite
				ON interview_sessions(invite_id)`,
		`CREATE INDEX IF NOT EXISTS idx_notifications_user_unread_created
				ON notifications(user_id, created_at DESC) WHERE NOT read`,
		`CREATE INDEX IF NOT EXISTS idx_links_from_active
				ON links(from_id) WHERE valid_to IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_links_to_active
				ON links(to_id) WHERE valid_to IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_agent_team_nodes_agent
				ON agent_team_nodes(agent_id)`,
	}
	for _, st := range stmts {
		if _, err := tx.Exec(st); err != nil {
			return err
		}
	}
	return nil
}
