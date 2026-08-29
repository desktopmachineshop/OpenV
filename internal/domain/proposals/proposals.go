package proposals

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Proposal operations.
const (
	OpCreateArtifact   = "create_artifact"
	OpUpdateArtifact   = "update_artifact"
	OpDeleteArtifact   = "delete_artifact"
	OpCreateLink       = "create_link"
	OpDeleteLink       = "delete_link"
	OpRecordTestResult = "record_test_result"
)

// Proposal statuses.
const (
	StatusPending     = "pending"
	StatusApproved    = "approved"
	StatusRejected    = "rejected"
	StatusApplied     = "applied"
	StatusApplyFailed = "apply_failed"
)

// MaxProposalsPerRun caps a single run's write volume (blast-radius guard).
const MaxProposalsPerRun = 100

var (
	ErrNotFound     = errors.New("proposal not found")
	ErrNotPending   = errors.New("proposal has already been reviewed")
	ErrRunWriteCap  = errors.New("run has reached its proposal limit")
	ErrUnsupportedOp = errors.New("unsupported proposal operation")
)

// Proposal is a pending agent write awaiting human review.
type Proposal struct {
	ID              string                 `json:"id"`
	RunID           string                 `json:"run_id"`
	ProjectID       string                 `json:"project_id"`
	Op              string                 `json:"op"`
	TargetID        *string                `json:"target_id,omitempty"`
	Payload         map[string]interface{} `json:"payload"`
	Status          string                 `json:"status"`
	ReviewNote      string                 `json:"review_note"`
	AppliedEntityID *string                `json:"applied_entity_id,omitempty"`
	ReviewedBy      *string                `json:"reviewed_by,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
	ReviewedAt      *time.Time             `json:"reviewed_at,omitempty"`
}

// Repository defines proposal persistence.
type Repository interface {
	Save(p *Proposal) error
	Update(p *Proposal) error
	FindByID(id string) (*Proposal, error)
	List(projectID, status, runID string) ([]*Proposal, error)
	CountByRun(runID string) (int, error)
}

// Appliers are the callbacks that execute an approved proposal against the
// real services. Wired in main.go; keeps this package decoupled from the
// artifact/link/vv packages.
type Appliers struct {
	CreateArtifact   func(payload map[string]interface{}) (string, error)
	UpdateArtifact   func(targetID string, payload map[string]interface{}) (string, error)
	DeleteArtifact   func(targetID string) error
	CreateLink       func(payload map[string]interface{}) (string, error)
	DeleteLink       func(targetID string) error
	RecordTestResult func(payload map[string]interface{}) (string, error)
}

// Service defines proposal domain logic.
type Service interface {
	// Propose records a pending write from an agent run.
	Propose(runID, projectID, op string, targetID *string, payload map[string]interface{}) (*Proposal, error)
	Get(id string) (*Proposal, error)
	List(projectID, status, runID string) ([]*Proposal, error)
	// Approve applies the proposal via the wired appliers.
	Approve(id string, reviewedBy *string, note string) (*Proposal, error)
	Reject(id string, reviewedBy *string, note string) (*Proposal, error)
}

// DefaultService implements Service.
type DefaultService struct {
	repo       Repository
	appliers   Appliers
	onResolved func(runID string)
}

// NewDefaultService creates a proposal service.
func NewDefaultService(repo Repository, appliers Appliers) *DefaultService {
	return &DefaultService{repo: repo, appliers: appliers}
}

// OnResolved registers a callback fired after any of a run's proposals is
// resolved — approved (whether the write applied or apply-failed) or rejected.
// The run service uses it to finalize a run that is awaiting approval once its
// last proposal is reviewed. Routing every resolution through the service
// (rather than the HTTP handlers) also covers the applier path: an approval
// whose write fails still resolves the proposal to apply_failed, and the
// finalize check must run for it too. Call during wiring only.
func (s *DefaultService) OnResolved(fn func(runID string)) {
	s.onResolved = fn
}

func (s *DefaultService) notifyResolved(runID string) {
	if s.onResolved != nil {
		s.onResolved(runID)
	}
}

var validOps = map[string]bool{
	OpCreateArtifact:   true,
	OpUpdateArtifact:   true,
	OpDeleteArtifact:   true,
	OpCreateLink:       true,
	OpDeleteLink:       true,
	OpRecordTestResult: true,
}

// Propose records a pending agent write.
func (s *DefaultService) Propose(runID, projectID, op string, targetID *string, payload map[string]interface{}) (*Proposal, error) {
	if !validOps[op] {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedOp, op)
	}
	count, err := s.repo.CountByRun(runID)
	if err != nil {
		return nil, err
	}
	if count >= MaxProposalsPerRun {
		return nil, ErrRunWriteCap
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}
	p := &Proposal{
		ID:        uuid.New().String(),
		RunID:     runID,
		ProjectID: projectID,
		Op:        op,
		TargetID:  targetID,
		Payload:   payload,
		Status:    StatusPending,
		CreatedAt: time.Now(),
	}
	if err := s.repo.Save(p); err != nil {
		return nil, err
	}
	return p, nil
}

// Get returns a proposal by id.
func (s *DefaultService) Get(id string) (*Proposal, error) {
	p, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrNotFound
	}
	return p, nil
}

// List returns proposals filtered by any of project, status, run.
func (s *DefaultService) List(projectID, status, runID string) ([]*Proposal, error) {
	return s.repo.List(projectID, status, runID)
}

// Approve applies a pending proposal.
func (s *DefaultService) Approve(id string, reviewedBy *string, note string) (*Proposal, error) {
	p, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if p.Status != StatusPending {
		return nil, ErrNotPending
	}

	now := time.Now()
	p.ReviewedBy = reviewedBy
	p.ReviewNote = note
	p.ReviewedAt = &now

	entityID, applyErr := s.apply(p)
	if applyErr != nil {
		p.Status = StatusApplyFailed
		p.ReviewNote = joinNote(note, "apply failed: "+applyErr.Error())
	} else {
		p.Status = StatusApplied
		if entityID != "" {
			p.AppliedEntityID = &entityID
		}
	}
	if err := s.repo.Update(p); err != nil {
		return nil, err
	}
	s.notifyResolved(p.RunID)
	if applyErr != nil {
		return p, applyErr
	}
	return p, nil
}

// Reject declines a pending proposal.
func (s *DefaultService) Reject(id string, reviewedBy *string, note string) (*Proposal, error) {
	p, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if p.Status != StatusPending {
		return nil, ErrNotPending
	}
	now := time.Now()
	p.Status = StatusRejected
	p.ReviewedBy = reviewedBy
	p.ReviewNote = note
	p.ReviewedAt = &now
	if err := s.repo.Update(p); err != nil {
		return nil, err
	}
	s.notifyResolved(p.RunID)
	return p, nil
}

func (s *DefaultService) apply(p *Proposal) (string, error) {
	target := ""
	if p.TargetID != nil {
		target = *p.TargetID
	}
	switch p.Op {
	case OpCreateArtifact:
		if s.appliers.CreateArtifact == nil {
			return "", ErrUnsupportedOp
		}
		return s.appliers.CreateArtifact(p.Payload)
	case OpUpdateArtifact:
		if s.appliers.UpdateArtifact == nil {
			return "", ErrUnsupportedOp
		}
		return s.appliers.UpdateArtifact(target, p.Payload)
	case OpDeleteArtifact:
		if s.appliers.DeleteArtifact == nil {
			return "", ErrUnsupportedOp
		}
		return target, s.appliers.DeleteArtifact(target)
	case OpCreateLink:
		if s.appliers.CreateLink == nil {
			return "", ErrUnsupportedOp
		}
		return s.appliers.CreateLink(p.Payload)
	case OpDeleteLink:
		if s.appliers.DeleteLink == nil {
			return "", ErrUnsupportedOp
		}
		return target, s.appliers.DeleteLink(target)
	case OpRecordTestResult:
		if s.appliers.RecordTestResult == nil {
			return "", ErrUnsupportedOp
		}
		return s.appliers.RecordTestResult(p.Payload)
	}
	return "", ErrUnsupportedOp
}

func joinNote(a, b string) string {
	if a == "" {
		return b
	}
	return a + " | " + b
}
