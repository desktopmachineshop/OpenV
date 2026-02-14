package artifacts

import (
	"time"

	"github.com/google/uuid"
)

// Artifact represents a single requirement, test case, hazard, or other typed item
type Artifact struct {
	ID        string                 `json:"id"`
	ProjectID string                 `json:"project_id"`
	Type      string                 `json:"type"` // requirement, test-case, hazard, design-item, etc.
	Title     string                 `json:"title"`
	Body      string                 `json:"body"` // markdown or rich text
	Attributes map[string]interface{} `json:"attributes"`
	Version   int                    `json:"version"`
	ValidFrom time.Time              `json:"valid_from"`
	ValidTo   *time.Time             `json:"valid_to"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// CreateArtifactRequest is the payload for creating a new artifact
type CreateArtifactRequest struct {
	ProjectID  string                 `json:"project_id"`
	Type       string                 `json:"type"`
	Title      string                 `json:"title"`
	Body       string                 `json:"body"`
	Attributes map[string]interface{} `json:"attributes"`
}

// UpdateArtifactRequest is the payload for updating an artifact
type UpdateArtifactRequest struct {
	Title      string                 `json:"title"`
	Body       string                 `json:"body"`
	Attributes map[string]interface{} `json:"attributes"`
}

// NewArtifact creates a new artifact with generated ID
func NewArtifact(req CreateArtifactRequest) *Artifact {
	now := time.Now()
	return &Artifact{
		ID:         uuid.New().String(),
		ProjectID:  req.ProjectID,
		Type:       req.Type,
		Title:      req.Title,
		Body:       req.Body,
		Attributes: req.Attributes,
		Version:    1,
		ValidFrom:  now,
		ValidTo:    nil,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// Service defines the artifact domain logic
type Service interface {
	CreateArtifact(artifact *Artifact) error
	GetArtifact(id string) (*Artifact, error)
	GetArtifactsByProject(projectID string) ([]*Artifact, error)
	UpdateArtifact(id string, req UpdateArtifactRequest) (*Artifact, error)
	DeleteArtifact(id string) error
	ListArtifacts(projectID string, artifactType string) ([]*Artifact, error)
}

// Repository defines persistence operations for artifacts
type Repository interface {
	Save(artifact *Artifact) error
	FindByID(id string) (*Artifact, error)
	FindByProjectID(projectID string) ([]*Artifact, error)
	FindByProjectAndType(projectID string, artifactType string) ([]*Artifact, error)
	Update(artifact *Artifact) error
	Delete(id string) error
}

// DefaultService implements the Service interface
type DefaultService struct {
	repo Repository
}

// NewDefaultService creates a new artifact service
func NewDefaultService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo}
}

// CreateArtifact creates a new artifact
func (s *DefaultService) CreateArtifact(artifact *Artifact) error {
	return s.repo.Save(artifact)
}

// GetArtifact retrieves an artifact by ID
func (s *DefaultService) GetArtifact(id string) (*Artifact, error) {
	return s.repo.FindByID(id)
}

// GetArtifactsByProject retrieves all artifacts for a project
func (s *DefaultService) GetArtifactsByProject(projectID string) ([]*Artifact, error) {
	return s.repo.FindByProjectID(projectID)
}

// UpdateArtifact updates an artifact
func (s *DefaultService) UpdateArtifact(id string, req UpdateArtifactRequest) (*Artifact, error) {
	artifact, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}

	artifact.Title = req.Title
	artifact.Body = req.Body
	artifact.Attributes = req.Attributes
	artifact.Version++
	artifact.UpdatedAt = time.Now()

	err = s.repo.Update(artifact)
	if err != nil {
		return nil, err
	}

	return artifact, nil
}

// DeleteArtifact deletes an artifact
func (s *DefaultService) DeleteArtifact(id string) error {
	return s.repo.Delete(id)
}

// ListArtifacts lists artifacts by project and optional type filter
func (s *DefaultService) ListArtifacts(projectID string, artifactType string) ([]*Artifact, error) {
	if artifactType == "" {
		return s.repo.FindByProjectID(projectID)
	}
	return s.repo.FindByProjectAndType(projectID, artifactType)
}
