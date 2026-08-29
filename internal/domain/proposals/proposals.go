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
	ID        string                 `json:"id"`
	RunID     string                 `json:"run_id"`
	ProjectID string                 `json:"project_id"`
	Op        string                 `json:"op"`
	TargetID  *string                `json:"target_id,omitempty"`
	Payload   map[string]interface{} `json:"payload"`
	// Ref is a caller-chosen temporary token an agent may attach to a
	// create_artifact proposal so that a sibling create_link proposal in the
	// same run can point at the not-yet-created artifact (issue #235). It is
	// unique within a run and never collides with a real artifact UUID. Only
	// create_artifact proposals carry one; it is resolved to the real artifact
	// id at apply time (see DefaultService.resolveLinkPayload). Empty for
	// proposals that mint no reference.
	Ref             string     `json:"ref,omitempty"`
	Status          string     `json:"status"`
	ReviewNote      string     `json:"review_note"`
	AppliedEntityID *string    `json:"applied_entity_id,omitempty"`
	ReviewedBy      *string    `json:"reviewed_by,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	ReviewedAt      *time.Time `json:"reviewed_at,omitempty"`
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

// SetAppliers wires (or rewires) the callbacks that execute approved
// proposals. It exists so the composition root can construct the service
// before the HTTP handler that supplies the appliers, then inject them once
// the handler is built — breaking the construction cycle (the handler needs
// the proposal service, the appliers need the handler).
func (s *DefaultService) SetAppliers(appliers Appliers) {
	s.appliers = appliers
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
	// Lift a caller-supplied temporary reference token out of the payload into
	// its own column (issue #235). Keeping it out of the payload means the
	// artifact applier's payload stays a clean CreateArtifactRequest, and a
	// sibling create_link proposal can find the ref with a plain column read
	// rather than digging through JSON.
	ref := ""
	if raw, ok := payload["ref"].(string); ok && raw != "" {
		ref = raw
		delete(payload, "ref")
	}
	p := &Proposal{
		ID:        uuid.New().String(),
		RunID:     runID,
		ProjectID: projectID,
		Op:        op,
		TargetID:  targetID,
		Payload:   payload,
		Ref:       ref,
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
		payload, err := s.resolveLinkPayload(p)
		if err != nil {
			return "", err
		}
		return s.appliers.CreateLink(payload)
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

// resolveLinkPayload returns a copy of a create_link proposal's payload with
// any from_id/to_id that names a sibling proposal's temporary ref replaced by
// the real artifact id that resulted from applying that sibling (issue #235).
//
// A from_id/to_id that matches no sibling ref is treated as a literal artifact
// id and passed through untouched — refs are caller-chosen tokens that never
// collide with a real UUID. When an endpoint IS a ref, the referenced artifact
// proposal must already be applied: if it is still pending, or was rejected or
// apply-failed, the link cannot resolve and this returns an error so the link
// proposal fails cleanly (apply_failed) instead of creating a dangling edge.
// This is why bulk-approve must order artifact proposals before the link
// proposals that reference them (see BulkReviewProposals).
func (s *DefaultService) resolveLinkPayload(p *Proposal) (map[string]interface{}, error) {
	fromID, _ := p.Payload["from_id"].(string)
	toID, _ := p.Payload["to_id"].(string)

	// Build the ref -> proposal index only from this run's siblings.
	siblings, err := s.repo.List("", "", p.RunID)
	if err != nil {
		return nil, err
	}
	refIndex := make(map[string]*Proposal, len(siblings))
	for _, sib := range siblings {
		if sib.Ref != "" {
			refIndex[sib.Ref] = sib
		}
	}

	resolvedFrom, err := resolveRef(fromID, refIndex)
	if err != nil {
		return nil, err
	}
	resolvedTo, err := resolveRef(toID, refIndex)
	if err != nil {
		return nil, err
	}
	if resolvedFrom == fromID && resolvedTo == toID {
		return p.Payload, nil // no refs in play; nothing to rewrite
	}

	out := make(map[string]interface{}, len(p.Payload))
	for k, v := range p.Payload {
		out[k] = v
	}
	out["from_id"] = resolvedFrom
	out["to_id"] = resolvedTo
	return out, nil
}

// resolveRef maps one link endpoint to a real artifact id. If id matches no
// sibling ref it is a literal id and returned unchanged; if it matches a ref
// the referenced artifact proposal must be applied, otherwise this errors.
func resolveRef(id string, refIndex map[string]*Proposal) (string, error) {
	sib, ok := refIndex[id]
	if !ok {
		return id, nil
	}
	if sib.Status == StatusApplied && sib.AppliedEntityID != nil && *sib.AppliedEntityID != "" {
		return *sib.AppliedEntityID, nil
	}
	return "", fmt.Errorf("cannot resolve pending-proposal reference %q: the artifact proposal it points to is %s (approve the artifact proposal before this link)", id, sib.Status)
}

func joinNote(a, b string) string {
	if a == "" {
		return b
	}
	return a + " | " + b
}
