package orgs

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// OrgTeam is a named group of people within an organization.
// (Distinct from agent crews, which live in internal/domain/teams.)
type OrgTeam struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"org_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedBy   *string   `json:"created_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	// Members is populated on list/get for display.
	Members []*Member `json:"members,omitempty"`
}

// TeamRepository defines persistence for people-teams.
type TeamRepository interface {
	SaveTeam(t *OrgTeam) error
	UpdateTeam(t *OrgTeam) error
	FindTeamByID(id string) (*OrgTeam, error)
	ListTeams(orgID string) ([]*OrgTeam, error)
	DeleteTeam(id string) error
	AddTeamMember(teamID, userID string) error
	RemoveTeamMember(teamID, userID string) error
	ListTeamMembers(teamID string) ([]*Member, error)
}

// TeamService defines people-team domain logic.
type TeamService interface {
	CreateTeam(orgID, name, description string, createdBy *string) (*OrgTeam, error)
	GetTeam(id string) (*OrgTeam, error)
	ListTeams(orgID string) ([]*OrgTeam, error)
	UpdateTeam(id string, name, description *string) (*OrgTeam, error)
	DeleteTeam(id string) error
	AddTeamMember(teamID, userID string) error
	RemoveTeamMember(teamID, userID string) error
}

// DefaultTeamService implements TeamService.
type DefaultTeamService struct {
	repo TeamRepository
	orgs Service
}

// NewTeamService creates a people-team service.
func NewTeamService(repo TeamRepository, orgService Service) *DefaultTeamService {
	return &DefaultTeamService{repo: repo, orgs: orgService}
}

// CreateTeam creates a people-team within an org.
func (s *DefaultTeamService) CreateTeam(orgID, name, description string, createdBy *string) (*OrgTeam, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("team name is required")
	}
	if _, err := s.orgs.Get(orgID); err != nil {
		return nil, err
	}
	now := time.Now()
	team := &OrgTeam{
		ID:          uuid.New().String(),
		OrgID:       orgID,
		Name:        name,
		Description: description,
		CreatedBy:   createdBy,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.repo.SaveTeam(team); err != nil {
		return nil, err
	}
	return team, nil
}

// GetTeam returns a team with its members.
func (s *DefaultTeamService) GetTeam(id string) (*OrgTeam, error) {
	team, err := s.repo.FindTeamByID(id)
	if err != nil {
		return nil, err
	}
	if team == nil {
		return nil, errors.New("team not found")
	}
	members, err := s.repo.ListTeamMembers(id)
	if err != nil {
		return nil, err
	}
	team.Members = members
	return team, nil
}

// ListTeams returns an org's teams with members populated.
func (s *DefaultTeamService) ListTeams(orgID string) ([]*OrgTeam, error) {
	teams, err := s.repo.ListTeams(orgID)
	if err != nil {
		return nil, err
	}
	for _, team := range teams {
		members, err := s.repo.ListTeamMembers(team.ID)
		if err != nil {
			return nil, err
		}
		team.Members = members
	}
	return teams, nil
}

// UpdateTeam renames/redescribes a team.
func (s *DefaultTeamService) UpdateTeam(id string, name, description *string) (*OrgTeam, error) {
	team, err := s.repo.FindTeamByID(id)
	if err != nil {
		return nil, err
	}
	if team == nil {
		return nil, errors.New("team not found")
	}
	if name != nil && strings.TrimSpace(*name) != "" {
		team.Name = strings.TrimSpace(*name)
	}
	if description != nil {
		team.Description = *description
	}
	team.UpdatedAt = time.Now()
	if err := s.repo.UpdateTeam(team); err != nil {
		return nil, err
	}
	return team, nil
}

// DeleteTeam removes a team (project grants cascade).
func (s *DefaultTeamService) DeleteTeam(id string) error {
	return s.repo.DeleteTeam(id)
}

// AddTeamMember adds an org member to a team.
func (s *DefaultTeamService) AddTeamMember(teamID, userID string) error {
	team, err := s.repo.FindTeamByID(teamID)
	if err != nil {
		return err
	}
	if team == nil {
		return errors.New("team not found")
	}
	member, err := s.orgs.IsMember(team.OrgID, userID)
	if err != nil {
		return err
	}
	if !member {
		return errors.New("user must be a member of the organization first")
	}
	return s.repo.AddTeamMember(teamID, userID)
}

// RemoveTeamMember removes a user from a team.
func (s *DefaultTeamService) RemoveTeamMember(teamID, userID string) error {
	return s.repo.RemoveTeamMember(teamID, userID)
}
