package projects

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Agent auth modes: how agent runs launched in this project authenticate
// with their AI provider. Either way, runs still execute on a runner (the
// launcher's Agent Connector / agentd) — this only picks the credential.
const (
	// AgentAuthUserAccount uses each member's own local CLI sign-in
	// (subscription login) on the machine running their runner.
	AgentAuthUserAccount = "user-account"
	// AgentAuthAPIKey overrides local sign-ins with the workspace's API key
	// (from the provider setting's api_key_env on the runner host).
	AgentAuthAPIKey = "api-key"
)

// ValidAgentAuth reports whether the value is a known agent auth mode.
func ValidAgentAuth(mode string) bool {
	return mode == AgentAuthUserAccount || mode == AgentAuthAPIKey
}

// Project represents a project in the system
type Project struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"org_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	AgentAuth   string    `json:"agent_auth"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateProjectRequest represents the request to create a project
type CreateProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	AgentAuth   string `json:"agent_auth"`
}

// UpdateProjectRequest represents the request to update a project.
// Empty fields are left unchanged.
type UpdateProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	AgentAuth   string `json:"agent_auth"`
}

// NewProject creates a new project
func NewProject(req CreateProjectRequest) *Project {
	auth := req.AgentAuth
	if auth == "" {
		auth = AgentAuthUserAccount
	}
	return &Project{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
		AgentAuth:   auth,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

// Service defines the project service interface
type Service interface {
	CreateProject(project *Project) error
	GetProject(id string) (*Project, error)
	ListProjects() ([]*Project, error)
	// ListProjectsByOrg returns the projects in one workspace, scoped in SQL.
	// It fails closed: an empty orgID yields no projects.
	ListProjectsByOrg(orgID string) ([]*Project, error)
	UpdateProject(id string, req UpdateProjectRequest) (*Project, error)
	DeleteProject(id string) error
}

// DefaultService provides default implementation of Service
type DefaultService struct {
	repository Repository
}

// NewService creates a new project service
func NewService(repository Repository) Service {
	return &DefaultService{
		repository: repository,
	}
}

// CreateProject creates a new project
func (s *DefaultService) CreateProject(project *Project) error {
	if project.AgentAuth == "" {
		project.AgentAuth = AgentAuthUserAccount
	}
	if !ValidAgentAuth(project.AgentAuth) {
		return fmt.Errorf("invalid agent_auth %q", project.AgentAuth)
	}
	return s.repository.Create(project)
}

// GetProject retrieves a project by ID
func (s *DefaultService) GetProject(id string) (*Project, error) {
	return s.repository.GetByID(id)
}

// ListProjects retrieves all projects
func (s *DefaultService) ListProjects() ([]*Project, error) {
	return s.repository.GetAll()
}

// ListProjectsByOrg retrieves the projects in one workspace. It fails closed:
// an empty orgID yields no projects.
func (s *DefaultService) ListProjectsByOrg(orgID string) ([]*Project, error) {
	return s.repository.ListByOrg(orgID)
}

// UpdateProject updates a project. Empty request fields keep their current
// values so partial updates (e.g. only agent_auth) don't wipe the rest.
func (s *DefaultService) UpdateProject(id string, req UpdateProjectRequest) (*Project, error) {
	project, err := s.repository.GetByID(id)
	if err != nil {
		return nil, err
	}

	if req.Name != "" {
		project.Name = req.Name
	}
	if req.Description != "" {
		project.Description = req.Description
	}
	if req.AgentAuth != "" {
		if !ValidAgentAuth(req.AgentAuth) {
			return nil, fmt.Errorf("invalid agent_auth %q", req.AgentAuth)
		}
		project.AgentAuth = req.AgentAuth
	}
	project.UpdatedAt = time.Now()

	err = s.repository.Update(project)
	if err != nil {
		return nil, err
	}

	return project, nil
}

// DeleteProject deletes a project
func (s *DefaultService) DeleteProject(id string) error {
	return s.repository.Delete(id)
}
