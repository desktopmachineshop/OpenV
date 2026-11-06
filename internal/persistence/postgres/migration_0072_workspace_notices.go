package postgres

import "database/sql"

// 0072: once-only notices to a workspace's admins (orgs.Notices). A row is
// the record that the notice key was sent: its subject, when, and how many
// admins it reached. The key is the occurrence a module names (a renewal
// that failed, a trial about to end), so the primary key is what stops a
// polling loop from sending one twice, across replicas too. Rows go with
// their workspace through the cascading foreign key (Q17). Once it has
// shipped it is frozen (I16).
func m0072WorkspaceNotices(tx *sql.Tx) error {
	_, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS workspace_notices (
			org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			notice_key VARCHAR(200) NOT NULL,
			subject VARCHAR(300) NOT NULL DEFAULT '',
			recipients INTEGER NOT NULL DEFAULT 0,
			sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (org_id, notice_key)
		)
	`)
	return err
}
