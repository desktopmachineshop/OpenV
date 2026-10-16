package postgres

import (
	"database/sql"
	"errors"

	"github.com/openv/requirements-platform/internal/domain/providers/usertokens"
)

// ProviderTokenRepository implements tokens.Repository: a member's sealed
// provider sign-in token, one row per member and provider.
type ProviderTokenRepository struct {
	db *sql.DB
}

// NewProviderTokenRepository creates the repository.
func NewProviderTokenRepository(db *sql.DB) *ProviderTokenRepository {
	return &ProviderTokenRepository{db: db}
}

// Get returns the member's token for a provider, or (nil, nil) when absent.
func (r *ProviderTokenRepository) Get(userID, provider string) (*usertokens.Token, error) {
	t := &usertokens.Token{}
	err := r.db.QueryRow(`
		SELECT user_id::text, provider, sealed, last4, created_at, expires_at
		FROM user_provider_tokens
		WHERE user_id = $1 AND provider = $2
	`, userID, provider).Scan(&t.UserID, &t.Provider, &t.Sealed, &t.Last4, &t.CreatedAt, &t.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

// Upsert stores t, replacing the member's earlier token for the provider.
func (r *ProviderTokenRepository) Upsert(t *usertokens.Token) error {
	_, err := r.db.Exec(`
		INSERT INTO user_provider_tokens (user_id, provider, sealed, last4, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id, provider) DO UPDATE SET
			sealed = EXCLUDED.sealed,
			last4 = EXCLUDED.last4,
			created_at = EXCLUDED.created_at,
			expires_at = EXCLUDED.expires_at
	`, t.UserID, t.Provider, t.Sealed, t.Last4, t.CreatedAt, t.ExpiresAt)
	return err
}

// Delete forgets the member's token for a provider; absent is not an error.
func (r *ProviderTokenRepository) Delete(userID, provider string) error {
	_, err := r.db.Exec(`DELETE FROM user_provider_tokens WHERE user_id = $1 AND provider = $2`, userID, provider)
	return err
}
