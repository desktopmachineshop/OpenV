package links

import (
	"encoding/json"
	"log"
	"reflect"
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
	ValidFrom  time.Time              `json:"valid_from"`
	ValidTo    *time.Time             `json:"valid_to"`
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
		ValidFrom:  now,
		ValidTo:    nil,
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
	GetLinksForArtifactVersion(artifactID string, version int) ([]*Link, error)
	UpdateLink(id string, req UpdateLinkRequest) (*Link, error)
	DeleteLink(id string) error
	GetAllLinks(projectID string) ([]*Link, error)
	SetArtifactService(artifactService interface{}) // Allows link service to trigger artifact versioning
}

// Repository defines persistence operations for links
type Repository interface {
	Save(link *Link) error
	FindByID(id string) (*Link, error)
	FindByFromID(fromID string) ([]*Link, error)
	FindByToID(toID string) ([]*Link, error)
	FindByFromIDForVersion(fromID string, version int) ([]*Link, error)
	FindByToIDForVersion(toID string, version int) ([]*Link, error)
	FindAll(projectID string) ([]*Link, error)
	Update(link *Link) error
	Delete(id string) error
	RecordLinkForArtifactVersion(linkID string, artifactID string, artifactVersion int) error
}

// DefaultService implements the Service interface
type DefaultService struct {
	repo              Repository
	artifactService   interface{} // Will be set to avoid circular imports
}

// NewDefaultService creates a new link service
func NewDefaultService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo}
}

// SetArtifactService sets the artifact service for triggering versioning
func (s *DefaultService) SetArtifactService(artifactService interface{}) {
	s.artifactService = artifactService
}

// CreateLink creates a new link and records version associations
func (s *DefaultService) CreateLink(link *Link) error {
	err := s.repo.Save(link)
	if err != nil {
		return err
	}

	// Record link-artifact associations for version tracking
	s.recordLinkForCurrentArtifactVersions(link)

	// Note: Automatic artifact versioning on link changes disabled to avoid reflection issues
	// Users can manually update artifacts to create new versions after link changes

	return nil
}

// recordLinkForCurrentArtifactVersions records the link for the current version of both artifacts
func (s *DefaultService) recordLinkForCurrentArtifactVersions(link *Link) {
	if s.artifactService == nil {
		return
	}

	// Get current versions of both artifacts
	getMethod := reflect.ValueOf(s.artifactService).MethodByName("GetArtifact")
	if !getMethod.IsValid() {
		return
	}

	// Record for FromID artifact's current version
	results := getMethod.Call([]reflect.Value{reflect.ValueOf(link.FromID)})
	if len(results) >= 2 && results[1].IsNil() {
		artifact := results[0].Interface()
		data, err := json.Marshal(artifact)
		if err == nil {
			var artifactData struct{ Version int }
			if json.Unmarshal(data, &artifactData) == nil {
				s.repo.RecordLinkForArtifactVersion(link.ID, link.FromID, artifactData.Version)
			}
		}
	}

	// Record for ToID artifact's current version
	results = getMethod.Call([]reflect.Value{reflect.ValueOf(link.ToID)})
	if len(results) >= 2 && results[1].IsNil() {
		artifact := results[0].Interface()
		data, err := json.Marshal(artifact)
		if err == nil {
			var artifactData struct{ Version int }
			if json.Unmarshal(data, &artifactData) == nil {
				s.repo.RecordLinkForArtifactVersion(link.ID, link.ToID, artifactData.Version)
			}
		}
	}
}

// triggerArtifactVersioning creates a new version of an artifact  
// when a link is created/updated/deleted
func (s *DefaultService) triggerArtifactVersioning(artifactID string) {
	if s.artifactService == nil {
		return
	}

	// Try to call UpdateArtifact with empty fields to preserve current state and bump version
	// Use reflection to call the method dynamically
	type ArtifactDTO struct {
		ID        string
		Version   int
		Type      string
		Title     string
		Body      string
		ParentID  *string
		SortOrder *int
		Attributes map[string]interface{}
		UpdatedAt time.Time
	}
	
	type UpdateRequest struct {
		Type       string
		Title      string
		Body       string
		ParentID   *string
		SortOrder  *int
		Attributes map[string]interface{}
	}

	// Get current artifact
	getMethod := reflect.ValueOf(s.artifactService).MethodByName("GetArtifact")
	if !getMethod.IsValid() {
		return
	}

	results := getMethod.Call([]reflect.Value{reflect.ValueOf(artifactID)})
	if len(results) < 2 || !results[1].IsNil() {
		return // Error getting artifact
	}

	artifact := results[0].Interface()
	if artifact == nil {
		return
	}

	// Marshal and unmarshal to extract fields
	data, err := json.Marshal(artifact)
	if err != nil {
		log.Printf("Error marshaling artifact: %v", err)
		return
	}

	var artifactData ArtifactDTO
	if err := json.Unmarshal(data, &artifactData); err != nil {
		log.Printf("Error unmarshaling artifact: %v", err)
		return
	}

	// Build update request with current state
	updateReq := UpdateRequest{
		Type:       artifactData.Type,
		Title:      artifactData.Title,
		Body:       artifactData.Body,
		ParentID:   artifactData.ParentID,
		SortOrder:  artifactData.SortOrder,
		Attributes: artifactData.Attributes,
	}

	// Call UpdateArtifact
	updateMethod := reflect.ValueOf(s.artifactService).MethodByName("UpdateArtifact")
	if updateMethod.IsValid() {
		updateMethod.Call([]reflect.Value{
			reflect.ValueOf(artifactID),
			reflect.ValueOf(updateReq),
		})
	}
}

// GetLink retrieves a link by ID
func (s *DefaultService) GetLink(id string) (*Link, error) {
	return s.repo.FindByID(id)
}

// GetLinksFrom retrieves all current links from an artifact
func (s *DefaultService) GetLinksFrom(artifactID string) ([]*Link, error) {
	return s.repo.FindByFromID(artifactID)
}

// GetLinksTo retrieves all current links to an artifact
func (s *DefaultService) GetLinksTo(artifactID string) ([]*Link, error) {
	return s.repo.FindByToID(artifactID)
}

// GetLinksForArtifactVersion retrieves links for a specific artifact version
func (s *DefaultService) GetLinksForArtifactVersion(artifactID string, version int) ([]*Link, error) {
	return s.repo.FindByFromIDForVersion(artifactID, version)
}

// UpdateLink updates a link and records version associations
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

	// Record link-artifact associations for version tracking
	s.recordLinkForCurrentArtifactVersions(link)

	// Note: Automatic artifact versioning disabled

	return link, nil
}

// DeleteLink deletes a link
func (s *DefaultService) DeleteLink(id string) error {
	err := s.repo.Delete(id)
	if err != nil {
		return err
	}

	// Note: Automatic artifact versioning disabled
	// Link is removed but artifacts are not versioned

	return nil
}

// GetAllLinks retrieves all links in a project
func (s *DefaultService) GetAllLinks(projectID string) ([]*Link, error) {
	return s.repo.FindAll(projectID)
}
