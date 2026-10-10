package postgres

import "database/sql"

// 0059: the platform admin's user dashboard counts active and lost users and
// where they sign in from, which needs a record of activity that outlives a
// session: sessions expire and are swept, so their last_seen_at cannot say
// who was active last month. One row per user per UTC day they used the app,
// with the two-letter country Cloudflare reported for that day (” when it
// reported none). No IP address is kept. The rows go with their user.
//
// The table is seeded from what is already known: each user was active on
// the day they signed up, and on the day each live session was last seen.
func m0059UserActivityDays(tx *sql.Tx) error {
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS user_activity_days (
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			day DATE NOT NULL,
			country VARCHAR(2) NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, day)
		)
	`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_user_activity_days_day ON user_activity_days(day)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO user_activity_days (user_id, day)
		SELECT id, created_at::date FROM users
		ON CONFLICT DO NOTHING
	`); err != nil {
		return err
	}
	_, err := tx.Exec(`
		INSERT INTO user_activity_days (user_id, day)
		SELECT DISTINCT user_id, last_seen_at::date FROM sessions
		ON CONFLICT DO NOTHING
	`)
	return err
}
