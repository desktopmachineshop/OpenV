package attachments

import (
	"time"

	"github.com/google/uuid"
)

// Attachment represents an image or file attachment to an artifact
type Attachment struct {
	ID        string    `json:"id"`
	ArtifactID string    `json:"artifact_id"`
	Filename  string    `json:"filename"`
	MimeType  string    `json:"mime_type"`
	FilePath  string    `json:"file_path"`
	FileSize  int       `json:"file_size"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateAttachmentRequest is the payload for creating an attachment
type CreateAttachmentRequest struct {
	ArtifactID string
	Filename   string
	MimeType   string
	FilePath   string
	FileSize   int
}

// NewAttachment creates a new attachment
func NewAttachment(req CreateAttachmentRequest) *Attachment {
	return &Attachment{
		ID:         uuid.New().String(),
		ArtifactID: req.ArtifactID,
		Filename:   req.Filename,
		MimeType:   req.MimeType,
		FilePath:   req.FilePath,
		FileSize:   req.FileSize,
		CreatedAt:  time.Now(),
	}
}

// Repository defines persistence operations for attachments
type Repository interface {
	Save(attachment *Attachment) error
	FindByID(id string) (*Attachment, error)
	FindByArtifactID(artifactID string) ([]*Attachment, error)
	// FindByArtifactIDs fetches the attachments for many artifacts in a single
	// query, grouped by artifact ID. Artifacts with no attachments are simply
	// absent from the map. It exists to kill the per-artifact N+1 in bulk
	// readers such as project export.
	FindByArtifactIDs(artifactIDs []string) (map[string][]*Attachment, error)
	Delete(id string) error
}

// Service defines the attachment domain logic
type Service interface {
	CreateAttachment(attachment *Attachment) error
	GetAttachment(id string) (*Attachment, error)
	GetAttachmentsByArtifact(artifactID string) ([]*Attachment, error)
	// GetAttachmentsByArtifacts returns the attachments for many artifacts in a
	// single query, grouped by artifact ID (see Repository.FindByArtifactIDs).
	GetAttachmentsByArtifacts(artifactIDs []string) (map[string][]*Attachment, error)
	DeleteAttachment(id string) error
}

// DefaultService implements the Service interface
type DefaultService struct {
	repository Repository
}

// NewDefaultService creates a new attachment service
func NewDefaultService(repository Repository) Service {
	return &DefaultService{repository: repository}
}

// CreateAttachment saves a new attachment
func (s *DefaultService) CreateAttachment(attachment *Attachment) error {
	return s.repository.Save(attachment)
}

// GetAttachment retrieves an attachment by ID
func (s *DefaultService) GetAttachment(id string) (*Attachment, error) {
	return s.repository.FindByID(id)
}

// GetAttachmentsByArtifact retrieves all attachments for an artifact
func (s *DefaultService) GetAttachmentsByArtifact(artifactID string) ([]*Attachment, error) {
	return s.repository.FindByArtifactID(artifactID)
}

// GetAttachmentsByArtifacts retrieves attachments for many artifacts in one
// query, grouped by artifact ID.
func (s *DefaultService) GetAttachmentsByArtifacts(artifactIDs []string) (map[string][]*Attachment, error) {
	return s.repository.FindByArtifactIDs(artifactIDs)
}

// DeleteAttachment deletes an attachment
func (s *DefaultService) DeleteAttachment(id string) error {
	return s.repository.Delete(id)
}
