package postgres

import "database/sql"

// 0028: votes on the community pool of demo products, so the roller can
// offer "top 5 all time" and "top 5 this week" instead of only chance.
//
// Votes are stored per (product, user) for the same reason reports are
// (0021): one account, one vote, so a count cannot be clicked up. The
// pair is the primary key, which makes voting twice a no-op at the
// storage layer rather than something the service has to police.
//
// shared_products.votes is a denormalised all-time count, written inside
// the same transaction as the vote row so the two cannot drift; it is
// what the all-time leaderboard orders by without touching the vote
// table. The weekly figure is deliberately NOT denormalised — it changes
// as time passes, not only as votes arrive — so it is counted from
// shared_product_votes over a rolling window, which is what the
// created_at index serves.
func m0028SharedProductVotes(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS shared_product_votes (
				product_id UUID NOT NULL REFERENCES shared_products(id) ON DELETE CASCADE,
				user_id UUID NOT NULL,
				created_at TIMESTAMP NOT NULL DEFAULT NOW(),
				PRIMARY KEY (product_id, user_id)
			)
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
			CREATE INDEX IF NOT EXISTS idx_shared_product_votes_created
			ON shared_product_votes (created_at)
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
			ALTER TABLE shared_products ADD COLUMN IF NOT EXISTS votes INTEGER NOT NULL DEFAULT 0
		`); err != nil {
		return err
	}
	_, err := tx.Exec(`
			CREATE INDEX IF NOT EXISTS idx_shared_products_top
			ON shared_products (votes DESC, created_at DESC) WHERE hidden = FALSE
		`)
	return err
}
