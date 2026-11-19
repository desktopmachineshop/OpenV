package projects

import (
	"context"
	"errors"
	"time"
)

// A project's cover photo: an image shown at the top of its Overview and on
// its card in All projects. The bytes are stored with the project and go
// when it does. The API checks the image (type, size, that its bytes are
// the type declared) before it reaches the service.

// MaxCoverBytes caps a cover photo. The app scales a large photo down before
// it uploads it, so this is room for a detailed image, not a camera original.
const MaxCoverBytes = 5 * 1024 * 1024

// ErrNoCover is the answer for a project that has no cover photo, and for an
// id no project has.
var ErrNoCover = errors.New("project has no cover photo")

// Cover is a project's cover photo.
type Cover struct {
	ProjectID string
	Mime      string
	Data      []byte
	UpdatedAt time.Time
}

// CoverRepository stores cover photos, one per project.
type CoverRepository interface {
	// GetCover answers the project's cover, ErrNoCover when it has none
	// (or no project has the id).
	GetCover(projectID string) (*Cover, error)
	// SetCover stores the project's cover, replacing any it had, and
	// answers when it was stored; ErrNotFound for an id no project has.
	SetCover(projectID, mime string, data []byte) (time.Time, error)
	// DeleteCover removes the project's cover; no cover is not an error.
	DeleteCover(projectID string) error
	// CoverStamps answers, for each of the projects that has a cover, when
	// it was stored. A project with no cover is absent from the map.
	CoverStamps(projectIDs []string) (map[string]time.Time, error)
}

// CoverService reads and changes cover photos and marks projects with them.
type CoverService interface {
	Get(projectID string) (*Cover, error)
	Set(projectID, mime string, data []byte) (time.Time, error)
	Delete(projectID string) error
	// Stamp sets CoverUpdatedAt on each project that has a cover.
	Stamp(list ...*Project) error
	// Generate makes a cover from prompt with the workspace's API key and
	// stores it, replacing any the project had; ErrCoverPrompt,
	// ErrNoCoverGenerator or ErrCoverProvider when nothing is stored.
	Generate(ctx context.Context, projectID, apiKey, prompt string) (time.Time, error)
	// Draw renders an SVG drawing (RenderCoverSVG) and stores the image as
	// the cover, replacing any the project had; ErrCoverSVG with the reason
	// when the drawing cannot be rendered, and nothing is stored.
	Draw(projectID, svg string) (time.Time, error)
}

// DefaultCoverService implements CoverService over a CoverRepository.
type DefaultCoverService struct {
	repo CoverRepository
	gen  ImageGenerator
}

// NewCoverService builds the cover service.
func NewCoverService(repo CoverRepository) *DefaultCoverService {
	return &DefaultCoverService{repo: repo}
}

// WithGenerator gives the service the generator Generate uses.
func (s *DefaultCoverService) WithGenerator(gen ImageGenerator) *DefaultCoverService {
	s.gen = gen
	return s
}

// Generate implements CoverService.
func (s *DefaultCoverService) Generate(ctx context.Context, projectID, apiKey, prompt string) (time.Time, error) {
	img, mime, err := generateCover(ctx, s.gen, apiKey, prompt)
	if err != nil {
		return time.Time{}, err
	}
	return s.repo.SetCover(projectID, mime, img)
}

// Draw implements CoverService.
func (s *DefaultCoverService) Draw(projectID, svg string) (time.Time, error) {
	img, mime, err := RenderCoverSVG(svg)
	if err != nil {
		return time.Time{}, err
	}
	return s.repo.SetCover(projectID, mime, img)
}

// Get implements CoverService.
func (s *DefaultCoverService) Get(projectID string) (*Cover, error) {
	return s.repo.GetCover(projectID)
}

// Set implements CoverService.
func (s *DefaultCoverService) Set(projectID, mime string, data []byte) (time.Time, error) {
	return s.repo.SetCover(projectID, mime, data)
}

// Delete implements CoverService.
func (s *DefaultCoverService) Delete(projectID string) error {
	return s.repo.DeleteCover(projectID)
}

// Stamp implements CoverService: one read for the whole list.
func (s *DefaultCoverService) Stamp(list ...*Project) error {
	ids := make([]string, 0, len(list))
	for _, p := range list {
		if p != nil {
			ids = append(ids, p.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	stamps, err := s.repo.CoverStamps(ids)
	if err != nil {
		return err
	}
	for _, p := range list {
		if p == nil {
			continue
		}
		if at, ok := stamps[p.ID]; ok {
			at := at.UTC()
			p.CoverUpdatedAt = &at
		} else {
			p.CoverUpdatedAt = nil
		}
	}
	return nil
}
