package postgres

import "database/sql"

// 0060: the platform admin's visitor dashboard counts who reaches the public
// site and how many of them go on to make an account. A site visitor is one
// browser, named by the random id the API keeps in its first-party visitor
// cookie; site_visitors holds what its first visit recorded (the day, the
// landing page as a template that carries no token, the referring site or
// utm_source, and Cloudflare's country) and the account it was later linked
// to, and site_visit_days one row per day it came while not signed in. No IP
// address, user agent or full referrer is kept. A deleted account leaves its
// visitor behind, unlinked.
func m0060SiteVisitors(tx *sql.Tx) error {
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS site_visitors (
			id UUID PRIMARY KEY,
			first_seen TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			first_day DATE NOT NULL,
			landing_path VARCHAR(100) NOT NULL DEFAULT '',
			source VARCHAR(60) NOT NULL DEFAULT '',
			country VARCHAR(2) NOT NULL DEFAULT '',
			user_id UUID REFERENCES users(id) ON DELETE SET NULL,
			linked_at TIMESTAMPTZ
		)
	`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_site_visitors_first_day ON site_visitors(first_day)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_site_visitors_user ON site_visitors(user_id) WHERE user_id IS NOT NULL`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS site_visit_days (
			visitor_id UUID NOT NULL REFERENCES site_visitors(id) ON DELETE CASCADE,
			day DATE NOT NULL,
			PRIMARY KEY (visitor_id, day)
		)
	`); err != nil {
		return err
	}
	_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_site_visit_days_day ON site_visit_days(day)`)
	return err
}
