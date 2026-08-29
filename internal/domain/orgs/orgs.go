package orgs

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Org roles.
const (
	RoleAdmin  = "admin"
	RoleMember = "member"
)

// Org types.
const (
	TypePersonal = "personal"
	TypeCompany  = "company"
)

var (
	ErrNotFound  = errors.New("organization not found")
	ErrNotMember = errors.New("you are not a member of this organization")

	// ErrInvalidRole flags an unknown role name in a membership write. API
	// handlers use it to tell user-facing validation failures (400) apart
	// from repository failures (500), so wrap it with %w when adding new
	// validations.
	ErrInvalidRole = errors.New("invalid org role")

	// ErrPersonalOrgMembers flags an attempt to add members to a personal
	// workspace — user-facing validation, like ErrInvalidRole.
	ErrPersonalOrgMembers = errors.New("personal workspaces cannot have additional members")
)

// Org is a tenant: a personal space or a company workspace.
type Org struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Slug      string                 `json:"slug"`
	OrgType   string                 `json:"type"`
	Plan      string                 `json:"plan"`
	Limits    map[string]interface{} `json:"limits"`
	CreatedBy *string                `json:"created_by,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
	// Role is the requesting user's role, populated by ListForUser.
	Role string `json:"role,omitempty"`
}

// Member is a user's membership in an org, denormalized for display.
type Member struct {
	OrgID     string    `json:"org_id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	UserName  string    `json:"user_name,omitempty"`
	UserEmail string    `json:"user_email,omitempty"`
	AvatarURL string    `json:"avatar_url,omitempty"`
}

// Repository defines persistence for orgs and memberships.
// Find methods return (nil, nil) when no row matches.
type Repository interface {
	SaveOrg(o *Org) error
	UpdateOrg(o *Org) error
	FindOrgByID(id string) (*Org, error)
	ListOrgsForUser(userID string) ([]*Org, error)
	FindPersonalOrgForUser(userID string) (*Org, error)
	// ListAllOrgIDs returns every organization id (boot-time seeding).
	ListAllOrgIDs() ([]string, error)

	UpsertMember(orgID, userID, role string) error
	RemoveMember(orgID, userID string) error
	MemberRole(orgID, userID string) (string, error) // "" when not a member
	ListMembers(orgID string) ([]*Member, error)
	CountAdmins(orgID string) (int, error)
}

// Service defines org domain logic.
type Service interface {
	CreateOrg(name, orgType string, createdBy string) (*Org, error)
	// EnsurePersonalOrg returns the user's personal org, creating it if
	// missing. Idempotent; used at signup and during backfill.
	EnsurePersonalOrg(userID, displayName string) (*Org, bool, error)
	Get(id string) (*Org, error)
	ListForUser(userID string) ([]*Org, error)
	// ListAll returns every organization id (trusted boot-time callers only).
	ListAll() ([]string, error)
	UpdateOrg(id string, name *string) (*Org, error)

	AddMember(orgID, userID, role string) error
	RemoveMember(orgID, userID string) error
	SetMemberRole(orgID, userID, role string) error
	ListMembers(orgID string) ([]*Member, error)
	RoleInOrg(orgID, userID string) (string, error)
	// IsMember is a convenience wrapper over RoleInOrg.
	IsMember(orgID, userID string) (bool, error)
}

// DefaultService implements Service.
type DefaultService struct {
	repo Repository
}

// NewDefaultService creates an org service.
func NewDefaultService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo}
}

var slugCleaner = regexp.MustCompile(`[^a-z0-9]+`)

func makeSlug(name, id string) string {
	base := strings.Trim(slugCleaner.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if base == "" {
		base = "org"
	}
	if len(base) > 40 {
		base = base[:40]
	}
	return base + "-" + strings.ReplaceAll(id, "-", "")[:8]
}

// CreateOrg creates an org and makes the creator its admin.
func (s *DefaultService) CreateOrg(name, orgType string, createdBy string) (*Org, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("organization name is required")
	}
	if orgType != TypePersonal && orgType != TypeCompany {
		return nil, fmt.Errorf("invalid organization type %q", orgType)
	}
	now := time.Now()
	org := &Org{
		ID:        uuid.New().String(),
		Name:      name,
		OrgType:   orgType,
		Plan:      PlanFree,
		Limits:    map[string]interface{}{},
		CreatedBy: &createdBy,
		CreatedAt: now,
		UpdatedAt: now,
	}
	org.Slug = makeSlug(name, org.ID)
	if err := s.repo.SaveOrg(org); err != nil {
		return nil, err
	}
	if err := s.repo.UpsertMember(org.ID, createdBy, RoleAdmin); err != nil {
		return nil, err
	}
	org.Role = RoleAdmin
	return org, nil
}

// EnsurePersonalOrg returns (org, created, error).
func (s *DefaultService) EnsurePersonalOrg(userID, displayName string) (*Org, bool, error) {
	existing, err := s.repo.FindPersonalOrgForUser(userID)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		return existing, false, nil
	}
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = "Personal"
	}
	org, err := s.CreateOrg(name+"'s Space", TypePersonal, userID)
	if err != nil {
		return nil, false, err
	}
	return org, true, nil
}

// Get returns an org by id.
func (s *DefaultService) Get(id string) (*Org, error) {
	org, err := s.repo.FindOrgByID(id)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, ErrNotFound
	}
	return org, nil
}

// ListForUser returns the user's orgs with their role populated.
func (s *DefaultService) ListForUser(userID string) ([]*Org, error) {
	return s.repo.ListOrgsForUser(userID)
}

// ListAll returns every organization id.
func (s *DefaultService) ListAll() ([]string, error) {
	return s.repo.ListAllOrgIDs()
}

// UpdateOrg renames an org.
func (s *DefaultService) UpdateOrg(id string, name *string) (*Org, error) {
	org, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if name != nil && strings.TrimSpace(*name) != "" {
		org.Name = strings.TrimSpace(*name)
	}
	org.UpdatedAt = time.Now()
	if err := s.repo.UpdateOrg(org); err != nil {
		return nil, err
	}
	return org, nil
}

func validOrgRole(role string) bool {
	return role == RoleAdmin || role == RoleMember
}

// AddMember adds or updates a membership.
func (s *DefaultService) AddMember(orgID, userID, role string) error {
	if !validOrgRole(role) {
		return fmt.Errorf("%w: %q", ErrInvalidRole, role)
	}
	org, err := s.Get(orgID)
	if err != nil {
		return err
	}
	if org.OrgType == TypePersonal {
		return ErrPersonalOrgMembers
	}
	return s.repo.UpsertMember(orgID, userID, role)
}

// RemoveMember removes a member, refusing to remove the last admin.
func (s *DefaultService) RemoveMember(orgID, userID string) error {
	role, err := s.repo.MemberRole(orgID, userID)
	if err != nil {
		return err
	}
	if role == RoleAdmin {
		admins, err := s.repo.CountAdmins(orgID)
		if err != nil {
			return err
		}
		if admins <= 1 {
			return errors.New("cannot remove the last admin of an organization")
		}
	}
	return s.repo.RemoveMember(orgID, userID)
}

// SetMemberRole changes a member's role, refusing to demote the last admin.
func (s *DefaultService) SetMemberRole(orgID, userID, role string) error {
	if !validOrgRole(role) {
		return fmt.Errorf("%w: %q", ErrInvalidRole, role)
	}
	current, err := s.repo.MemberRole(orgID, userID)
	if err != nil {
		return err
	}
	if current == "" {
		return ErrNotMember
	}
	if current == RoleAdmin && role != RoleAdmin {
		admins, err := s.repo.CountAdmins(orgID)
		if err != nil {
			return err
		}
		if admins <= 1 {
			return errors.New("cannot demote the last admin of an organization")
		}
	}
	return s.repo.UpsertMember(orgID, userID, role)
}

// ListMembers returns an org's members.
func (s *DefaultService) ListMembers(orgID string) ([]*Member, error) {
	return s.repo.ListMembers(orgID)
}

// RoleInOrg returns the user's role, "" when not a member.
func (s *DefaultService) RoleInOrg(orgID, userID string) (string, error) {
	return s.repo.MemberRole(orgID, userID)
}

// IsMember reports membership.
func (s *DefaultService) IsMember(orgID, userID string) (bool, error) {
	role, err := s.repo.MemberRole(orgID, userID)
	return role != "", err
}
