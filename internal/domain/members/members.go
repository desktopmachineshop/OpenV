package members

import (
	"errors"
	"time"
)

// Roles, in ascending order of privilege.
const (
	RoleViewer = "viewer"
	RoleEditor = "editor"
	RoleOwner  = "owner"
)

var roleRank = map[string]int{
	RoleViewer: 1,
	RoleEditor: 2,
	RoleOwner:  3,
}

// ErrForbidden is returned when a user lacks the required project role.
var ErrForbidden = errors.New("you do not have access to this project")

// ValidRole reports whether the given role name exists.
func ValidRole(role string) bool {
	_, ok := roleRank[role]
	return ok
}

// RoleAtLeast reports whether have meets or exceeds want.
func RoleAtLeast(have, want string) bool {
	return roleRank[have] >= roleRank[want]
}

// Member is a user's membership in a project.
type Member struct {
	ProjectID string    `json:"project_id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	// Denormalized for display.
	UserName  string `json:"user_name,omitempty"`
	UserEmail string `json:"user_email,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// TeamGrant is a project role granted to a people-team.
type TeamGrant struct {
	ProjectID string    `json:"project_id"`
	OrgTeamID string    `json:"org_team_id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	TeamName  string    `json:"team_name,omitempty"`
}

// Repository defines persistence for project membership.
type Repository interface {
	Upsert(m *Member) error
	Remove(projectID, userID string) error
	Find(projectID, userID string) (*Member, error)
	ListByProject(projectID string) ([]*Member, error)
	// ListProjectIDsForUser includes projects reachable via team grants.
	ListProjectIDsForUser(userID string) ([]string, error)
	// RolesFor returns every role the user holds on the project — the
	// direct grant plus any roles inherited through people-team grants.
	RolesFor(projectID, userID string) ([]string, error)

	UpsertTeamGrant(g *TeamGrant) error
	RemoveTeamGrant(projectID, orgTeamID string) error
	ListTeamGrants(projectID string) ([]*TeamGrant, error)
}

// Service defines membership domain logic.
type Service interface {
	AddMember(projectID, userID, role string) error
	RemoveMember(projectID, userID string) error
	SetRole(projectID, userID, role string) error
	ListMembers(projectID string) ([]*Member, error)
	ProjectIDsForUser(userID string) ([]string, error)
	// RoleFor returns the user's DIRECT role in the project ("" if none).
	// Admin users are handled by the caller, not here.
	RoleFor(projectID, userID string) (string, error)
	// EffectiveRole returns the strongest role the user holds on the
	// project via direct or team grants ("" when none).
	EffectiveRole(projectID, userID string) (string, error)

	GrantTeam(projectID, orgTeamID, role string) error
	RevokeTeam(projectID, orgTeamID string) error
	ListTeamGrants(projectID string) ([]*TeamGrant, error)
}

// DefaultService implements Service.
type DefaultService struct {
	repo Repository
}

// NewDefaultService creates a new membership service.
func NewDefaultService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo}
}

// AddMember adds or updates a membership.
func (s *DefaultService) AddMember(projectID, userID, role string) error {
	if !ValidRole(role) {
		return errors.New("invalid role: " + role)
	}
	return s.repo.Upsert(&Member{ProjectID: projectID, UserID: userID, Role: role, CreatedAt: time.Now()})
}

// RemoveMember removes a membership.
func (s *DefaultService) RemoveMember(projectID, userID string) error {
	return s.repo.Remove(projectID, userID)
}

// SetRole updates an existing membership's role.
func (s *DefaultService) SetRole(projectID, userID, role string) error {
	return s.AddMember(projectID, userID, role)
}

// ListMembers returns all members of a project.
func (s *DefaultService) ListMembers(projectID string) ([]*Member, error) {
	return s.repo.ListByProject(projectID)
}

// ProjectIDsForUser returns project ids the user is a member of.
func (s *DefaultService) ProjectIDsForUser(userID string) ([]string, error) {
	return s.repo.ListProjectIDsForUser(userID)
}

// RoleFor returns the user's direct role in a project, or "" if none.
func (s *DefaultService) RoleFor(projectID, userID string) (string, error) {
	m, err := s.repo.Find(projectID, userID)
	if err != nil {
		return "", err
	}
	if m == nil {
		return "", nil
	}
	return m.Role, nil
}

// EffectiveRole returns the highest-ranked role from direct and team grants.
func (s *DefaultService) EffectiveRole(projectID, userID string) (string, error) {
	roles, err := s.repo.RolesFor(projectID, userID)
	if err != nil {
		return "", err
	}
	best := ""
	for _, role := range roles {
		if roleRank[role] > roleRank[best] {
			best = role
		}
	}
	return best, nil
}

// GrantTeam grants a project role to a people-team.
func (s *DefaultService) GrantTeam(projectID, orgTeamID, role string) error {
	if !ValidRole(role) {
		return errors.New("invalid role: " + role)
	}
	return s.repo.UpsertTeamGrant(&TeamGrant{ProjectID: projectID, OrgTeamID: orgTeamID, Role: role, CreatedAt: time.Now()})
}

// RevokeTeam removes a team's project grant.
func (s *DefaultService) RevokeTeam(projectID, orgTeamID string) error {
	return s.repo.RemoveTeamGrant(projectID, orgTeamID)
}

// ListTeamGrants returns a project's team grants.
func (s *DefaultService) ListTeamGrants(projectID string) ([]*TeamGrant, error) {
	return s.repo.ListTeamGrants(projectID)
}
