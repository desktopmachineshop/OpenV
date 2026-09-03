package attachments

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Figures.
//
// An image attached to an artifact is a numbered figure: REQ-17-FIG-1, built
// from the artifact's own stable reference and a per-artifact counter. The
// number is minted once and never reissued — deleting a figure does not free
// it — so a figure reference in a report, a review comment or a conversation
// months later still names the one image it named then. That is the contract
// artifact refs carry, and figures inherit it because they are cited the same
// way.
//
// The reference is also the stored filename: an image uploaded as
// "Screenshot 2026-09-03 at 14.22.31.png" is stored as "REQ-17-FIG-1.png", so
// what a reader downloads is named for what the document calls it. The name
// the uploader chose is kept alongside, because it is sometimes the only clue
// to what a drawing was.

// figureSuffix separates an artifact reference from its figure number.
const figureSuffix = "-FIG-"

var figureRefPattern = regexp.MustCompile(`^(.+)-FIG-(\d+)$`)

// FormatFigureRef builds a figure reference from an artifact's reference and a
// figure number.
func FormatFigureRef(artifactRef string, num int) string {
	return fmt.Sprintf("%s%s%d", artifactRef, figureSuffix, num)
}

// ParseFigureRef splits a figure reference into its artifact reference and
// number. ok is false for anything that is not one.
func ParseFigureRef(ref string) (artifactRef string, num int, ok bool) {
	m := figureRefPattern.FindStringSubmatch(ref)
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil || n < 1 {
		return "", 0, false
	}
	return m[1], n, true
}

// FigureFilename is the name a figure's file is stored and served under: the
// figure reference plus the uploaded file's extension.
//
// The extension is taken from the original name and bounded — it is
// attacker-supplied text that ends up in a filename — and an upload with no
// usable extension simply has none, rather than acquiring one it cannot
// support.
func FigureFilename(figureRef, originalFilename string) string {
	ext := strings.ToLower(path.Ext(originalFilename))
	safe := strings.Builder{}
	for _, r := range ext {
		if r == '.' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			safe.WriteRune(r)
		}
	}
	ext = safe.String()
	if len(ext) > 8 || ext == "." {
		ext = ""
	}
	return figureRef + ext
}

// Attachment is one figure on an artifact, holding its current version's file.
// Every version, including the first, is also recorded as a Version row.
type Attachment struct {
	ID         string `json:"id"`
	ArtifactID string `json:"artifact_id"`
	// Filename is the stored name, derived from FigureRef.
	Filename string `json:"filename"`
	// OriginalFilename is the name the uploader's file had.
	OriginalFilename string `json:"original_filename"`
	MimeType         string `json:"mime_type"`
	FilePath         string `json:"file_path"`
	FileSize         int    `json:"file_size"`
	// FigureRef is the citable reference ("REQ-17-FIG-1"); FigureNum is its
	// number within the artifact. Both are empty/zero only on rows whose
	// artifact has no reference to build on.
	FigureRef string `json:"figure_ref,omitempty"`
	FigureNum int    `json:"figure_num,omitempty"`
	// Version is the figure's current version, starting at 1.
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
}

// Version is one uploaded revision of a figure. The newest matches the
// attachment's own file fields; older ones stay on disk so a superseded
// drawing can still be retrieved.
type Version struct {
	ID               string    `json:"id"`
	AttachmentID     string    `json:"attachment_id"`
	Version          int       `json:"version"`
	Filename         string    `json:"filename"`
	OriginalFilename string    `json:"original_filename"`
	MimeType         string    `json:"mime_type"`
	FilePath         string    `json:"file_path"`
	FileSize         int       `json:"file_size"`
	CreatedBy        *string   `json:"created_by,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// CreateAttachmentRequest is the payload for creating an attachment
type CreateAttachmentRequest struct {
	ArtifactID       string
	Filename         string
	OriginalFilename string
	MimeType         string
	FilePath         string
	FileSize         int
}

// NewAttachment creates a new attachment at version 1. The figure reference is
// assigned by the repository, which owns the counter.
func NewAttachment(req CreateAttachmentRequest) *Attachment {
	return &Attachment{
		ID:               uuid.New().String(),
		ArtifactID:       req.ArtifactID,
		Filename:         req.Filename,
		OriginalFilename: req.OriginalFilename,
		MimeType:         req.MimeType,
		FilePath:         req.FilePath,
		FileSize:         req.FileSize,
		Version:          1,
		CreatedAt:        time.Now(),
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

	// SaveWithFigureRef stores a new attachment, minting its figure number
	// from the artifact's counter and recording it as version 1. artifactRef
	// is the artifact's stable reference; when it is empty the attachment is
	// stored without a figure reference rather than inventing one.
	SaveWithFigureRef(attachment *Attachment, artifactRef string) error
	// AddVersion replaces the figure's current file with a new version and
	// records it, returning the version number written.
	AddVersion(attachmentID string, v *Version) (int, error)
	// ListVersions returns a figure's versions, newest first.
	ListVersions(attachmentID string) ([]*Version, error)
	// FindVersion returns one version of a figure.
	FindVersion(attachmentID string, version int) (*Version, error)
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

	// CreateFigure stores a new figure on an artifact, allocating its number.
	CreateFigure(attachment *Attachment, artifactRef string) error
	// AddVersion supersedes a figure's file with a new version, returning the
	// version number written.
	AddVersion(attachmentID string, v *Version) (int, error)
	// GetVersions returns a figure's versions, newest first.
	GetVersions(attachmentID string) ([]*Version, error)
	// GetVersion returns one version of a figure.
	GetVersion(attachmentID string, version int) (*Version, error)
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

// CreateFigure stores a new figure and allocates its number.
func (s *DefaultService) CreateFigure(attachment *Attachment, artifactRef string) error {
	return s.repository.SaveWithFigureRef(attachment, artifactRef)
}

// AddVersion supersedes a figure's file with a new version.
func (s *DefaultService) AddVersion(attachmentID string, v *Version) (int, error) {
	return s.repository.AddVersion(attachmentID, v)
}

// GetVersions returns a figure's versions, newest first.
func (s *DefaultService) GetVersions(attachmentID string) ([]*Version, error) {
	return s.repository.ListVersions(attachmentID)
}

// GetVersion returns one version of a figure.
func (s *DefaultService) GetVersion(attachmentID string, version int) (*Version, error) {
	return s.repository.FindVersion(attachmentID, version)
}
