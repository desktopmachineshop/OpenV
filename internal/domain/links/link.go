package links

import (
	"time"

	"github.com/google/uuid"
)

// Link represents a traceability link between two artifacts
type Link struct {
	ID         string                 `json:"id"`
	FromID     string                 `json:"from_id"`
	ToID       string                 `json:"to_id"`
	Type       string                 `json:"type"` // verifies, satisfies, mitigates, implements, etc.
	Attributes map[string]interface{} `json:"attributes"`
	Version    int                    `json:"version"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
}

// CreateLinkRequest is the payload for creating a new link
type CreateLinkRequest struct {
	FromID     string                 `json:"from_id"`
	ToID       string                 `json:"to_id"`
	Type       string                 `json:"type"`
	Attributes map[string]interface{} `json:"attributes"`
}

// UpdateLinkRequest is the payload for updating a link
type UpdateLinkRequest struct {
	Type       string                 `json:"type"`
	Attributes map[string]interface{} `json:"attributes"`
}

// NewLink creates a new link with generated ID
func NewLink(req CreateLinkRequest) *Link {
	now := time.Now()
	return &Link{
		ID:         uuid.New().String(),
		FromID:     req.FromID,
		ToID:       req.ToID,
		Type:       req.Type,
		Attributes: req.Attributes,
		Version:    1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// Service defines the link domain logic
type Service interface {
	CreateLink(link *Link) error
	GetLink(id string) (*Link, error)
	GetLinksFrom(artifactID string) ([]*Link, error)
	GetLinksTo(artifactID string) ([]*Link, error)
	UpdateLink(id string, req UpdateLinkRequest) (*Link, error)
	DeleteLink(id string) error
	GetAllLinks(projectID string) ([]*Link, error)
}

// Repository defines persistence operations for links
type Repository interface {
	Save(link *Link) error
	FindByID(id string) (*Link, error)
	FindByFromID(fromID string) ([]*Link, error)
	FindByToID(toID string) ([]*Link, error)
	FindAll(projectID string) ([]*Link, error)
	Update(link *Link) error
	Delete(id string) error
}

// DefaultService implements the Service interface
type DefaultService struct {
	repo Repository
}

// NewDefaultService creates a new link service
func NewDefaultService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo}
}

// CreateLink creates a new link
func (s *DefaultService) CreateLink(link *Link) error {
	return s.repo.Save(link)
}

// GetLink retrieves a link by ID
func (s *DefaultService) GetLink(id string) (*Link, error) {
	return s.repo.FindByID(id)
}

// GetLinksFrom retrieves all links from an artifact
func (s *DefaultService) GetLinksFrom(artifactID string) ([]*Link, error) {
	return s.repo.FindByFromID(artifactID)
}

// GetLinksTo retrieves all links to an artifact
func (s *DefaultService) GetLinksTo(artifactID string) ([]*Link, error) {
	return s.repo.FindByToID(artifactID)
}

// UpdateLink updates a link
func (s *DefaultService) UpdateLink(id string, req UpdateLinkRequest) (*Link, error) {
	link, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}

	link.Type = req.Type
	link.Attributes = req.Attributes
	link.Version++
	link.UpdatedAt = time.Now()

	err = s.repo.Update(link)
	if err != nil {
		return nil, err
	}

	return link, nil
}

// DeleteLink deletes a link
func (s *DefaultService) DeleteLink(id string) error {
	return s.repo.Delete(id)
}

// GetAllLinks retrieves all links in a project
func (s *DefaultService) GetAllLinks(projectID string) ([]*Link, error) {
	return s.repo.FindAll(projectID)
}
