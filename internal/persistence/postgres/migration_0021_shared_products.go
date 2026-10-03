package postgres

import "database/sql"

// 0021: the community pool of joke demo products for the new-project
// wizard's random roller. Deliberately global — every workspace reads and
// writes the same rows — so it carries no org_id scoping column of the
// usual kind: created_by_org/created_by_user are moderation metadata
// (rate limiting, takedown), never a visibility filter, and are never
// served to clients. No foreign keys: community content outlives the
// workspace that published it, and a workspace purge must not fail on it
// or silently retract other people's roll list.
func m0021SharedProducts(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS shared_products (
				id UUID PRIMARY KEY,
				name_key TEXT NOT NULL UNIQUE,
				category TEXT NOT NULL,
				name TEXT NOT NULL,
				description TEXT NOT NULL,
				vision TEXT NOT NULL,
				problem TEXT NOT NULL,
				target_users TEXT NOT NULL,
				created_by_org UUID,
				created_by_user UUID,
				reports INTEGER NOT NULL DEFAULT 0,
				hidden BOOLEAN NOT NULL DEFAULT FALSE,
				created_at TIMESTAMP NOT NULL DEFAULT NOW()
			)
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
			CREATE INDEX IF NOT EXISTS idx_shared_products_visible
			ON shared_products (created_at DESC) WHERE hidden = FALSE
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
			CREATE INDEX IF NOT EXISTS idx_shared_products_org_created
			ON shared_products (created_by_org, created_at DESC)
		`); err != nil {
		return err
	}
	// Reports are per person, not per click: without the unique pair,
	// one user could press Report enough times to hide anything in
	// everyone's roll list.
	_, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS shared_product_reports (
				product_id UUID NOT NULL REFERENCES shared_products(id) ON DELETE CASCADE,
				user_id UUID NOT NULL,
				created_at TIMESTAMP NOT NULL DEFAULT NOW(),
				PRIMARY KEY (product_id, user_id)
			)
		`)
	return err
}
