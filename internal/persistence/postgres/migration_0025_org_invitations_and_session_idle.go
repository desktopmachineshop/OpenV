package postgres

import "database/sql"

// 0025: workspace invitations and session idle expiry (REQ-95, REQ-99 /
// HAZ-15, HAZ-16).
//
// org_invitations is how someone joins a workspace they have no account
// for — the prerequisite for closing self-service registration, since a
// closed deployment has no other door. It carries the same token contract
// as worker_keys and email_verifications: the raw value is shown once and
// only its SHA-256 is stored, because the link is a credential. Bounded
// twice, by expires_at (7 days) and by accepted_at, so a link left in a
// mailbox is not a standing key into the workspace.
//
// The email is stored already folded to lower case (the domain folds it
// on the way in), and the partial unique index enforces one PENDING
// invitation per address per workspace while leaving the accepted history
// alone: re-inviting after an acceptance is a new, separate row.
//
// last_emailed_at is when the link was last actually delivered, and is
// NULL until a send succeeds — the mail goes out off the request path, so
// the row's existence says nothing about whether anybody received one. It
// is what makes suppressing a repeat mail safe: an invitation whose send
// failed, or was never attempted, has no stamp and is sent again.
//
// sessions.last_seen_at is added here rather than assumed. The column is
// in the frozen baseline, so every database created since it landed has
// it — but a database whose sessions table predates it does not, and
// CREATE TABLE IF NOT EXISTS would never have added it. Idle expiry reads
// that column on every authenticated request, so it is made certain,
// backfilled from created_at ("used once, at sign-in").
func m0025OrgInvitationsAndSessionIdle(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS org_invitations (
				id UUID PRIMARY KEY,
				org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
				email VARCHAR(320) NOT NULL,
				role VARCHAR(32) NOT NULL DEFAULT 'member',
				token_hash VARCHAR(128) NOT NULL,
				invited_by UUID REFERENCES users(id) ON DELETE SET NULL,
				expires_at TIMESTAMP NOT NULL,
				accepted_at TIMESTAMP,
				created_at TIMESTAMP NOT NULL DEFAULT NOW(),
				last_emailed_at TIMESTAMP
			)
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_org_invitations_token ON org_invitations(token_hash)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
			CREATE UNIQUE INDEX IF NOT EXISTS idx_org_invitations_pending
			ON org_invitations(org_id, email) WHERE accepted_at IS NULL
		`); err != nil {
		return err
	}
	// A verified address (email verification, SSO) and a closed
	// deployment's sign-up check look an address up across every
	// workspace, so the email needs its own index. Lookups compare the
	// column directly against a folded address — never LOWER(email) —
	// so this plain index is the one they use.
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_org_invitations_email ON org_invitations(email)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
			ALTER TABLE sessions ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMP
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE sessions SET last_seen_at = created_at WHERE last_seen_at IS NULL`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
			ALTER TABLE sessions
				ALTER COLUMN last_seen_at SET DEFAULT NOW(),
				ALTER COLUMN last_seen_at SET NOT NULL
		`); err != nil {
		return err
	}
	// The sweeper deletes on last_seen_at as well as expires_at now.
	_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_sessions_last_seen_at ON sessions(last_seen_at)`)
	return err
}
