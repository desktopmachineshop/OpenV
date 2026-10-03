package postgres

import "database/sql"

// 0006: in-app notifications (issue #132). One row per recipient per
// event; entity_ref is an opaque jsonb pointer the frontend uses to
// navigate ({"kind":"run","run_id":...}). The partial index serves the
// two hot queries (bell badge count, unread-first listing) without
// paying for read rows. NOTE: 0005 is deliberately skipped here — it is
// being claimed by the concurrent suspect-links branch; the registry
// only requires ascending, unique versions, so both branches merge
// cleanly in either order.
func m0006Notifications(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			CREATE TABLE notifications (
				id UUID PRIMARY KEY,
				org_id UUID,
				user_id UUID NOT NULL,
				type VARCHAR(64) NOT NULL,
				title TEXT NOT NULL,
				body TEXT NOT NULL DEFAULT '',
				entity_ref JSONB NOT NULL DEFAULT '{}'::jsonb,
				read BOOLEAN NOT NULL DEFAULT FALSE,
				created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)
		`); err != nil {
		return err
	}
	_, err := tx.Exec(`
			CREATE INDEX idx_notifications_user_created
			ON notifications(user_id, created_at DESC)
		`)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
			CREATE INDEX idx_notifications_user_unread
			ON notifications(user_id) WHERE NOT read
		`)
	return err
}
