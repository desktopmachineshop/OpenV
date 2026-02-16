package artifacts

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Error definitions
var (
	ErrNotFound = errors.New("artifact not found")
)

// Artifact represents a single requirement, test case, hazard, or other typed item
type Artifact struct {
	ID        string                 `json:"id"`
	ProjectID string                 `json:"project_id"`
	ParentID  *string                `json:"parent_id,omitempty"`
	Type      string                 `json:"type"` // requirement, test-case, hazard, design-item, heading, description, etc.
	Title     string                 `json:"title"`
	Body      string                 `json:"body"` // markdown or rich text
	SortOrder int                    `json:"sort_order"`
	Attributes map[string]interface{} `json:"attributes"`
	Version   int                    `json:"version"`
	ValidFrom time.Time              `json:"valid_from"`
	ValidTo   *time.Time             `json:"valid_to"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// SetLinksSnapshot stores links snapshot in attributes
func (a *Artifact) SetLinksSnapshot(links []interface{}) {
	if a.Attributes == nil {
		a.Attributes = make(map[string]interface{})
	}
	a.Attributes["links_snapshot"] = links
}

// GetLinksSnapshot retrieves links snapshot from attributes
func (a *Artifact) GetLinksSnapshot() []interface{} {
	if a.Attributes == nil {
		return []interface{}{}
	}
	if snapshot, ok := a.Attributes["links_snapshot"]; ok {
		if links, ok := snapshot.([]interface{}); ok {
			return links
		}
	}
	return []interface{}{}
}

// SetImagesSnapshot stores images snapshot in attributes
func (a *Artifact) SetImagesSnapshot(images []interface{}) {
	if a.Attributes == nil {
		a.Attributes = make(map[string]interface{})
	}
	a.Attributes["images_snapshot"] = images
}

// GetImagesSnapshot retrieves images snapshot from attributes
func (a *Artifact) GetImagesSnapshot() []interface{} {
	if a.Attributes == nil {
		return []interface{}{}
	}
	if snapshot, ok := a.Attributes["images_snapshot"]; ok {
		if images, ok := snapshot.([]interface{}); ok {
			return images
		}
	}
	return []interface{}{}
}

// CreateArtifactRequest is the payload for creating a new artifact
type CreateArtifactRequest struct {
	ProjectID  string                 `json:"project_id"`
	ParentID   *string                `json:"parent_id,omitempty"`
	Type       string                 `json:"type"`
	Title      string                 `json:"title"`
	Body       string                 `json:"body"`
	SortOrder  *int                   `json:"sort_order,omitempty"`
	Attributes map[string]interface{} `json:"attributes"`
}

// UpdateArtifactRequest is the payload for updating an artifact
type UpdateArtifactRequest struct {
	ParentID         *string                `json:"parent_id,omitempty"`
	Type             string                 `json:"type"`
	Title            string                 `json:"title"`
	Body             string                 `json:"body"`
	SortOrder        *int                   `json:"sort_order,omitempty"`
	Attributes       map[string]interface{} `json:"attributes"`
	LinksSnapshot    []interface{}          `json:"linksSnapshot,omitempty"`
	PendingLinkAdds  []interface{}          `json:"pendingLinkAdds,omitempty"`
	PendingLinkRemoves []string             `json:"pendingLinkRemoves,omitempty"`
}

// NewArtifact creates a new artifact with generated ID
func NewArtifact(req CreateArtifactRequest) *Artifact {
	now := time.Now()
	order := 0
	if req.SortOrder != nil {
		order = *req.SortOrder
	}
	
	// Ensure attributes is never nil
	attributes := req.Attributes
	if attributes == nil {
		attributes = make(map[string]interface{})
	}
	
	return &Artifact{
		ID:         uuid.New().String(),
		ProjectID:  req.ProjectID,
		ParentID:   req.ParentID,
		Type:       req.Type,
		Title:      req.Title,
		Body:       req.Body,
		SortOrder:  order,
		Attributes: attributes,
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
	GetArtifactVersions(id string) ([]*Artifact, error)
	RestoreArtifactVersion(id string, version int) (*Artifact, error)
}

// Repository defines persistence operations for artifacts
type Repository interface {
	Save(artifact *Artifact) error
	FindByID(id string) (*Artifact, error)
	FindByProjectID(projectID string) ([]*Artifact, error)
	FindByProjectAndType(projectID string, artifactType string) ([]*Artifact, error)
	Update(artifact *Artifact) error
	Delete(id string) error
	NextSortOrder(projectID string, parentID *string) (int, error)
	FindVersionsByID(id string) ([]*Artifact, error)
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
	if artifact.SortOrder == 0 {
		order, err := s.repo.NextSortOrder(artifact.ProjectID, artifact.ParentID)
		if err != nil {
			return err
		}
		artifact.SortOrder = order
	}
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

	parentChanged := false
	if (artifact.ParentID == nil) != (req.ParentID == nil) {
		parentChanged = true
	} else if artifact.ParentID != nil && req.ParentID != nil && *artifact.ParentID != *req.ParentID {
		parentChanged = true
	}

	artifact.ParentID = req.ParentID
	artifact.Type = req.Type
	artifact.Title = req.Title
	artifact.Body = req.Body
	artifact.Attributes = req.Attributes
	if req.SortOrder != nil {
		artifact.SortOrder = *req.SortOrder
	} else if parentChanged {
		order, err := s.repo.NextSortOrder(artifact.ProjectID, artifact.ParentID)
		if err != nil {
			return nil, err
		}
		artifact.SortOrder = order
	}
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

// GetArtifactVersions retrieves all versions of an artifact
func (s *DefaultService) GetArtifactVersions(id string) ([]*Artifact, error) {
	return s.repo.FindVersionsByID(id)
}

// RestoreArtifactVersion restores a previous version of an artifact
func (s *DefaultService) RestoreArtifactVersion(id string, version int) (*Artifact, error) {
	versions, err := s.repo.FindVersionsByID(id)
	if err != nil {
		return nil, err
	}

	// Find the specific version to restore
	var versionToRestore *Artifact
	for _, v := range versions {
		if v.Version == version {
			versionToRestore = v
			break
		}
	}

	if versionToRestore == nil {
		return nil, ErrNotFound
	}

	// Get current artifact to preserve some fields
	current, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}

	// Create a new version based on the old version
	restored := &Artifact{
		ID:         current.ID,
		ProjectID:  current.ProjectID,
		ParentID:   versionToRestore.ParentID,
		Type:       versionToRestore.Type,
		Title:      versionToRestore.Title,
		Body:       versionToRestore.Body,
		SortOrder:  current.SortOrder, // Keep current sort order
		Attributes: versionToRestore.Attributes,
		Version:    current.Version + 1,
		ValidFrom:  time.Now(),
		ValidTo:    nil,
		CreatedAt:  current.CreatedAt,
		UpdatedAt:  time.Now(),
	}

	err = s.repo.Update(restored)
	if err != nil {
		return nil, err
	}

	return restored, nil
}
