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

	// MyLocalPath is the calling user's per-user override of LocalPath —
	// runs on each member's own machine use their own checkout location.
	// Populated on user-scoped reads only; never stored on this row.
	MyLocalPath string `json:"my_local_path,omitempty"`
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
	// SetUserPath stores a user's local-path override for a connection
	// (empty path removes the override).
	SetUserPath(userID, connID, path string) error
	// UserPaths returns a user's overrides for a project's connections,
	// keyed by connection id.
	UserPaths(userID, projectID string) (map[string]string, error)
}

// Service defines repo connection domain logic.
type Service interface {
	Create(req CreateRequest) (*RepoConnection, error)
	Get(id string) (*RepoConnection, error)
	ListByProject(projectID string) ([]*RepoConnection, error)
	// ListByProjectForUser returns a project's connections with the user's
	// per-user local paths attached (MyLocalPath).
	ListByProjectForUser(projectID, userID string) ([]*RepoConnection, error)
	Update(id string, req UpdateRequest) (*RepoConnection, error)
	// SetMyPath stores (or clears, with an empty path) a user's local-path
	// override for a connection.
	SetMyPath(userID, connID, path string) error
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

// ListByProjectForUser returns a project's connections with the user's
// per-user local paths attached.
func (s *DefaultService) ListByProjectForUser(projectID, userID string) ([]*RepoConnection, error) {
	conns, err := s.repo.ListByProject(projectID)
	if err != nil {
		return nil, err
	}
	if userID == "" || len(conns) == 0 {
		return conns, nil
	}
	paths, err := s.repo.UserPaths(userID, projectID)
	if err != nil {
		return nil, err
	}
	for _, c := range conns {
		c.MyLocalPath = paths[c.ID]
	}
	return conns, nil
}

// SetMyPath stores (or clears) a user's local-path override for a connection.
func (s *DefaultService) SetMyPath(userID, connID, path string) error {
	if userID == "" {
		return errors.New("user id is required")
	}
	if c, err := s.repo.FindByID(connID); err != nil {
		return err
	} else if c == nil {
		return errors.New("repo connection not found")
	}
	return s.repo.SetUserPath(userID, connID, path)
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
