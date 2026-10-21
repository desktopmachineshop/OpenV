package postgres

import (
	"time"

	"github.com/openv/requirements-platform/internal/domain/users"
)

// Linked sign-in methods and their pending link requests (migration 0066,
// users/identity_link.go).

// HasIdentity reports whether provider is a linked sign-in method of the
// account.
func (r *UserRepository) HasIdentity(userID, provider string) (bool, error) {
	var linked bool
	err := r.db.QueryRow(`
		SELECT EXISTS (SELECT 1 FROM user_identities WHERE user_id = $1 AND provider = $2)
	`, userID, provider).Scan(&linked)
	if malformedID(err) {
		return false, nil
	}
	return linked, err
}

// SaveIdentityLink stores a link request after discarding the account's
// unused pending ones, so one is live per account.
func (r *UserRepository) SaveIdentityLink(l *users.IdentityLink) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM identity_link_requests WHERE user_id = $1 AND NOT used`, l.UserID); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO identity_link_requests (id, user_id, provider, token_hash, expires_at, used, created_at)
		VALUES ($1, $2, $3, $4, $5, FALSE, $6)
	`, l.ID, l.UserID, l.Provider, l.TokenHash, l.ExpiresAt, l.CreatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

// FindIdentityLink returns a live link request without spending it.
func (r *UserRepository) FindIdentityLink(tokenHash string, now time.Time) (*users.IdentityLink, error) {
	l := &users.IdentityLink{TokenHash: tokenHash}
	err := r.db.QueryRow(`
		SELECT id, user_id, provider, expires_at, created_at FROM identity_link_requests
		WHERE token_hash = $1 AND NOT used AND expires_at > $2
	`, tokenHash, now).Scan(&l.ID, &l.UserID, &l.Provider, &l.ExpiresAt, &l.CreatedAt)
	if noRow(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return l, nil
}

// LinkIdentity spends a link request and records its provider in one
// transaction. The UPDATE ... RETURNING is the single-use guard, as in
// ConsumePasswordReset.
func (r *UserRepository) LinkIdentity(tokenHash string, now time.Time) (*users.IdentityLink, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	l := &users.IdentityLink{TokenHash: tokenHash, Used: true}
	err = tx.QueryRow(`
		UPDATE identity_link_requests SET used = TRUE
		WHERE token_hash = $1 AND NOT used AND expires_at > $2
		RETURNING id, user_id, provider, expires_at, created_at
	`, tokenHash, now).Scan(&l.ID, &l.UserID, &l.Provider, &l.ExpiresAt, &l.CreatedAt)
	if noRow(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`
		INSERT INTO user_identities (user_id, provider, created_at) VALUES ($1, $2, $3)
		ON CONFLICT (user_id, provider) DO NOTHING
	`, l.UserID, l.Provider, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return l, nil
}

// DeleteSpentIdentityLinks removes used link requests and requests that
// expired before cutoff, by the rules of DeleteSpentEmailVerifications:
// housekeeping for the reaper, a live request never removed, cutoff taken to
// UTC.
func (r *UserRepository) DeleteSpentIdentityLinks(cutoff time.Time) error {
	_, err := r.db.Exec(`DELETE FROM identity_link_requests WHERE used OR expires_at < $1`, cutoff.UTC())
	return err
}
