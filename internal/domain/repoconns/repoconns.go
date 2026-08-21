package repoconns

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// RepoConnection links a project to a git repository agents may work in.
type RepoConnection struct {
	ID                 string    `json:"id"`
	ProjectID          string    `json:"project_id"`
	Name               string    `json:"name"`
	RemoteURL          string    `json:"remote_url"`
	LocalPath          string    `json:"local_path"`
	DefaultBranch      string    `json:"default_branch"`
	CredentialStrategy string    `json:"credential_strategy"` // always "host": agents use the host's git credentials
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// CreateRequest is the payload for creating a repo connection.
type CreateRequest struct {
	ProjectID     string `json:"project_id"`
	Name          string `json:"name"`
	RemoteURL     string `json:"remote_url"`
	LocalPath     string `json:"local_path"`
	DefaultBranch string `json:"default_branch"`
}

// UpdateRequest carries the editable fields; nil fields are left unchanged.
type UpdateRequest struct {
	Name          *string `json:"name,omitempty"`
	RemoteURL     *string `json:"remote_url,omitempty"`
	LocalPath     *string `json:"local_path,omitempty"`
	DefaultBranch *string `json:"default_branch,omitempty"`
}

// Repository defines persistence operations for repo connections.
// FindByID returns (nil, nil) when no row exists.
type Repository interface {
	Save(c *RepoConnection) error
	Update(c *RepoConnection) error
	FindByID(id string) (*RepoConnection, error)
	ListByProject(projectID string) ([]*RepoConnection, error)
	Delete(id string) error
}

// Service defines repo connection domain logic.
type Service interface {
	Create(req CreateRequest) (*RepoConnection, error)
	Get(id string) (*RepoConnection, error)
	ListByProject(projectID string) ([]*RepoConnection, error)
	Update(id string, req UpdateRequest) (*RepoConnection, error)
	Delete(id string) error
}

// DefaultService implements the Service interface.
type DefaultService struct {
	repo Repository
}

// NewDefaultService creates a new repo connection service.
func NewDefaultService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo}
}

// Create validates and persists a new repo connection.
func (s *DefaultService) Create(req CreateRequest) (*RepoConnection, error) {
	if req.ProjectID == "" {
		return nil, errors.New("project_id is required")
	}
	if req.Name == "" {
		return nil, errors.New("name is required")
	}
	if req.RemoteURL == "" && req.LocalPath == "" {
		return nil, errors.New("at least one of remote_url or local_path is required")
	}

	branch := req.DefaultBranch
	if branch == "" {
		branch = "main"
	}

	now := time.Now()
	c := &RepoConnection{
		ID:                 uuid.New().String(),
		ProjectID:          req.ProjectID,
		Name:               req.Name,
		RemoteURL:          req.RemoteURL,
		LocalPath:          req.LocalPath,
		DefaultBranch:      branch,
		CredentialStrategy: "host",
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	if err := s.repo.Save(c); err != nil {
		return nil, err
	}
	return c, nil
}

// Get retrieves a repo connection by ID.
func (s *DefaultService) Get(id string) (*RepoConnection, error) {
	c, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errors.New("repo connection not found")
	}
	return c, nil
}

// ListByProject returns a project's repo connections.
func (s *DefaultService) ListByProject(projectID string) ([]*RepoConnection, error) {
	return s.repo.ListByProject(projectID)
}

// Update applies the non-nil fields of req.
func (s *DefaultService) Update(id string, req UpdateRequest) (*RepoConnection, error) {
	c, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errors.New("repo connection not found")
	}

	if req.Name != nil {
		if *req.Name == "" {
			return nil, errors.New("name is required")
		}
		c.Name = *req.Name
	}
	if req.RemoteURL != nil {
		c.RemoteURL = *req.RemoteURL
	}
	if req.LocalPath != nil {
		c.LocalPath = *req.LocalPath
	}
	if req.DefaultBranch != nil {
		c.DefaultBranch = *req.DefaultBranch
		if c.DefaultBranch == "" {
			c.DefaultBranch = "main"
		}
	}
	if c.RemoteURL == "" && c.LocalPath == "" {
		return nil, errors.New("at least one of remote_url or local_path is required")
	}
	c.CredentialStrategy = "host"

	c.UpdatedAt = time.Now()
	if err := s.repo.Update(c); err != nil {
		return nil, err
	}
	return c, nil
}

// Delete removes a repo connection.
func (s *DefaultService) Delete(id string) error {
	return s.repo.Delete(id)
}
