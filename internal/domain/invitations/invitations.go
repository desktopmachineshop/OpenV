package invitations

// Workspace invitations (REQ-95 / HAZ-15). A closed deployment has no
// self-service door, so an admin's invitation is how a new person gets an
// account at all: the invitation names the address and the role, and holds
// them until whoever controls that address signs up or signs in.
//
// The token follows the worker-key pattern — random, shown once at creation,
// stored only as a SHA-256 hash — because an invitation link IS a credential:
// it puts its holder into a workspace. It is bounded twice, by an expiry and
// by acceptance, so a link left in a mailbox is not a standing key.

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// DefaultTTL is how long an invitation stays acceptable.
const DefaultTTL = 7 * 24 * time.Hour

var (
	// ErrNotFound covers an unknown invitation id.
	ErrNotFound = errors.New("invitation not found")
	// ErrInvalidToken covers every way a link can fail — unknown, revoked,
	// spent, expired — with one message, so a probe learns nothing about
	// which token values exist.
	ErrInvalidToken = errors.New("invitation link is invalid or has expired")
	// ErrInvalidEmail is user-facing validation, like orgs.ErrInvalidRole:
	// the handler answers 400 rather than 500.
	ErrInvalidEmail = errors.New("a valid email is required")
)

// Invitation is one pending or accepted invitation to a workspace. Token is
// never stored; the raw value is returned once, by Create.
type Invitation struct {
	ID         string     `json:"id"`
	OrgID      string     `json:"org_id"`
	Email      string     `json:"email"`
	Role       string     `json:"role"`
	TokenHash  string     `json:"-"`
	InvitedBy  *string    `json:"invited_by,omitempty"`
	ExpiresAt  time.Time  `json:"expires_at"`
	AcceptedAt *time.Time `json:"accepted_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`

	// OrgName and InvitedByName are denormalized for display: the invitee
	// sees which workspace, and by whom, before they have any membership to
	// read it from.
	OrgName       string `json:"org_name,omitempty"`
	InvitedByName string `json:"invited_by_name,omitempty"`
}

// Pending reports whether the invitation can still be accepted at now.
func (i *Invitation) Pending(now time.Time) bool {
	return i != nil && i.AcceptedAt == nil && i.ExpiresAt.After(now)
}

// Repository defines invitation persistence. Find methods return (nil, nil)
// when no row matches.
type Repository interface {
	Save(inv *Invitation) error
	// ListPending returns the workspace's unaccepted, unexpired invitations.
	ListPending(orgID string, now time.Time) ([]*Invitation, error)
	FindByID(id string) (*Invitation, error)
	FindByTokenHash(hash string) (*Invitation, error)
	// FindPendingForOrg returns the workspace's pending invitation for an
	// email, if any (the uniqueness the admin UI relies on).
	FindPendingForOrg(orgID, email string, now time.Time) (*Invitation, error)
	// ListPendingForEmail returns every workspace's pending invitations for
	// one address — what a new account accepts on sign-up.
	ListPendingForEmail(email string, now time.Time) ([]*Invitation, error)
	// MarkAccepted stamps accepted_at, and reports whether THIS caller won:
	// false means the row was already accepted, so two concurrent sign-ins
	// cannot both count as the acceptance.
	MarkAccepted(id string, at time.Time) (bool, error)
	Delete(id string) error
	// DeleteExpired removes expired, never-accepted rows; housekeeping only,
	// correctness never depends on it.
	DeleteExpired(before time.Time) error
}

// MemberAdder is the slice of orgs.Service an acceptance needs. Declaring it
// here rather than taking the whole service keeps the dependency one method
// wide and the tests free of an org service.
type MemberAdder interface {
	AddMember(orgID, userID, role string) error
}

// Service defines invitation domain logic.
type Service interface {
	// Create issues an invitation and returns it with the raw token (shown
	// once). Re-inviting an address that already has a pending invitation
	// replaces it, so the newest link is the only one that works.
	Create(orgID, email, role string, invitedBy *string) (*Invitation, string, error)
	// ListPending returns a workspace's live invitations.
	ListPending(orgID string) ([]*Invitation, error)
	// Revoke deletes one invitation, verifying it belongs to the workspace.
	Revoke(orgID, invID string) error
	// Lookup resolves a raw token to its pending invitation, for the sign-up
	// page's preview. ErrInvalidToken when it is not acceptable.
	Lookup(token string) (*Invitation, error)
	// PendingForEmail returns every pending invitation for an address.
	PendingForEmail(email string) ([]*Invitation, error)
	// AcceptToken accepts the invitation a raw token names, joining userID to
	// its workspace at the invited role.
	AcceptToken(token, userID string) (*Invitation, error)
	// AcceptAllForEmail accepts every pending invitation for an address —
	// what registration and a first SSO sign-in call. Best-effort per
	// invitation: one workspace that refuses the membership does not strand
	// the others, and the accepted ones are returned.
	AcceptAllForEmail(email, userID string) ([]*Invitation, error)
	// PurgeExpired drops expired rows (boot/reaper housekeeping).
	PurgeExpired(now time.Time) error
}

// DefaultService implements Service.
type DefaultService struct {
	repo    Repository
	members MemberAdder
	ttl     time.Duration
}

// NewDefaultService creates an invitation service.
func NewDefaultService(repo Repository, members MemberAdder) *DefaultService {
	return &DefaultService{repo: repo, members: members, ttl: DefaultTTL}
}

// NormalizeEmail is the one place an invited address is folded, so a lookup
// by "Dave@Example.com" finds the invitation stored for "dave@example.com".
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Create issues an invitation; see Service.
func (s *DefaultService) Create(orgID, email, role string, invitedBy *string) (*Invitation, string, error) {
	email = NormalizeEmail(email)
	if email == "" || !strings.Contains(email, "@") {
		return nil, "", ErrInvalidEmail
	}
	if role == "" {
		role = orgs.RoleMember
	}
	if role != orgs.RoleAdmin && role != orgs.RoleMember {
		return nil, "", orgs.ErrInvalidRole
	}
	now := time.Now()
	// Re-inviting is how an admin resends: the old link stops working, so
	// only one live credential per address per workspace ever exists.
	if existing, err := s.repo.FindPendingForOrg(orgID, email, now); err == nil && existing != nil {
		if err := s.repo.Delete(existing.ID); err != nil {
			return nil, "", err
		}
	}
	token, err := users.NewToken()
	if err != nil {
		return nil, "", err
	}
	inv := &Invitation{
		ID:        uuid.New().String(),
		OrgID:     orgID,
		Email:     email,
		Role:      role,
		TokenHash: users.HashToken(token),
		InvitedBy: invitedBy,
		ExpiresAt: now.Add(s.ttl),
		CreatedAt: now,
	}
	if err := s.repo.Save(inv); err != nil {
		return nil, "", err
	}
	return inv, token, nil
}

// ListPending returns a workspace's live invitations.
func (s *DefaultService) ListPending(orgID string) ([]*Invitation, error) {
	return s.repo.ListPending(orgID, time.Now())
}

// Revoke deletes one of the workspace's invitations.
func (s *DefaultService) Revoke(orgID, invID string) error {
	inv, err := s.repo.FindByID(invID)
	if err != nil {
		return err
	}
	// An invitation from another workspace is reported missing rather than
	// forbidden: the caller has no business learning that the id exists.
	if inv == nil || inv.OrgID != orgID {
		return ErrNotFound
	}
	return s.repo.Delete(inv.ID)
}

// Lookup resolves a raw token to its pending invitation.
func (s *DefaultService) Lookup(token string) (*Invitation, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrInvalidToken
	}
	inv, err := s.repo.FindByTokenHash(users.HashToken(token))
	if err != nil {
		return nil, err
	}
	if !inv.Pending(time.Now()) {
		return nil, ErrInvalidToken
	}
	return inv, nil
}

// PendingForEmail returns every pending invitation for an address.
func (s *DefaultService) PendingForEmail(email string) ([]*Invitation, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return nil, nil
	}
	return s.repo.ListPendingForEmail(email, time.Now())
}

// AcceptToken accepts the invitation a raw token names.
func (s *DefaultService) AcceptToken(token, userID string) (*Invitation, error) {
	inv, err := s.Lookup(token)
	if err != nil {
		return nil, err
	}
	if err := s.accept(inv, userID, time.Now()); err != nil {
		return nil, err
	}
	return inv, nil
}

// AcceptAllForEmail accepts every pending invitation for an address.
func (s *DefaultService) AcceptAllForEmail(email, userID string) ([]*Invitation, error) {
	pending, err := s.PendingForEmail(email)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var accepted []*Invitation
	for _, inv := range pending {
		if err := s.accept(inv, userID, now); err != nil {
			// A workspace that has since been deleted, or refuses the role,
			// must not block the person's other invitations.
			continue
		}
		accepted = append(accepted, inv)
	}
	return accepted, nil
}

// accept claims the row and creates the membership. The claim comes first:
// if two sign-ins race, only the winner writes, and a membership that fails
// afterwards leaves an accepted invitation rather than a repeatable join.
func (s *DefaultService) accept(inv *Invitation, userID string, now time.Time) error {
	won, err := s.repo.MarkAccepted(inv.ID, now)
	if err != nil {
		return err
	}
	if !won {
		return ErrInvalidToken
	}
	if err := s.members.AddMember(inv.OrgID, userID, inv.Role); err != nil {
		return err
	}
	inv.AcceptedAt = &now
	return nil
}

// PurgeExpired drops expired rows.
func (s *DefaultService) PurgeExpired(now time.Time) error {
	return s.repo.DeleteExpired(now)
}
