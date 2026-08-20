package products

import (
	"errors"
	"time"
)

// Error definitions
var (
	ErrProjectRequired = errors.New("project id is required")
)

// ProductProfile captures the product definition for a project.
type ProductProfile struct {
	ProjectID        string                   `json:"project_id"`
	Vision           string                   `json:"vision"`
	ProblemStatement string                   `json:"problem_statement"`
	TargetUsers      string                   `json:"target_users"`
	Constraints      []map[string]interface{} `json:"constraints"`
	SuccessMetrics   []map[string]interface{} `json:"success_metrics"`
	Settings         map[string]interface{}   `json:"settings"`
	CreatedAt        time.Time                `json:"created_at"`
	UpdatedAt        time.Time                `json:"updated_at"`
}

// UpdateProfileRequest is the payload for replacing a product profile.
// Full-replace semantics: every field overwrites the stored value.
type UpdateProfileRequest struct {
	Vision           string                   `json:"vision"`
	ProblemStatement string                   `json:"problem_statement"`
	TargetUsers      string                   `json:"target_users"`
	Constraints      []map[string]interface{} `json:"constraints"`
	SuccessMetrics   []map[string]interface{} `json:"success_metrics"`
	Settings         map[string]interface{}   `json:"settings"`
}

// Repository defines persistence operations for product profiles.
type Repository interface {
	Get(projectID string) (*ProductProfile, error) // nil, nil when no profile exists
	Upsert(p *ProductProfile) error
}

// Service defines product profile domain logic.
type Service interface {
	GetProfile(projectID string) (*ProductProfile, error)
	UpdateProfile(projectID string, req UpdateProfileRequest) (*ProductProfile, error)
}

// DefaultService implements the Service interface.
type DefaultService struct {
	repo Repository
}

// NewDefaultService creates a new product profile service.
func NewDefaultService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo}
}

// GetProfile returns the profile for a project, creating and persisting an
// empty profile if none exists yet.
func (s *DefaultService) GetProfile(projectID string) (*ProductProfile, error) {
	if projectID == "" {
		return nil, ErrProjectRequired
	}

	profile, err := s.repo.Get(projectID)
	if err != nil {
		return nil, err
	}
	if profile != nil {
		normalizeProfile(profile)
		return profile, nil
	}

	now := time.Now()
	profile = &ProductProfile{
		ProjectID:      projectID,
		Constraints:    []map[string]interface{}{},
		SuccessMetrics: []map[string]interface{}{},
		Settings:       map[string]interface{}{},
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.repo.Upsert(profile); err != nil {
		return nil, err
	}

	return profile, nil
}

// UpdateProfile replaces the profile contents for a project.
func (s *DefaultService) UpdateProfile(projectID string, req UpdateProfileRequest) (*ProductProfile, error) {
	profile, err := s.GetProfile(projectID)
	if err != nil {
		return nil, err
	}

	profile.Vision = req.Vision
	profile.ProblemStatement = req.ProblemStatement
	profile.TargetUsers = req.TargetUsers
	profile.Constraints = req.Constraints
	profile.SuccessMetrics = req.SuccessMetrics
	profile.Settings = req.Settings
	profile.UpdatedAt = time.Now()
	normalizeProfile(profile)

	if err := s.repo.Upsert(profile); err != nil {
		return nil, err
	}

	return profile, nil
}

// normalizeProfile ensures JSONB-backed fields are never nil.
func normalizeProfile(p *ProductProfile) {
	if p.Constraints == nil {
		p.Constraints = []map[string]interface{}{}
	}
	if p.SuccessMetrics == nil {
		p.SuccessMetrics = []map[string]interface{}{}
	}
	if p.Settings == nil {
		p.Settings = map[string]interface{}{}
	}
}
