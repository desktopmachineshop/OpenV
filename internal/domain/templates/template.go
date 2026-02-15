package templates

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/exports"
)

// Template represents a reusable project snapshot.
type Template struct {
	ID          string          `json:"id"`
	Key         string          `json:"key,omitempty"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Snapshot    json.RawMessage `json:"snapshot"`
	IsDefault   bool            `json:"is_default"`
	CreatedAt   time.Time       `json:"created_at"`
}

// Repository defines template persistence operations.
type Repository interface {
	Create(template *Template) error
	List() ([]*Template, error)
	GetByID(id string) (*Template, error)
	GetByKey(key string) (*Template, error)
}

// Service defines template behavior.
type Service interface {
	SeedDefaults() error
	CreateTemplateFromProject(projectID string, name string, description string) (*Template, error)
	ListTemplates() ([]*Template, error)
	GetTemplate(id string) (*Template, error)
	CreateProjectFromTemplate(templateID string, name string, description string) (string, error)
}

// DefaultService implements template logic.
type DefaultService struct {
	repo          Repository
	exportService exports.Service
}

// NewService creates a new template service.
func NewService(repo Repository, exportService exports.Service) *DefaultService {
	return &DefaultService{repo: repo, exportService: exportService}
}

// SeedDefaults ensures default templates exist.
func (s *DefaultService) SeedDefaults() error {
	defaults, err := DefaultTemplates()
	if err != nil {
		return err
	}

	for _, tpl := range defaults {
		if tpl.Key == "" {
			continue
		}
		existing, err := s.repo.GetByKey(tpl.Key)
		if err == nil && existing != nil {
			continue
		}
		if err := s.repo.Create(tpl); err != nil {
			return err
		}
	}

	return nil
}

// CreateTemplateFromProject captures a project snapshot as a template.
func (s *DefaultService) CreateTemplateFromProject(projectID string, name string, description string) (*Template, error) {
	if projectID == "" {
		return nil, errors.New("project_id is required")
	}
	if name == "" {
		return nil, errors.New("template name is required")
	}

	snapshot, _, err := s.exportService.ExportProject(projectID, exports.FormatJSON)
	if err != nil {
		return nil, err
	}

	template := &Template{
		ID:          uuid.New().String(),
		Name:        name,
		Description: description,
		Snapshot:    json.RawMessage(snapshot),
		IsDefault:   false,
		CreatedAt:   time.Now(),
	}

	if err := s.repo.Create(template); err != nil {
		return nil, err
	}

	return template, nil
}

// ListTemplates returns all templates.
func (s *DefaultService) ListTemplates() ([]*Template, error) {
	return s.repo.List()
}

// GetTemplate returns a template by ID.
func (s *DefaultService) GetTemplate(id string) (*Template, error) {
	return s.repo.GetByID(id)
}

// CreateProjectFromTemplate creates a new project from a template.
func (s *DefaultService) CreateProjectFromTemplate(templateID string, name string, description string) (string, error) {
	if templateID == "" {
		return "", errors.New("template_id is required")
	}

	tpl, err := s.repo.GetByID(templateID)
	if err != nil {
		return "", err
	}

	return s.exportService.ImportProjectWithOverrides([]byte(tpl.Snapshot), name, description)
}
