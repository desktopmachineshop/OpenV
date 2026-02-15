package baselines

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Baseline represents a snapshot of a project at a point in time.
type Baseline struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"project_id"`
	Name      string          `json:"name"`
	Snapshot  json.RawMessage `json:"snapshot"`
	CreatedAt time.Time       `json:"created_at"`
}

// Repository defines baseline persistence operations.
type Repository interface {
	Create(baseline *Baseline) error
	ListByProjectID(projectID string) ([]*Baseline, error)
	GetByID(id string) (*Baseline, error)
	Delete(id string) error
}

// Service defines baseline business logic.
type Service interface {
	CreateBaseline(projectID string, name string, snapshot []byte) (*Baseline, error)
	ListBaselines(projectID string) ([]*Baseline, error)
	GetBaseline(id string) (*Baseline, error)
	DeleteBaseline(id string) error
}

// DefaultService provides baseline logic.
type DefaultService struct {
	repo Repository
}

// NewService creates a new baseline service.
func NewService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo}
}

// CreateBaseline stores a project snapshot as a baseline.
func (s *DefaultService) CreateBaseline(projectID string, name string, snapshot []byte) (*Baseline, error) {
	if projectID == "" {
		return nil, errors.New("project_id is required")
	}
	if name == "" {
		return nil, errors.New("baseline name is required")
	}
	if !json.Valid(snapshot) {
		return nil, errors.New("snapshot must be valid JSON")
	}

	baseline := &Baseline{
		ID:        uuid.New().String(),
		ProjectID: projectID,
		Name:      name,
		Snapshot:  json.RawMessage(snapshot),
		CreatedAt: time.Now(),
	}

	if err := s.repo.Create(baseline); err != nil {
		return nil, err
	}

	return baseline, nil
}

// ListBaselines returns baselines for a project.
func (s *DefaultService) ListBaselines(projectID string) ([]*Baseline, error) {
	return s.repo.ListByProjectID(projectID)
}

// GetBaseline retrieves a baseline by ID.
func (s *DefaultService) GetBaseline(id string) (*Baseline, error) {
	return s.repo.GetByID(id)
}

// DeleteBaseline removes a baseline by ID.
func (s *DefaultService) DeleteBaseline(id string) error {
	return s.repo.Delete(id)
}
