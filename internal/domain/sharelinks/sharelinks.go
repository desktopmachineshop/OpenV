// Package sharelinks lets a project be opened from a link (REQ-149,
// REQ-150).
//
// Two kinds of link exist. A public link shows the live project, read only,
// to anyone who holds it, no account needed: the way a specification is
// handed to a customer or an assessor. A reviewer link is for someone who
// should also be able to leave comments, notes and mentions without changing
// any artifact text: opening it signed in grants the reviewer role on the
// project, and the person then uses the ordinary app.
//
// A link's token is a credential. It is random, shown once when the link is
// created, stored as a SHA-256 hash only, and can be revoked or given an
// expiry. Contributor access is not a link: it stays a membership an owner
// grants by name.
package sharelinks

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/users"
)

// Roles a link can carry.
const (
	RolePublic   = "public"
	RoleReviewer = "reviewer"
)

var (
	ErrInvalidRole  = errors.New("a share link is public or reviewer")
	ErrInvalidToken = errors.New("share link not found")
	ErrNotFound     = errors.New("share link not found")
	// ErrInvalidExpiry refuses an expiry outside years 1 to 9999 in UTC.
	// The expiry is stored as an instant and read back in UTC (migration
	// 0051), and Go writes no JSON time after year 9999 or before year 0,
	// so one sent as 9999-12-31T23:00:00-05:00, year 10000 in UTC, would
	// leave the project's list of links unanswerable for good: a link is
	// revoked, never deleted. Year 0, PostgreSQL's 1 BC, goes with them.
	ErrInvalidExpiry = errors.New("expires_at must fall between years 1 and 9999 in UTC")
)

// Link is one share link. The token itself is never stored.
type Link struct {
	ID        string     `json:"id"`
	ProjectID string     `json:"project_id"`
	Role      string     `json:"role"`
	Label     string     `json:"label"`
	CreatedBy *string    `json:"created_by,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// Usable reports whether the link still opens anything at now.
func (l *Link) Usable(now time.Time) bool {
	if l == nil || l.RevokedAt != nil {
		return false
	}
	if l.ExpiresAt != nil && !now.Before(*l.ExpiresAt) {
		return false
	}
	return true
}

// Repository is the persistence a share link needs.
type Repository interface {
	Create(link *Link, tokenHash string) error
	Get(id string) (*Link, error)
	ListByProject(projectID string) ([]*Link, error)
	FindByTokenHash(hash string) (*Link, error)
	Revoke(id string, at time.Time) error
}

// Service mints, lists, revokes and resolves links.
type Service interface {
	// Create returns the link and its raw token, the one time it is shown.
	Create(projectID, role, label string, createdBy *string, expiresAt *time.Time) (*Link, string, error)
	Get(id string) (*Link, error)
	List(projectID string) ([]*Link, error)
	Revoke(id string) error
	// Resolve returns the usable link a raw token opens; ErrInvalidToken
	// for anything else, one message so a guess learns nothing.
	Resolve(token string) (*Link, error)
}

// DefaultService implements Service.
type DefaultService struct {
	repo Repository
	now  func() time.Time
}

// NewService creates a service.
func NewService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo, now: time.Now}
}

// ValidRole reports whether role names a link kind.
func ValidRole(role string) bool { return role == RolePublic || role == RoleReviewer }

// validExpiry reports whether an expiry, if any, falls in years 1 to 9999
// in UTC, the years it can be read back and listed in (ErrInvalidExpiry).
func validExpiry(expiresAt *time.Time) bool {
	if expiresAt == nil {
		return true
	}
	y := expiresAt.UTC().Year()
	return y >= 1 && y <= 9999
}

// Create implements Service.
func (s *DefaultService) Create(projectID, role, label string, createdBy *string, expiresAt *time.Time) (*Link, string, error) {
	if !ValidRole(role) {
		return nil, "", ErrInvalidRole
	}
	// The expiry is stored to the microsecond, so it is checked as it will
	// be stored: a finer one that Postgres would round into year 10000
	// could not be listed. Truncating closes the link no later than sent.
	if expiresAt != nil {
		e := expiresAt.Truncate(time.Microsecond)
		expiresAt = &e
	}
	if !validExpiry(expiresAt) {
		return nil, "", ErrInvalidExpiry
	}
	token, err := users.NewToken()
	if err != nil {
		return nil, "", err
	}
	link := &Link{
		ID:        uuid.New().String(),
		ProjectID: projectID,
		Role:      role,
		Label:     strings.TrimSpace(label),
		CreatedBy: createdBy,
		CreatedAt: s.now(),
		ExpiresAt: expiresAt,
	}
	if err := s.repo.Create(link, users.HashToken(token)); err != nil {
		return nil, "", err
	}
	return link, token, nil
}

// Get implements Service.
func (s *DefaultService) Get(id string) (*Link, error) { return s.repo.Get(id) }

// List implements Service.
func (s *DefaultService) List(projectID string) ([]*Link, error) {
	return s.repo.ListByProject(projectID)
}

// Revoke implements Service.
func (s *DefaultService) Revoke(id string) error { return s.repo.Revoke(id, s.now()) }

// Resolve implements Service.
func (s *DefaultService) Resolve(token string) (*Link, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrInvalidToken
	}
	link, err := s.repo.FindByTokenHash(users.HashToken(token))
	if err != nil || link == nil || !link.Usable(s.now()) {
		return nil, ErrInvalidToken
	}
	return link, nil
}
