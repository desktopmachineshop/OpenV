package invitations

// Workspace invitations (REQ-95 / HAZ-15). A closed deployment has no
// self-service door, so an admin's invitation is how a new person gets an
// account at all: the invitation names the address and the role, and holds
// them until the account that PROVABLY OWNS that address takes them up.
//
// "Provably owns" is the whole design. The link's token proves the invited
// mailbox was read, and every acceptance checks it against the address the
// invitation was issued to (AcceptTokenForEmail); the one exception is a
// sign-in where an identity provider itself asserts the address as verified
// (AcceptAllForProviderVerifiedEmail). Claiming the address — registering
// it, or having an emailed verification link for it confirmed — is not
// proof and grants nothing here.
//
// The token follows the worker-key pattern — random, shown once at creation,
// stored only as a SHA-256 hash — because an invitation link IS a credential:
// it puts its holder into a workspace. It is bounded twice, by an expiry and
// by acceptance, so a link left in a mailbox is not a standing key.

import (
	"errors"
	"fmt"
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
	// ErrEmailMismatch means the link itself is good but was issued to a
	// different address than the account presenting it. It is separate from
	// ErrInvalidToken because the two answers differ: an unusable link is
	// one flat 404 that teaches a prober nothing, while a mismatch is a 403
	// the signed-in person can act on. The invited address does not travel
	// with the error — what to disclose is the caller's decision.
	ErrEmailMismatch = errors.New("this invitation was sent to a different address")
)

// Acceptance is what taking up an invitation did. Role is the role the
// account holds in the workspace once the acceptance is over, which is not
// always the invited role: an account that was already a member keeps the
// role it had (AlreadyMember), because an invitation is an offer of a way
// in, never a way to move somebody between roles.
type Acceptance struct {
	Invitation    *Invitation `json:"invitation"`
	Role          string      `json:"role"`
	AlreadyMember bool        `json:"already_member"`
}

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
	// Replace inserts an invitation, first clearing ANY unaccepted row for
	// the same (org, email) — expired or not — as one atomic write. The
	// pending-uniqueness index covers every unaccepted row, so an expired
	// leftover would otherwise make a re-invite fail, and two admins
	// re-inviting at once would race.
	Replace(inv *Invitation) error
	// ListPending returns the workspace's unaccepted, unexpired invitations.
	ListPending(orgID string, now time.Time) ([]*Invitation, error)
	FindByID(id string) (*Invitation, error)
	FindByTokenHash(hash string) (*Invitation, error)
	// ListPendingForEmail returns every workspace's pending invitations for
	// one address — what an address takes up once a provider has verified it.
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

// Workspaces is the slice of orgs.Service invitations need: the membership
// an acceptance creates, the role the account already holds there — an
// existing membership is never rewritten by an invitation — and the
// workspace itself, which Create reads to refuse an invitation into a
// personal space. Declaring it here rather than taking the whole service
// keeps the dependency three methods wide and the tests free of an org
// service.
type Workspaces interface {
	AddMember(orgID, userID, role string) error
	// RoleInOrg returns the account's current role, "" when it is not a
	// member.
	RoleInOrg(orgID, userID string) (string, error)
	Get(orgID string) (*orgs.Org, error)
}

// Service defines invitation domain logic.
type Service interface {
	// Create issues an invitation and returns it with the raw token (shown
	// once). Re-inviting an address replaces whatever unaccepted invitation
	// it already has, so the newest link is the only one that works. A
	// personal workspace is refused with orgs.ErrPersonalOrgMembers — it
	// cannot have members, so it cannot invite any.
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
	// AcceptTokenForEmail takes up the invitation a raw token names, joining
	// userID to its workspace at the invited role — but only for the address
	// the invitation was issued to. There is deliberately no way to accept a
	// token WITHOUT naming the address: the rule that an invitation converts
	// only for the account that provably owns the invited address is
	// structural here, not a check a caller can forget. A token that reached
	// one mailbox can therefore never join a different account.
	//
	// ErrInvalidToken when the link is unknown, revoked, spent or expired;
	// ErrEmailMismatch when the link is live but was sent elsewhere.
	AcceptTokenForEmail(token, email, userID string) (*Acceptance, error)
	// AcceptAllForProviderVerifiedEmail accepts every pending invitation for
	// an address. Membership is a credential, so the name states the only
	// precondition under which it may be called: an identity provider has
	// asserted this address as verified for the account signing in. Nothing
	// weaker qualifies — not registering the address, and not confirming an
	// emailed verification link, which the address-change flow lets an
	// account point at an address it does not own.
	//
	// One workspace that refuses its membership does not strand the others:
	// the acceptances that succeeded are returned AND the failures are
	// reported, so the caller can log them. A refused invitation stays
	// pending and can be taken up later with its link.
	AcceptAllForProviderVerifiedEmail(email, userID string) ([]*Acceptance, error)
	// PurgeExpired drops expired rows (boot/reaper housekeeping).
	PurgeExpired(now time.Time) error
}

// DefaultService implements Service.
type DefaultService struct {
	repo    Repository
	members Workspaces
	ttl     time.Duration
}

// NewDefaultService creates an invitation service.
func NewDefaultService(repo Repository, members Workspaces) *DefaultService {
	return &DefaultService{repo: repo, members: members, ttl: DefaultTTL}
}

// Create issues an invitation; see Service.
func (s *DefaultService) Create(orgID, email, role string, invitedBy *string) (*Invitation, string, error) {
	email = users.NormalizeEmail(email)
	if email == "" || !strings.Contains(email, "@") {
		return nil, "", ErrInvalidEmail
	}
	if role == "" {
		role = orgs.RoleMember
	}
	if role != orgs.RoleAdmin && role != orgs.RoleMember {
		return nil, "", orgs.ErrInvalidRole
	}
	// A personal workspace cannot have members, so it must not be able to
	// hand out an invitation that promises one. Refused before any row is
	// written, with the same error the membership path uses.
	org, err := s.members.Get(orgID)
	if err != nil {
		return nil, "", err
	}
	if org == nil {
		return nil, "", orgs.ErrNotFound
	}
	if org.OrgType == orgs.TypePersonal {
		return nil, "", orgs.ErrPersonalOrgMembers
	}
	now := time.Now()
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
	// Re-inviting is how an admin resends: the previous link — pending or
	// long expired — is cleared in the same write, so only one live
	// credential per address per workspace ever exists and a re-invite
	// cannot trip the pending-uniqueness index.
	if err := s.repo.Replace(inv); err != nil {
		return nil, "", err
	}
	// Read the row back for the display names (workspace, inviter): they are
	// joined in by the repository, and the invitation email names both.
	if stored, err := s.repo.FindByID(inv.ID); err == nil && stored != nil {
		return stored, token, nil
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
	email = users.NormalizeEmail(email)
	if email == "" {
		return nil, nil
	}
	return s.repo.ListPendingForEmail(email, time.Now())
}

// AcceptTokenForEmail accepts a raw token only for the address it was sent
// to; see the Service interface.
func (s *DefaultService) AcceptTokenForEmail(token, email, userID string) (*Acceptance, error) {
	inv, err := s.Lookup(token)
	if err != nil {
		return nil, err
	}
	if inv.Email != users.NormalizeEmail(email) {
		return nil, ErrEmailMismatch
	}
	return s.accept(inv, userID, time.Now())
}

// AcceptAllForProviderVerifiedEmail accepts every pending invitation for an
// address a provider has verified; see the Service interface.
func (s *DefaultService) AcceptAllForProviderVerifiedEmail(email, userID string) ([]*Acceptance, error) {
	pending, err := s.PendingForEmail(email)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var accepted []*Acceptance
	var failures []error
	for _, inv := range pending {
		acc, err := s.accept(inv, userID, now)
		if err != nil {
			// A workspace that has since been deleted, or refuses the role,
			// must not block the person's other invitations — but it is
			// reported rather than dropped, so the caller can log a
			// membership that did not happen. The row stays pending and the
			// link still works.
			failures = append(failures, fmt.Errorf("invitation %s (workspace %s): %w", inv.ID, inv.OrgID, err))
			continue
		}
		accepted = append(accepted, acc)
	}
	return accepted, errors.Join(failures...)
}

// accept creates the membership and marks the row taken.
//
// Two rules shape it. An existing membership is left exactly as it is: an
// invitation offers a way in, never a move between roles, so an admin who
// follows a later "member" link stays an admin and a workspace's last admin
// can never be demoted through this path. And the writes are ordered
// membership-first, so a failure cannot consume the invitation: if stamping
// the row fails after the member was added, the invitation stays pending and
// the next attempt lands on the already-a-member branch, which is
// idempotent. The reverse order would spend the link and leave no membership
// at all.
//
// Losing the race for the stamp (won == false) still reports the link as
// spent. The membership that was written a moment earlier stands: it is
// exactly the one the token authorized, into the workspace the token names,
// at the role it names.
func (s *DefaultService) accept(inv *Invitation, userID string, now time.Time) (*Acceptance, error) {
	existing, err := s.members.RoleInOrg(inv.OrgID, userID)
	if err != nil {
		return nil, err
	}
	if existing != "" {
		// Already in: take the invitation out of circulation and report the
		// role they actually hold, which is not necessarily the invited one.
		if _, err := s.repo.MarkAccepted(inv.ID, now); err != nil {
			return nil, err
		}
		inv.AcceptedAt = &now
		return &Acceptance{Invitation: inv, Role: existing, AlreadyMember: true}, nil
	}
	if err := s.members.AddMember(inv.OrgID, userID, inv.Role); err != nil {
		return nil, err
	}
	won, err := s.repo.MarkAccepted(inv.ID, now)
	if err != nil {
		return nil, err
	}
	if !won {
		return nil, ErrInvalidToken
	}
	inv.AcceptedAt = &now
	return &Acceptance{Invitation: inv, Role: inv.Role}, nil
}

// PurgeExpired drops expired rows.
func (s *DefaultService) PurgeExpired(now time.Time) error {
	return s.repo.DeleteExpired(now)
}
