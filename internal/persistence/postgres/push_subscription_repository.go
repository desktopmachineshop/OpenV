package postgres

import (
	"database/sql"
	"time"

	"github.com/openv/requirements-platform/internal/domain/pushsubs"
)

// PushSubscriptionRepository implements pushsubs.Repository (REQ-109). Every
// user-facing query is keyed by user_id so a member can only ever see or
// withdraw their own devices; DeleteByEndpoint is the sender's path for a
// subscription the push service reported gone.
type PushSubscriptionRepository struct {
	db *sql.DB
}

// NewPushSubscriptionRepository creates the repository.
func NewPushSubscriptionRepository(db *sql.DB) *PushSubscriptionRepository {
	return &PushSubscriptionRepository{db: db}
}

// Upsert stores one device's subscription, keyed on the unique endpoint. A
// repeat POST of the same endpoint refreshes the keys, the user agent and the
// owner, and clears any failure mark — so the endpoint stays one row however
// often the browser rotates its keys.
//
// The identity of an existing row is NOT overwritten: RETURNING hands back
// the id and created_at the row actually has, and they are written back into
// s. Callers (the handler's 201 body) therefore describe the persisted
// device, not the candidate they built, and re-posting a known endpoint
// answers with the same id GET /me/push-subscriptions lists.
func (r *PushSubscriptionRepository) Upsert(s *pushsubs.Subscription) error {
	return r.db.QueryRow(`
		INSERT INTO push_subscriptions (id, user_id, endpoint, p256dh, auth, user_agent, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (endpoint) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			p256dh = EXCLUDED.p256dh,
			auth = EXCLUDED.auth,
			user_agent = EXCLUDED.user_agent,
			failed_at = NULL
		RETURNING id, created_at
	`, s.ID, s.UserID, s.Endpoint, s.P256dh, s.Auth, s.UserAgent, s.CreatedAt).Scan(&s.ID, &s.CreatedAt)
}

const pushSubscriptionColumns = `id, user_id, endpoint, p256dh, auth, COALESCE(user_agent, ''), created_at, last_used_at, failed_at`

func scanPushSubscription(row interface{ Scan(...interface{}) error }) (*pushsubs.Subscription, error) {
	s := new(pushsubs.Subscription)
	var lastUsed, failed sql.NullTime
	if err := row.Scan(&s.ID, &s.UserID, &s.Endpoint, &s.P256dh, &s.Auth, &s.UserAgent, &s.CreatedAt, &lastUsed, &failed); err != nil {
		return nil, err
	}
	if lastUsed.Valid {
		t := lastUsed.Time
		s.LastUsedAt = &t
	}
	if failed.Valid {
		t := failed.Time
		s.FailedAt = &t
	}
	return s, nil
}

// ListForUser returns the member's devices, newest first.
func (r *PushSubscriptionRepository) ListForUser(userID string) ([]*pushsubs.Subscription, error) {
	rows, err := r.db.Query(`
		SELECT `+pushSubscriptionColumns+`
		FROM push_subscriptions
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*pushsubs.Subscription
	for rows.Next() {
		s, err := scanPushSubscription(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	return list, rows.Err()
}

// DeleteForUser removes one endpoint belonging to that user only.
func (r *PushSubscriptionRepository) DeleteForUser(userID, endpoint string) (int64, error) {
	res, err := r.db.Exec(
		`DELETE FROM push_subscriptions WHERE user_id = $1 AND endpoint = $2`, userID, endpoint)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteByEndpoint removes a subscription the push service reported gone.
func (r *PushSubscriptionRepository) DeleteByEndpoint(endpoint string) (int64, error) {
	res, err := r.db.Exec(`DELETE FROM push_subscriptions WHERE endpoint = $1`, endpoint)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// MarkUsed records a successful send; a success also clears the failure mark.
func (r *PushSubscriptionRepository) MarkUsed(id string, at time.Time) error {
	_, err := r.db.Exec(
		`UPDATE push_subscriptions SET last_used_at = $2, failed_at = NULL WHERE id = $1`, id, at)
	return err
}

// MarkFailed records a send failure, keeping the subscription: a push service
// can be transiently unreachable, and only a "gone" answer is final.
func (r *PushSubscriptionRepository) MarkFailed(id string, at time.Time) error {
	_, err := r.db.Exec(
		`UPDATE push_subscriptions SET failed_at = $2 WHERE id = $1`, id, at)
	return err
}
