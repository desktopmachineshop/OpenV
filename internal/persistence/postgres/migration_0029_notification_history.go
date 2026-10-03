package postgres

import "database/sql"

// 0029: notification history — clearing archives instead of deleting, and
// a notification can be flagged to keep (REQ-120).
//
// cleared_at NULL means the row is in the inbox; a timestamp means the
// member cleared it. Clearing used to DELETE, which made "the notifications
// I dealt with last month" unanswerable — the rows were gone. Archiving
// keeps the history that the Cleared tab reads, and makes clearing
// recoverable in the only sense that matters: the row is still there.
//
// flagged is the member's own "keep this one in reach". It is deliberately
// independent of cleared_at, so flagging does not exempt a row from a
// clear — a flagged row that was cleared is still flagged, and the Flagged
// tab still finds it. Nothing is lost by clearing, so the clear needs no
// special case for it.
//
// Both indexes are PARTIAL, matching the two tabs that are read most: the
// inbox (cleared_at IS NULL) and the flagged list. The cleared tab reads
// the same (user_id, created_at) ordering and is served by the existing
// user index, since it is the rarely-opened one.
func m0029NotificationHistory(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			ALTER TABLE notifications
				ADD COLUMN IF NOT EXISTS cleared_at TIMESTAMP,
				ADD COLUMN IF NOT EXISTS flagged BOOLEAN NOT NULL DEFAULT FALSE
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
			CREATE INDEX IF NOT EXISTS idx_notifications_inbox
			ON notifications (user_id, created_at DESC) WHERE cleared_at IS NULL
		`); err != nil {
		return err
	}
	_, err := tx.Exec(`
			CREATE INDEX IF NOT EXISTS idx_notifications_flagged
			ON notifications (user_id, created_at DESC) WHERE flagged
		`)
	return err
}
