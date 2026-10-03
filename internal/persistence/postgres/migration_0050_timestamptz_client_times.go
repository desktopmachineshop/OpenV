package postgres

import "database/sql"

// 0050: an instant a client sends is stored as one (#379 bug 4). An
// evidence bundle's captured_at, a work item's due_date and an
// invitation's expires_at, both an interview invite's and a workspace
// invitation's, were TIMESTAMP, which drops the offset a time was sent
// with and keeps its wall clock, so the instant moved by the offset. The
// values stored so far are the UTC wall clock the server writes, and
// are read as UTC.
//
// The evidence list's index orders by COALESCE(captured_at, created_at);
// with captured_at a TIMESTAMPTZ and created_at still a TIMESTAMP, the
// implicit cast between them depends on the session's TimeZone, which an
// index may not, so the index is rebuilt over created_at read as UTC.
func m0050TimestamptzClientTimes(tx *sql.Tx) error {
	for _, stmt := range []string{
		`DROP INDEX IF EXISTS idx_evidence_bundles_project`,
		`ALTER TABLE evidence_bundles ALTER COLUMN captured_at TYPE TIMESTAMPTZ USING captured_at AT TIME ZONE 'UTC'`,
		`CREATE INDEX idx_evidence_bundles_project
				ON evidence_bundles (project_id, COALESCE(captured_at, created_at AT TIME ZONE 'UTC') DESC)`,
		`ALTER TABLE work_items ALTER COLUMN due_date TYPE TIMESTAMPTZ USING due_date AT TIME ZONE 'UTC'`,
		`ALTER TABLE interview_invites ALTER COLUMN expires_at TYPE TIMESTAMPTZ USING expires_at AT TIME ZONE 'UTC'`,
		`ALTER TABLE org_invitations ALTER COLUMN expires_at TYPE TIMESTAMPTZ USING expires_at AT TIME ZONE 'UTC'`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
