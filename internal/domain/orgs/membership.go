// Memberships: who belongs to a workspace, and with which role.

package orgs

import (
	"errors"
	"fmt"
	"time"
)

// Org roles.
const (
	RoleAdmin  = "admin"
	RoleMember = "member"
)

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

// Membership is the slice of Service for who belongs to a workspace, and
// with which role.
type Membership interface {
	AddMember(orgID, userID, role string) error
	RemoveMember(orgID, userID string) error
	SetMemberRole(orgID, userID, role string) error
	ListMembers(orgID string) ([]*Member, error)
	RoleInOrg(orgID, userID string) (string, error)
	// RoleInOrgAny is RoleInOrg including deleted workspaces (restore path).
	RoleInOrgAny(orgID, userID string) (string, error)
	// IsMember is a convenience wrapper over RoleInOrg.
	IsMember(orgID, userID string) (bool, error)
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

// RemoveMember removes a member, refusing the last admin and an account that
// is not a member (ErrNotMember, which the store also answers to the second
// of two removals that both passed the role read).
func (s *DefaultService) RemoveMember(orgID, userID string) error {
	role, err := s.repo.MemberRole(orgID, userID)
	if err != nil {
		return err
	}
	if role == "" {
		return ErrNotMember
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
			return ErrLastAdmin
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

// RoleInOrgAny returns the user's role including deleted workspaces.
func (s *DefaultService) RoleInOrgAny(orgID, userID string) (string, error) {
	return s.repo.MemberRoleAny(orgID, userID)
}

// IsMember reports membership.
func (s *DefaultService) IsMember(orgID, userID string) (bool, error) {
	role, err := s.repo.MemberRole(orgID, userID)
	return role != "", err
}
