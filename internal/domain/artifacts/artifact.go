package artifacts

import (
	"errors"
	"log/slog"
	"strings"
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
	// Status is the review state (draft, in_review, approved, superseded);
	// see status.go. It is a real column; Attributes["status"] is only a
	// deprecated read-compat mirror kept in sync on every write.
	Status    string                 `json:"status"`
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

	// New artifacts start as drafts; a legacy Attributes["status"] value
	// (interview/guided/import paths still stamp one) seeds the column.
	status := StatusDraft
	if v, ok := attributes["status"].(string); ok {
		status = NormalizeStatus(v)
	}

	a := &Artifact{
		ID:         uuid.New().String(),
		ProjectID:  req.ProjectID,
		ParentID:   req.ParentID,
		Type:       req.Type,
		Title:      req.Title,
		Body:       req.Body,
		SortOrder:  order,
		Status:     status,
		Attributes: attributes,
		Version:    1,
		ValidFrom:  now,
		ValidTo:    nil,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	a.syncStatusAttribute()
	return a
}

// Service defines the artifact domain logic
type Service interface {
	CreateArtifact(artifact *Artifact) error
	GetArtifact(id string) (*Artifact, error)
	GetArtifactsByProject(projectID string) ([]*Artifact, error)
	UpdateArtifact(id string, req UpdateArtifactRequest) (*Artifact, error)
	// ChangeStatus applies one review-state transition (see status.go) and
	// persists it as a new temporal version. Returns ErrInvalidStatus for an
	// unknown target and ErrInvalidStatusTransition for a move the state
	// machine forbids.
	ChangeStatus(id string, status string) (*Artifact, error)
	DeleteArtifact(id string) error
	ListArtifacts(projectID string, artifactType string) ([]*Artifact, error)
	GetArtifactVersions(id string) ([]*Artifact, error)
	RestoreArtifactVersion(id string, version int) (*Artifact, error)
	// SearchArtifacts finds current artifacts whose title or body contains
	// query (case-insensitive) within the given projects, title matches first.
	SearchArtifacts(projectIDs []string, query string, limit int) ([]*SearchHit, error)
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
	// SearchInProjects performs the title/body substring search behind
	// Service.SearchArtifacts (SearchHit.ProjectName is left empty).
	SearchInProjects(projectIDs []string, query string, limit int) ([]*SearchHit, error)
}

// LinkSuspector flags and clears link suspicion for an artifact (issue
// #131). Implemented by links.DefaultService; declared here so the artifact
// service can drive suspicion from its update path without importing the
// links package.
type LinkSuspector interface {
	// MarkArtifactLinksSuspect flags every live link touching the artifact.
	MarkArtifactLinksSuspect(artifactID string) error
	// ClearArtifactLinksSuspicion clears the flag on those links.
	ClearArtifactLinksSuspicion(artifactID string) error
}

// DefaultService implements the Service interface
type DefaultService struct {
	repo Repository
	// linkSuspector, when set, is notified of content changes (mark) and
	// approvals (clear). Optional: nil disables suspicion tracking.
	linkSuspector LinkSuspector
}

// NewDefaultService creates a new artifact service
func NewDefaultService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo}
}

// SetLinkSuspector wires the link service in after construction (both
// services need each other; links already back-references artifacts).
func (s *DefaultService) SetLinkSuspector(ls LinkSuspector) {
	s.linkSuspector = ls
}

// markLinksSuspect flags the artifact's live links after a content change.
// Failure is logged, not returned: the artifact write already committed,
// and suspicion is advisory review metadata, not part of the write itself.
func (s *DefaultService) markLinksSuspect(artifactID string) {
	if s.linkSuspector == nil {
		return
	}
	if err := s.linkSuspector.MarkArtifactLinksSuspect(artifactID); err != nil {
		slog.Warn("artifacts: failed to mark links suspect", "artifact_id", artifactID, "error", err)
	}
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

// UpdateArtifact updates an artifact.
//
// Attributes contract (issue #125): a nil req.Attributes means "no change" —
// the current attributes carry over to the new version untouched. An
// explicit non-nil map (including an empty one) REPLACES the attributes
// wholesale. Callers that decode JSON get this for free: an omitted or null
// "attributes" field unmarshals to nil, {} to an empty map.
//
// Suspect links (issue #131): when the update changes the artifact's
// CONTENT — type, title, or body — every live link touching it is flagged
// suspect until confirmed or the artifact is approved again. Structural
// moves (parent/sort order) and attribute-only writes (e.g. the
// links_snapshot refresh in autoVersionLinkedArtifacts) do not change what
// the artifact says, so they leave link suspicion alone.
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

	contentChanged := artifact.Type != req.Type ||
		artifact.Title != req.Title ||
		artifact.Body != req.Body

	artifact.ParentID = req.ParentID
	artifact.Type = req.Type
	artifact.Title = req.Title
	artifact.Body = req.Body
	if req.Attributes != nil {
		artifact.Attributes = req.Attributes
	}

	// Status policy for content edits: the new version keeps the current
	// status, except that editing an APPROVED artifact demotes the new
	// version to draft — the approved snapshot survives untouched in the
	// temporal history (issue #127). The status column is authoritative;
	// any Attributes["status"] in the request is overwritten by the mirror,
	// so a plain update can never smuggle in an approval.
	artifact.Status = NormalizeStatus(artifact.Status)
	if artifact.Status == StatusApproved {
		artifact.Status = StatusDraft
	}
	artifact.syncStatusAttribute()

	if req.SortOrder != nil {
		artifact.SortOrder = *req.SortOrder
	} else if parentChanged {
		order, err := s.repo.NextSortOrder(artifact.ProjectID, artifact.ParentID)
		if err != nil {
			return nil, err
		}
		artifact.SortOrder = order
	}
	// Every update is a new temporal version: stamp ValidFrom = now so the
	// archived row's validity interval closes exactly where the new row's
	// opens (issue #161 — the repository archives the old row with
	// valid_to = this ValidFrom; carrying the stale ValidFrom forward gave
	// the archived row a zero-length interval and the new row a validity
	// window reaching back before it existed).
	now := time.Now()
	artifact.Version++
	artifact.ValidFrom = now
	artifact.UpdatedAt = now

	err = s.repo.Update(artifact)
	if err != nil {
		return nil, err
	}

	if contentChanged {
		s.markLinksSuspect(id)
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

// SearchArtifacts finds current artifacts matching query in the given projects.
func (s *DefaultService) SearchArtifacts(projectIDs []string, query string, limit int) ([]*SearchHit, error) {
	if len(projectIDs) == 0 || strings.TrimSpace(query) == "" {
		return []*SearchHit{}, nil
	}
	return s.repo.SearchInProjects(projectIDs, query, limit)
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

	// A restore is a content edit, so the same status policy as
	// UpdateArtifact applies: carry the CURRENT status forward (not the
	// restored snapshot's stale one), demoting approved to draft.
	restoredStatus := NormalizeStatus(current.Status)
	if restoredStatus == StatusApproved {
		restoredStatus = StatusDraft
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
		Status:     restoredStatus,
		Attributes: versionToRestore.Attributes,
		Version:    current.Version + 1,
		ValidFrom:  time.Now(),
		ValidTo:    nil,
		CreatedAt:  current.CreatedAt,
		UpdatedAt:  time.Now(),
	}
	restored.syncStatusAttribute()

	err = s.repo.Update(restored)
	if err != nil {
		return nil, err
	}

	// A restore is a content edit like any other: if it changed what the
	// artifact says, its links become suspect (issue #131).
	if current.Type != restored.Type || current.Title != restored.Title || current.Body != restored.Body {
		s.markLinksSuspect(id)
	}

	return restored, nil
}
