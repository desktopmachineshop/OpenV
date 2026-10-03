package postgres

import "database/sql"

// A to-do raised from a note remembers which note it came from, so the
// note can show its live status instead of a copy that goes stale.
func m0043WorkItemSourceNote(tx *sql.Tx) error {
	if _, err := tx.Exec(`ALTER TABLE work_items ADD COLUMN IF NOT EXISTS source_chatter_id UUID`); err != nil {
		return err
	}
	_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_work_items_source_chatter ON work_items(source_chatter_id) WHERE source_chatter_id IS NOT NULL`)
	return err
}
