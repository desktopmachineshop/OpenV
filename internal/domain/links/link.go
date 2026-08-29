package links

import (
	"encoding/json"
	"reflect"
	"time"

	"github.com/google/uuid"
)

// Link represents a traceability link between two artifacts
type Link struct {
	ID     string `json:"id"`
	FromID string `json:"from_id"`
	ToID   string `json:"to_id"`
	Type   string `json:"type"` // verifies, satisfies, mitigates, implements, etc.
	// Suspect marks a link whose meaning may no longer hold because the
	// content of an artifact it touches changed after the link was made
	// (issue #131). It is cleared by an explicit per-link confirmation
	// (PUT /api/v1/links/{id}/confirm) or automatically when the artifact
	// is approved again — review implies reconfirming its traceability.
	Suspect    bool                   `json:"suspect"`
	Attributes map[string]interface{} `json:"attributes"`
	Version    int                    `json:"version"`
	ValidFrom  time.Time              `json:"valid_from"`
	ValidTo    *time.Time             `json:"valid_to"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
}

// SuspectLink is a suspect link enriched with the titles and types of the
// two artifacts it connects, for the review queue (issue #183). It is a
// read-only projection assembled by a join in the repository — the plain
// Link carries only endpoint ids, and the queue needs human-readable labels
// so a reviewer can judge a link without opening both artifacts.
type SuspectLink struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	FromID    string    `json:"from_id"`
	FromTitle string    `json:"from_title"`
	FromType  string    `json:"from_type"`
	ToID      string    `json:"to_id"`
	ToTitle   string    `json:"to_title"`
	ToType    string    `json:"to_type"`
	UpdatedAt time.Time `json:"updated_at"`
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
	// ListSuspectByProject returns the live suspect links touching a project
	// (either endpoint), enriched with artifact titles/types, for the review
	// queue (issue #183).
	ListSuspectByProject(projectID string) ([]*SuspectLink, error)
	SetArtifactService(artifactService interface{}) // Allows link service to trigger artifact versioning
	// ConfirmLink clears the suspect flag on one link: a human has re-read
	// the changed artifact and vouches that the link still holds.
	ConfirmLink(id string) (*Link, error)
	// MarkArtifactLinksSuspect flags every live link touching the artifact
	// as suspect (called when the artifact's content changes).
	MarkArtifactLinksSuspect(artifactID string) error
	// ClearArtifactLinksSuspicion clears the suspect flag on every live
	// link touching the artifact (called when the artifact is approved).
	ClearArtifactLinksSuspicion(artifactID string) error
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
	// FindSuspectByProject returns the live suspect links whose from or to
	// artifact belongs to the project, joined with those artifacts' titles
	// and types (issue #183).
	FindSuspectByProject(projectID string) ([]*SuspectLink, error)
	Update(link *Link) error
	Delete(id string) error
	RecordLinkForArtifactVersion(linkID string, artifactID string, artifactVersion int) error
	// SetSuspect sets the suspect flag on one live link.
	SetSuspect(id string, suspect bool) error
	// SetSuspectByArtifact sets the suspect flag on every live link whose
	// from_id or to_id is the given artifact.
	SetSuspectByArtifact(artifactID string, suspect bool) error
}

// DefaultService implements the Service interface
type DefaultService struct {
	repo            Repository
	artifactService interface{} // Will be set to avoid circular imports
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

// ListSuspectByProject returns the enriched suspect links touching a project.
func (s *DefaultService) ListSuspectByProject(projectID string) ([]*SuspectLink, error) {
	return s.repo.FindSuspectByProject(projectID)
}

// ConfirmLink clears the suspect flag on one link and returns it.
func (s *DefaultService) ConfirmLink(id string) (*Link, error) {
	link, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if link.Suspect {
		if err := s.repo.SetSuspect(id, false); err != nil {
			return nil, err
		}
		link.Suspect = false
	}
	return link, nil
}

// MarkArtifactLinksSuspect flags every live link touching the artifact.
// Also satisfies artifacts.LinkSuspector so the artifact service can mark
// links suspect on content changes without an import cycle.
func (s *DefaultService) MarkArtifactLinksSuspect(artifactID string) error {
	return s.repo.SetSuspectByArtifact(artifactID, true)
}

// ClearArtifactLinksSuspicion clears the suspect flag on every live link
// touching the artifact (approval implies reconfirmation, issue #131).
func (s *DefaultService) ClearArtifactLinksSuspicion(artifactID string) error {
	return s.repo.SetSuspectByArtifact(artifactID, false)
}
