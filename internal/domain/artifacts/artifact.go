package artifacts

import (
	"encoding/json"
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
	ID        string  `json:"id"`
	ProjectID string  `json:"project_id"`
	ParentID  *string `json:"parent_id,omitempty"`
	Type      string  `json:"type"` // requirement, test-case, hazard, design-item, heading, description, etc.
	// Ref is the stable short address ("REQ-12"), unique among a project's
	// current artifacts and constant across versions. Assigned server-side
	// on create (see ref.go); distinct from CreateArtifactRequest.Ref, the
	// proposal-mode temporary token.
	Ref string `json:"ref,omitempty"`
	// DocNumber is the derived document number ("1.2") for a heading, filled
	// in by the API when it serves a whole project and left empty otherwise.
	// It is never stored: it describes where the artifact currently sits, so
	// reordering the document changes it while Ref stays put (numbering.go).
	DocNumber string `json:"doc_number,omitempty"`
	Title     string `json:"title"`
	Body      string `json:"body"` // markdown or rich text
	SortOrder int    `json:"sort_order"`
	// Status is the review state (draft, in_review, approved, superseded);
	// see status.go. It is a real column; Attributes["status"] is only a
	// deprecated read-compat mirror kept in sync on every write.
	Status     string                 `json:"status"`
	Attributes map[string]interface{} `json:"attributes"`
	Version    int                    `json:"version"`
	ValidFrom  time.Time              `json:"valid_from"`
	ValidTo    *time.Time             `json:"valid_to"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
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
	// Ref is a proposal-only field: a caller-chosen temporary token that lets a
	// proposal-mode agent reference this not-yet-created artifact from a sibling
	// create_link proposal in the same run (issue #235). It is ignored on the
	// direct (non-proposal) write path — NewArtifact never reads it — and is
	// lifted out of the payload into its own column when the write is diverted
	// to the proposal queue (see proposals.DefaultService.Propose).
	Ref string `json:"ref,omitempty"`
}

// OptionalString is a JSON field that distinguishes all three payload
// states: OMITTED (Present false — the key never appeared), explicit NULL
// (Present true, Value nil), and a SET string (Present true, Value non-nil).
// A plain *string cannot tell the first two apart, which is exactly the
// issue-#172 hazard: an update that omits parent_id must not be read as
// "move to root".
//
// encoding/json only calls UnmarshalJSON for keys present in the payload,
// so decoding stamps Present for free. Tag the field `omitzero` (Go 1.24+)
// so a non-present value stays omitted on re-marshal — the proposal queue
// round-trips requests through JSON before applying them, and null/omitted
// must survive that round trip unchanged.
type OptionalString struct {
	Present bool
	Value   *string
}

// PresentString returns an OptionalString that explicitly carries v
// (v == nil is an explicit null). For internal callers building requests
// in Go; the zero value OptionalString{} means "field omitted".
func PresentString(v *string) OptionalString {
	return OptionalString{Present: true, Value: v}
}

// UnmarshalJSON records that the field appeared, then decodes null/string
// into Value.
func (o *OptionalString) UnmarshalJSON(data []byte) error {
	o.Present = true
	return json.Unmarshal(data, &o.Value)
}

// MarshalJSON emits the carried value (null when Value is nil). Pair with
// the `omitzero` tag so omitted fields are not serialized as null.
func (o OptionalString) MarshalJSON() ([]byte, error) {
	return json.Marshal(o.Value)
}

// IsZero reports whether the field was omitted; it drives `omitzero`.
func (o OptionalString) IsZero() bool { return !o.Present }

// UpdateArtifactRequest is the payload for updating an artifact
// UpdateArtifactRequest's content fields (Type, Title, Body) are pointers so
// callers can distinguish "omitted" from "explicitly empty": a nil pointer
// means "no change" (the current value carries forward to the new version),
// while a non-nil pointer — even to "" — replaces the value. This mirrors the
// Attributes contract from issue #125 and closes issue #170, where the MCP
// update_artifact tool wiped bodies by sending "" for an omitted argument.
// JSON decoding gives callers this for free: omitted/null fields unmarshal
// to nil.
//
// ParentID needs a third state — "move to root" is a legitimate explicit
// value (JSON null) distinct from "omitted" — so it is an OptionalString
// (issue #172): omitted = keep the current parent, null = move to root,
// a string = reparent under that artifact.
//
// SortOrder deliberately stays a plain *int: nil (omitted or null) means
// "keep the current order" (or auto-assign when the parent changed), and
// there is no meaningful explicit-null semantic for it.
type UpdateArtifactRequest struct {
	ParentID           OptionalString         `json:"parent_id,omitzero"`
	Type               *string                `json:"type,omitempty"`
	Title              *string                `json:"title,omitempty"`
	Body               *string                `json:"body,omitempty"`
	SortOrder          *int                   `json:"sort_order,omitempty"`
	Attributes         map[string]interface{} `json:"attributes"`
	LinksSnapshot      []interface{}          `json:"linksSnapshot,omitempty"`
	PendingLinkAdds    []interface{}          `json:"pendingLinkAdds,omitempty"`
	PendingLinkRemoves []string               `json:"pendingLinkRemoves,omitempty"`
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
	// ListByStatus returns a project's current artifacts in the given review
	// status (e.g. in_review for the review queue, issue #183), newest change
	// first.
	ListByStatus(projectID string, status string) ([]*Artifact, error)
	// ListArtifactsPage returns one page of a project's current artifacts in
	// stable tree order plus the total count of matching artifacts. Artifacts
	// form a parent_id tree that the UI reassembles client-side, so
	// exhaustive callers (export, V&V, the module tree) should keep paging
	// until they have `total` rows; ListArtifacts remains the
	// fetch-everything path for in-process callers.
	ListArtifactsPage(projectID string, artifactType string, limit, offset int) ([]*Artifact, int, error)
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
	// FindByProjectAndStatus returns a project's current artifacts in the
	// given review status (issue #183).
	FindByProjectAndStatus(projectID string, status string) ([]*Artifact, error)
	// FindPageByProject returns one page of a project's current artifacts in
	// stable tree order (parent_id NULLS FIRST, sort_order, created_at, id);
	// artifactType "" means all types.
	FindPageByProject(projectID string, artifactType string, limit, offset int) ([]*Artifact, error)
	// CountByProject counts a project's current artifacts (type "" = all).
	CountByProject(projectID string, artifactType string) (int, error)
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

// EmbeddingIndexer receives an artifact's current content after a create or
// update so a semantic-search embedding can be (re)computed for it (issue
// #220). Implemented by embeddings.Service; declared here with primitive
// arguments so the artifact service can drive indexing without importing the
// embeddings package (which itself reads artifacts). Implementations MUST be
// best-effort and non-blocking — the artifact write has already committed, and
// embedding is advisory search infrastructure, never part of the write.
type EmbeddingIndexer interface {
	IndexArtifact(id string, version int, title, body string)
}

// DefaultService implements the Service interface
type DefaultService struct {
	repo Repository
	// linkSuspector, when set, is notified of content changes (mark) and
	// approvals (clear). Optional: nil disables suspicion tracking.
	linkSuspector LinkSuspector
	// embeddingIndexer, when set, is handed the artifact's content after a
	// create/update to (re)compute its search embedding. Optional: nil
	// disables embedding entirely (the default deployment).
	embeddingIndexer EmbeddingIndexer
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

// SetEmbeddingIndexer wires the semantic-search indexer in after construction
// (the indexer reads artifacts, so it is built after this service). nil leaves
// embedding disabled.
func (s *DefaultService) SetEmbeddingIndexer(ix EmbeddingIndexer) {
	s.embeddingIndexer = ix
}

// indexEmbedding hands the artifact's current content to the embedding indexer
// (best-effort, nil-safe). The indexer is responsible for doing the work
// off the request path and swallowing its own errors.
func (s *DefaultService) indexEmbedding(a *Artifact) {
	if s.embeddingIndexer == nil || a == nil {
		return
	}
	s.embeddingIndexer.IndexArtifact(a.ID, a.Version, a.Title, a.Body)
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
	if err := s.repo.Save(artifact); err != nil {
		return err
	}
	s.indexEmbedding(artifact)
	return nil
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
// Content fields contract (issue #170): Type, Title, and Body follow the
// same rule — nil means "no change", a non-nil pointer (even to "")
// replaces. This keeps clients that update a single field (e.g. the MCP
// update_artifact tool) from silently wiping the others.
//
// Parent contract (issue #172): ParentID is presence-aware. An omitted
// parent_id keeps the current parent; an explicit JSON null moves the
// artifact to the root; a set ID reparents it (auto-assigning sort order
// when SortOrder is not also given).
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

	// Parent contract (issue #172): only a PRESENT parent_id touches the
	// tree. Omitted keeps the current parent; explicit null moves to root;
	// a set ID reparents. An omitted field previously decoded to the same
	// nil as explicit null, so every parent-less update (e.g. the MCP
	// update_artifact tool) silently reparented the artifact to root.
	parentChanged := false
	if req.ParentID.Present {
		if (artifact.ParentID == nil) != (req.ParentID.Value == nil) {
			parentChanged = true
		} else if artifact.ParentID != nil && req.ParentID.Value != nil && *artifact.ParentID != *req.ParentID.Value {
			parentChanged = true
		}
	}

	contentChanged := (req.Type != nil && artifact.Type != *req.Type) ||
		(req.Title != nil && artifact.Title != *req.Title) ||
		(req.Body != nil && artifact.Body != *req.Body)

	if req.ParentID.Present {
		artifact.ParentID = req.ParentID.Value
	}
	if req.Type != nil {
		artifact.Type = *req.Type
	}
	if req.Title != nil {
		artifact.Title = *req.Title
	}
	if req.Body != nil {
		artifact.Body = *req.Body
	}
	if req.Attributes != nil {
		artifact.Attributes = req.Attributes
	}

	// Status policy for content edits: the new version keeps the current
	// status, except that editing an APPROVED artifact's CONTENT demotes the
	// new version to draft — the approved snapshot survives untouched in the
	// temporal history (issue #127). Only genuine content edits demote, using
	// the SAME contentChanged signal that flags links suspect (issue #174):
	// attribute-only writes (e.g. the links_snapshot refresh in
	// autoVersionLinkedArtifacts) and structural moves (parent/sort order) do
	// not change what the artifact says, so they must NOT demote an approved
	// requirement. The status column is authoritative; any Attributes["status"]
	// in the request is overwritten by the mirror, so a plain update can never
	// smuggle in an approval.
	artifact.Status = NormalizeStatus(artifact.Status)
	if contentChanged && artifact.Status == StatusApproved {
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
	s.indexEmbedding(artifact)

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

// ListByStatus lists a project's current artifacts in the given review status.
func (s *DefaultService) ListByStatus(projectID string, status string) ([]*Artifact, error) {
	return s.repo.FindByProjectAndStatus(projectID, status)
}

// ListArtifactsPage returns one page of a project's artifacts plus the total
// count of artifacts matching the filter.
func (s *DefaultService) ListArtifactsPage(projectID string, artifactType string, limit, offset int) ([]*Artifact, int, error) {
	total, err := s.repo.CountByProject(projectID, artifactType)
	if err != nil {
		return nil, 0, err
	}
	page, err := s.repo.FindPageByProject(projectID, artifactType, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	return page, total, nil
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
	s.indexEmbedding(restored)

	return restored, nil
}
