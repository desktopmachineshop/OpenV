package hostedworkers

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Hosted worker statuses.
const (
	StatusProvisioning = "provisioning"
	StatusRunning      = "running"
	StatusStopped      = "stopped"
	StatusError        = "error"
)

var ErrNotFound = errors.New("hosted worker not found")

// HostedWorker is the record of an org's platform-managed runner container.
// At most one exists per org (org_id is unique). Provider API keys are passed
// to the container environment at provision time and are NEVER persisted.
type HostedWorker struct {
	ID            string    `json:"id"`
	OrgID         string    `json:"org_id"`
	ContainerName string    `json:"container_name"`
	WorkerKeyID   *string   `json:"worker_key_id,omitempty"`
	Status        string    `json:"status"` // provisioning|running|stopped|error
	Detail        string    `json:"detail"`
	CreatedBy     *string   `json:"created_by,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Repository defines hosted worker persistence.
type Repository interface {
	Save(w *HostedWorker) error
	Update(w *HostedWorker) error
	// FindByOrg returns the org's hosted worker, or nil.
	FindByOrg(orgID string) (*HostedWorker, error)
	// FindByID returns a hosted worker, or nil.
	FindByID(id string) (*HostedWorker, error)
	Delete(id string) error
	ListAll() ([]*HostedWorker, error)
}

// Service defines hosted worker logic (thin CRUD; container lifecycle lives
// in internal/hosting).
type Service interface {
	// Create records a hosted worker in status provisioning.
	Create(orgID, containerName string, workerKeyID, createdBy *string) (*HostedWorker, error)
	// Get returns the org's hosted worker, or nil.
	Get(orgID string) (*HostedWorker, error)
	// SetStatus updates a worker's status/detail and returns the record.
	SetStatus(id, status, detail string) (*HostedWorker, error)
	Delete(id string) error
	ListAll() ([]*HostedWorker, error)
}

// DefaultService implements Service.
type DefaultService struct {
	repo Repository
}

// NewDefaultService creates a hosted worker service.
func NewDefaultService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo}
}

// Create records a hosted worker in status provisioning.
func (s *DefaultService) Create(orgID, containerName string, workerKeyID, createdBy *string) (*HostedWorker, error) {
	if orgID == "" {
		return nil, errors.New("org_id is required")
	}
	if strings.TrimSpace(containerName) == "" {
		return nil, errors.New("container_name is required")
	}
	now := time.Now()
	w := &HostedWorker{
		ID:            uuid.New().String(),
		OrgID:         orgID,
		ContainerName: strings.TrimSpace(containerName),
		WorkerKeyID:   workerKeyID,
		Status:        StatusProvisioning,
		CreatedBy:     createdBy,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.repo.Save(w); err != nil {
		return nil, err
	}
	return w, nil
}

// Get returns the org's hosted worker, or nil.
func (s *DefaultService) Get(orgID string) (*HostedWorker, error) {
	return s.repo.FindByOrg(orgID)
}

// SetStatus updates a worker's status/detail.
func (s *DefaultService) SetStatus(id, status, detail string) (*HostedWorker, error) {
	w, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, ErrNotFound
	}
	w.Status = status
	w.Detail = detail
	w.UpdatedAt = time.Now()
	if err := s.repo.Update(w); err != nil {
		return nil, err
	}
	return w, nil
}

// Delete removes a hosted worker record.
func (s *DefaultService) Delete(id string) error {
	return s.repo.Delete(id)
}

// ListAll returns every hosted worker record (boot reconcile).
func (s *DefaultService) ListAll() ([]*HostedWorker, error) {
	return s.repo.ListAll()
}
