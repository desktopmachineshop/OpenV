package postgres

import (
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"

	"github.com/openv/requirements-platform/internal/domain/users"
)

// UserRepository implements users.Repository.
type UserRepository struct {
	db *sql.DB
}

// NewUserRepository creates a new user repository.
func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

const userColumns = `id, email, name, avatar_url, auth_provider, COALESCE(password_hash, ''), is_admin, COALESCE(email_notifications, TRUE), COALESCE(push_notifications, FALSE), COALESCE(email_verified, FALSE), email_verified_at, created_at, updated_at`

func scanUser(row interface{ Scan(...interface{}) error }) (*users.User, error) {
	u := new(users.User)
	var verifiedAt sql.NullTime
	err := row.Scan(&u.ID, &u.Email, &u.Name, &u.AvatarURL, &u.AuthProvider, &u.PasswordHash, &u.IsAdmin, &u.EmailNotifications, &u.PushNotifications, &u.EmailVerified, &verifiedAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if verifiedAt.Valid {
		t := verifiedAt.Time
		u.EmailVerifiedAt = &t
	}
	return u, nil
}

// SaveUser inserts a user.
func (r *UserRepository) SaveUser(u *users.User) error {
	_, err := r.db.Exec(`
		INSERT INTO users (id, email, name, avatar_url, auth_provider, password_hash, is_admin, email_verified, email_verified_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, $9, $10, $11)
	`, u.ID, u.Email, u.Name, u.AvatarURL, u.AuthProvider, u.PasswordHash, u.IsAdmin, u.EmailVerified, u.EmailVerifiedAt, u.CreatedAt, u.UpdatedAt)
	return err
}

// UpdateUser updates mutable user fields. It deliberately leaves the
// verification columns alone: they only move through
// ConsumeEmailVerification, so a stale struct can never undo a confirm.
func (r *UserRepository) UpdateUser(u *users.User) error {
	_, err := r.db.Exec(`
		UPDATE users SET name = $2, avatar_url = $3, auth_provider = $4, password_hash = NULLIF($5, ''), is_admin = $6, updated_at = $7
		WHERE id = $1
	`, u.ID, u.Name, u.AvatarURL, u.AuthProvider, u.PasswordHash, u.IsAdmin, u.UpdatedAt)
	return err
}

// FindUserByEmail returns the user with the given email, or nil.
func (r *UserRepository) FindUserByEmail(email string) (*users.User, error) {
	row := r.db.QueryRow(`SELECT `+userColumns+` FROM users WHERE LOWER(email) = LOWER($1)`, email)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

// FindUserByID returns the user with the given id, or nil.
func (r *UserRepository) FindUserByID(id string) (*users.User, error) {
	row := r.db.QueryRow(`SELECT `+userColumns+` FROM users WHERE id = $1`, id)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

// ListUsers returns all users ordered by name.
func (r *UserRepository) ListUsers() ([]*users.User, error) {
	rows, err := r.db.Query(`SELECT ` + userColumns + ` FROM users ORDER BY name, email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*users.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, u)
	}
	return result, rows.Err()
}

// SetEmailNotifications flips one user's email-notification opt-out.
func (r *UserRepository) SetEmailNotifications(userID string, enabled bool) error {
	_, err := r.db.Exec(
		`UPDATE users SET email_notifications = $2, updated_at = NOW() WHERE id = $1`,
		userID, enabled)
	return err
}

// SetPushNotifications flips one user's web-push opt-in.
func (r *UserRepository) SetPushNotifications(userID string, enabled bool) error {
	_, err := r.db.Exec(
		`UPDATE users SET push_notifications = $2, updated_at = NOW() WHERE id = $1`,
		userID, enabled)
	return err
}

// SaveEmailVerification stores a link after discarding the user's unused
// pending ones, so one link is live per account.
func (r *UserRepository) SaveEmailVerification(v *users.EmailVerification) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM email_verifications WHERE user_id = $1 AND NOT used`, v.UserID); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO email_verifications (id, user_id, email, token_hash, expires_at, used, created_at)
		VALUES ($1, $2, $3, $4, $5, FALSE, $6)
	`, v.ID, v.UserID, v.Email, v.TokenHash, v.ExpiresAt, v.CreatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

// ConsumeEmailVerification spends a link and marks its user verified in one
// transaction. The UPDATE ... RETURNING is the single-use guard: two racing
// confirms of the same link see one row flip and one no-op. A unique
// violation on the users email index means the link's address was claimed
// by another account since the link was issued; the transaction rolls back
// and the caller sees ErrEmailTaken, leaving the user unverified.
func (r *UserRepository) ConsumeEmailVerification(tokenHash string, now time.Time) (*users.User, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var userID, email string
	err = tx.QueryRow(`
		UPDATE email_verifications SET used = TRUE
		WHERE token_hash = $1 AND NOT used AND expires_at > $2
		RETURNING user_id, email
	`, tokenHash, now).Scan(&userID, &email)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`
		UPDATE users SET email = $2, email_verified = TRUE, email_verified_at = $3, updated_at = $3
		WHERE id = $1
	`, userID, email, now); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return nil, users.ErrEmailTaken
		}
		return nil, err
	}
	u, err := scanUser(tx.QueryRow(`SELECT `+userColumns+` FROM users WHERE id = $1`, userID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return u, nil
}

// DeleteSpentEmailVerifications removes used links and links that expired
// before cutoff; housekeeping only, correctness never depends on it.
func (r *UserRepository) DeleteSpentEmailVerifications(cutoff time.Time) error {
	_, err := r.db.Exec(`DELETE FROM email_verifications WHERE used OR expires_at < $1`, cutoff)
	return err
}

// CountUsers returns the total number of users.
func (r *UserRepository) CountUsers() (int, error) {
	var count int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

// SaveSession inserts a session.
func (r *UserRepository) SaveSession(s *users.Session) error {
	_, err := r.db.Exec(`
		INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, s.ID, s.UserID, s.TokenHash, s.ExpiresAt, s.CreatedAt, s.LastSeenAt)
	return err
}

// FindSessionByTokenHash returns the session with the given token hash, or nil.
func (r *UserRepository) FindSessionByTokenHash(hash string) (*users.Session, error) {
	s := new(users.Session)
	err := r.db.QueryRow(`
		SELECT id, user_id, token_hash, COALESCE(active_org_id::text, ''), expires_at, created_at, last_seen_at
		FROM sessions WHERE token_hash = $1
	`, hash).Scan(&s.ID, &s.UserID, &s.TokenHash, &s.ActiveOrgID, &s.ExpiresAt, &s.CreatedAt, &s.LastSeenAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

// SetSessionActiveOrg persists a session's default workspace.
func (r *UserRepository) SetSessionActiveOrg(id string, orgID string) error {
	_, err := r.db.Exec(`UPDATE sessions SET active_org_id = NULLIF($2, '')::uuid WHERE id = $1`, id, orgID)
	return err
}

// TouchSession updates last_seen_at.
func (r *UserRepository) TouchSession(id string, lastSeen time.Time) error {
	_, err := r.db.Exec(`UPDATE sessions SET last_seen_at = $2 WHERE id = $1`, id, lastSeen)
	return err
}

// DeleteSession removes a session.
func (r *UserRepository) DeleteSession(id string) error {
	_, err := r.db.Exec(`DELETE FROM sessions WHERE id = $1`, id)
	return err
}

// DeleteExpiredSessions removes sessions past their expiry.
func (r *UserRepository) DeleteExpiredSessions(now time.Time) error {
	_, err := r.db.Exec(`DELETE FROM sessions WHERE expires_at < $1`, now)
	return err
}
