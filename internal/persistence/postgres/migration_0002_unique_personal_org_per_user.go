package postgres

import (
	"database/sql"
	"fmt"
	"strings"
)

// 0002: at most one personal organization per user. Personal orgs are
// always created with created_by = the owning user (signup's
// EnsurePersonalOrg and the boot backfill both do), so a partial unique
// index on created_by closes the check-then-insert races in both paths.
// NULL created_by rows (possible on hand-edited data) are not
// constrained — Postgres treats NULLs as distinct — which is the safe
// direction. Existing duplicates would make CREATE INDEX fail with an
// opaque error, so the migration checks first and fails with an
// actionable message; the transaction rolls back and the ledger stays
// unapplied, so a fixed database retries cleanly on the next boot.
func m0002UniquePersonalOrgPerUser(tx *sql.Tx) error {
	rows, err := tx.Query(`
			SELECT created_by::text FROM organizations
			WHERE org_type = 'personal' AND created_by IS NOT NULL
			GROUP BY created_by HAVING COUNT(*) > 1
		`)
	if err != nil {
		return err
	}
	var dupes []string
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			rows.Close()
			return err
		}
		dupes = append(dupes, userID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(dupes) > 0 {
		return fmt.Errorf(
			"cannot enforce one personal organization per user: user(s) %s own multiple personal organizations; merge or delete the duplicates, then restart",
			strings.Join(dupes, ", "))
	}
	_, err = tx.Exec(`
			CREATE UNIQUE INDEX idx_organizations_personal_owner
			ON organizations(created_by) WHERE org_type = 'personal'
		`)
	return err
}
