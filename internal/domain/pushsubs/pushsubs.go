// Package pushsubs holds the web push subscription domain (REQ-109): one row
// per DEVICE a member has opted in on, holding the browser push service's
// endpoint and the two keys needed to encrypt a payload for it (RFC 8291).
//
// The shape mirrors internal/domain/notifications: a Repository the postgres
// package implements, a Service façade the API handlers and the notify
// fan-out depend on, and no transport concerns — sending lives in
// internal/notify (push.go), which owns the VAPID keys and the HTTP calls.
//
// Every read and write is scoped by user id in the query itself, so a caller
// can never touch another member's devices. The one exception is
// DeleteByEndpoint, used by the sender when a push service reports a
// subscription gone (404/410): that is keyed on the endpoint alone because it
// is the sender, not a user, acting on the push service's word.
package pushsubs

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Subscription is one device's push registration.
type Subscription struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
	// Endpoint is the push service URL for this device. Globally unique; a
	// re-subscription on the same device collides on it and updates the keys.
	Endpoint string `json:"endpoint"`
	// P256dh and Auth are the base64url client keys from
	// PushSubscription.getKey(). They are the encryption secret for this
	// device and are NEVER returned by the API.
	P256dh string `json:"-"`
	Auth   string `json:"-"`
	// UserAgent is whatever the browser reported when subscribing, kept only
	// so the member can tell their devices apart in the settings list.
	UserAgent string    `json:"user_agent,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	// LastUsedAt is the last successful send; nil until one succeeds.
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	// FailedAt is the last send failure that was NOT a "gone" answer (those
	// delete the row). A later success clears it.
	FailedAt *time.Time `json:"failed_at,omitempty"`
}

// New builds a subscription with a fresh id and creation timestamp.
func New(userID, endpoint, p256dh, auth, userAgent string) *Subscription {
	return &Subscription{
		ID:        uuid.New().String(),
		UserID:    userID,
		Endpoint:  strings.TrimSpace(endpoint),
		P256dh:    strings.TrimSpace(p256dh),
		Auth:      strings.TrimSpace(auth),
		UserAgent: strings.TrimSpace(userAgent),
		CreatedAt: time.Now(),
	}
}

// Repository persists push subscriptions.
type Repository interface {
	// Upsert stores a subscription, keyed on its endpoint: an endpoint that
	// already exists has its keys, user agent and owner refreshed and its
	// failed_at cleared, so re-posting the same device is idempotent.
	Upsert(s *Subscription) error
	// ListForUser returns the member's devices, newest first.
	ListForUser(userID string) ([]*Subscription, error)
	// DeleteForUser removes one endpoint belonging to that user. Rows of
	// other users are untouched. Returns rows deleted.
	DeleteForUser(userID, endpoint string) (int64, error)
	// DeleteByEndpoint removes a subscription the push service reported gone,
	// whoever owns it. Returns rows deleted.
	DeleteByEndpoint(endpoint string) (int64, error)
	// MarkUsed records a successful send and clears any failure mark.
	MarkUsed(id string, at time.Time) error
	// MarkFailed records a send failure without removing the subscription.
	MarkFailed(id string, at time.Time) error
}

// Service is the thin domain façade over the repository.
type Service interface {
	// Subscribe stores or refreshes one device's subscription.
	Subscribe(s *Subscription) error
	ListForUser(userID string) ([]*Subscription, error)
	// Unsubscribe withdraws one of the caller's own devices.
	Unsubscribe(userID, endpoint string) (int64, error)
	// Forget drops a subscription the push service reported gone.
	Forget(endpoint string) (int64, error)
	MarkUsed(id string, at time.Time) error
	MarkFailed(id string, at time.Time) error
}

// DefaultService implements Service.
type DefaultService struct {
	repo Repository
}

// NewDefaultService creates the service.
func NewDefaultService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo}
}

func (s *DefaultService) Subscribe(sub *Subscription) error { return s.repo.Upsert(sub) }

func (s *DefaultService) ListForUser(userID string) ([]*Subscription, error) {
	return s.repo.ListForUser(userID)
}

func (s *DefaultService) Unsubscribe(userID, endpoint string) (int64, error) {
	return s.repo.DeleteForUser(userID, endpoint)
}

func (s *DefaultService) Forget(endpoint string) (int64, error) {
	return s.repo.DeleteByEndpoint(endpoint)
}

func (s *DefaultService) MarkUsed(id string, at time.Time) error { return s.repo.MarkUsed(id, at) }

func (s *DefaultService) MarkFailed(id string, at time.Time) error { return s.repo.MarkFailed(id, at) }
