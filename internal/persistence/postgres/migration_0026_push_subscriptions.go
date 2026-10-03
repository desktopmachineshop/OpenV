package postgres

import "database/sql"

// 0026: web push subscriptions and the per-user push opt-in (REQ-109).
//
// One row per DEVICE, not per user: a member who installs OpenV on a
// phone and a tablet has two subscriptions, each with its own endpoint
// and its own encryption keys. The endpoint is the browser push
// service's URL for that device, is globally unique, and is what a
// re-subscription (key rotation, reinstall) collides on — hence the
// unique index, which makes POST idempotent.
//
// last_used_at records the last successful send and failed_at the last
// unsuccessful one; a later success clears failed_at. A 404/410 from the
// push service means the subscription is gone for good and the row is
// deleted instead.
//
// users.push_notifications is the per-user opt-in. It defaults FALSE —
// unlike email, push is never on until the member grants the browser
// permission on a device, so an opt-in default of TRUE would be a lie.
func m0026PushSubscriptions(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			ALTER TABLE users
				ADD COLUMN IF NOT EXISTS push_notifications BOOLEAN NOT NULL DEFAULT FALSE
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS push_subscriptions (
				id UUID PRIMARY KEY,
				user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				endpoint TEXT NOT NULL,
				p256dh TEXT NOT NULL,
				auth TEXT NOT NULL,
				user_agent TEXT NOT NULL DEFAULT '',
				created_at TIMESTAMP NOT NULL DEFAULT NOW(),
				last_used_at TIMESTAMP,
				failed_at TIMESTAMP
			)
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_push_subscriptions_endpoint ON push_subscriptions(endpoint)`); err != nil {
		return err
	}
	_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_push_subscriptions_user ON push_subscriptions(user_id)`)
	return err
}
