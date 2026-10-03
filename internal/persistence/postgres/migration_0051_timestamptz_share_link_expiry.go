package postgres

import "database/sql"

// 0051: a share link's expiry is stored as the instant a client sends,
// as 0050 stores the other client-supplied times (#379 bug 4). As a
// TIMESTAMP it dropped the offset and kept the wall clock, so a link
// sent to expire at 12:00+02:00 opened until 12:00 UTC, two hours after
// its owner meant it to close. The values stored so far are read as the
// UTC wall clocks the app sends and the old reads compared as UTC, so
// every existing link closes at the instant it did before. No index or
// SQL comparison reads the column: the expiry is judged in Go.
func m0051TimestamptzShareLinkExpiry(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE project_share_links ALTER COLUMN expires_at TYPE TIMESTAMPTZ USING expires_at AT TIME ZONE 'UTC'`)
	return err
}
