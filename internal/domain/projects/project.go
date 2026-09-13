package projects

import (
	"errors"
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
	ID          string `json:"id"`
	OrgID       string `json:"org_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	AgentAuth   string `json:"agent_auth"`
	// ParentProjectID names the project this one refines (REQ-144): a
	// subsystem or supplier project under the system it belongs to. Empty
	// for a top-level project. Requirements here may "refine" requirements
	// of the parent, and the parent's verification rolls those up.
	ParentProjectID string    `json:"parent_project_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
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
	// ParentProjectID is tri-state: absent (nil) leaves the parent alone,
	// "" detaches the project, an id attaches it under that project.
	ParentProjectID *string `json:"parent_project_id"`
}

// Errors a parent assignment can fail with; the API answers 400 for each.
var (
	ErrParentNotFound     = errors.New("parent project not found")
	ErrParentOtherOrg     = errors.New("a parent project must be in the same workspace")
	ErrParentIsSelf       = errors.New("a project cannot be its own parent")
	ErrParentIsDescendant = errors.New("a project cannot be placed under one of its own child projects")
)

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
	// ListChildren returns the projects whose parent is id, oldest first.
	ListChildren(id string) ([]*Project, error)
	// Ancestors returns the parent chain of id, nearest first. A cycle in
	// stored data ends the walk rather than hanging.
	Ancestors(id string) ([]*Project, error)
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
	if req.ParentProjectID != nil {
		if err := s.checkParent(project, *req.ParentProjectID); err != nil {
			return nil, err
		}
		project.ParentProjectID = *req.ParentProjectID
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

// checkParent refuses a parent that is missing, in another workspace, the
// project itself, or one of its descendants (which would close a loop).
func (s *DefaultService) checkParent(project *Project, parentID string) error {
	if parentID == "" {
		return nil
	}
	if parentID == project.ID {
		return ErrParentIsSelf
	}
	parent, err := s.repository.GetByID(parentID)
	if err != nil || parent == nil {
		return ErrParentNotFound
	}
	if parent.OrgID != project.OrgID {
		return ErrParentOtherOrg
	}
	chain, err := s.Ancestors(parentID)
	if err != nil {
		return err
	}
	for _, p := range chain {
		if p.ID == project.ID {
			return ErrParentIsDescendant
		}
	}
	return nil
}

// ListChildren implements Service.
func (s *DefaultService) ListChildren(id string) ([]*Project, error) {
	return s.repository.ListChildren(id)
}

// Ancestors implements Service.
func (s *DefaultService) Ancestors(id string) ([]*Project, error) {
	var chain []*Project
	seen := map[string]bool{id: true}
	cur, err := s.repository.GetByID(id)
	if err != nil {
		return nil, err
	}
	for cur.ParentProjectID != "" && !seen[cur.ParentProjectID] {
		seen[cur.ParentProjectID] = true
		parent, err := s.repository.GetByID(cur.ParentProjectID)
		if err != nil || parent == nil {
			break
		}
		chain = append(chain, parent)
		cur = parent
	}
	return chain, nil
}
