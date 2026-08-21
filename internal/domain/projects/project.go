package projects

import (
	"time"

	"github.com/google/uuid"
)

// Project represents a project in the system
type Project struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"org_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateProjectRequest represents the request to create a project
type CreateProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// UpdateProjectRequest represents the request to update a project
type UpdateProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// NewProject creates a new project
func NewProject(req CreateProjectRequest) *Project {
	return &Project{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

// Service defines the project service interface
type Service interface {
	CreateProject(project *Project) error
	GetProject(id string) (*Project, error)
	ListProjects() ([]*Project, error)
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

// UpdateProject updates a project
func (s *DefaultService) UpdateProject(id string, req UpdateProjectRequest) (*Project, error) {
	project, err := s.repository.GetByID(id)
	if err != nil {
		return nil, err
	}

	project.Name = req.Name
	project.Description = req.Description
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
