package artifacts

import (
	"errors"
	"fmt"
	"time"
)

// Artifact review statuses (issue #127). Status is a first-class column on
// artifacts; Attributes["status"] is mirrored on every write purely for
// backward compatibility with pre-column readers (CSV export, older clients)
// and is DEPRECATED as a write target — the column is authoritative and
// attribute writes never change it.
const (
	StatusDraft      = "draft"
	StatusInReview   = "in_review"
	StatusApproved   = "approved"
	StatusSuperseded = "superseded"
)

// Sentinel errors for status writes. The API maps ErrInvalidStatus to 400 and
// ErrInvalidStatusTransition to 409.
var (
	ErrInvalidStatus           = errors.New("invalid artifact status")
	ErrInvalidStatusTransition = errors.New("invalid artifact status transition")
)

// statusTransitions is the review state machine:
//
//	draft <-> in_review -> approved -> superseded
//
// Superseded is terminal. A NEW VERSION of an approved artifact (a content
// edit) is not a transition here: UpdateArtifact resets the new version to
// draft while the archived approved row keeps its status — versioning is
// temporal, so the approved snapshot survives untouched in history.
var statusTransitions = map[string][]string{
	StatusDraft:      {StatusInReview},
	StatusInReview:   {StatusDraft, StatusApproved},
	StatusApproved:   {StatusSuperseded},
	StatusSuperseded: {},
}

// ValidStatus reports whether s is a known status value.
func ValidStatus(s string) bool {
	_, ok := statusTransitions[s]
	return ok
}

// CanTransition reports whether the state machine allows from -> to.
func CanTransition(from, to string) bool {
	for _, next := range statusTransitions[from] {
		if next == to {
			return true
		}
	}
	return false
}

// NormalizeStatus maps legacy attribute spellings onto the canonical
// vocabulary: "in-review" (the issue-#127 spelling some data may carry)
// becomes in_review, any unknown or empty value becomes draft.
func NormalizeStatus(s string) string {
	if s == "in-review" {
		return StatusInReview
	}
	if ValidStatus(s) {
		return s
	}
	return StatusDraft
}

// syncStatusAttribute mirrors the status column into Attributes["status"] so
// pre-column read paths keep working. Called on every domain write; the
// mirror also prevents a client from smuggling a status change (e.g.
// self-approval) through the wholesale Attributes replacement in
// UpdateArtifact (issue #125): whatever the request carried is overwritten
// with the authoritative column value.
func (a *Artifact) syncStatusAttribute() {
	if a.Attributes == nil {
		a.Attributes = make(map[string]interface{})
	}
	a.Attributes["status"] = a.Status
}

// ChangeStatus applies one review-state transition to the current version of
// an artifact and persists it as a new temporal version (so status history is
// auditable alongside content history).
//
// Role note: the API gates every transition — including approval — at
// project editor+. Owner-only approval would be stricter, but the membership
// model has no per-action granularity between editor and owner today, so
// editor-approval is the documented compromise; revisit if roles grow.
func (s *DefaultService) ChangeStatus(id string, to string) (*Artifact, error) {
	if !ValidStatus(to) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidStatus, to)
	}

	artifact, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}

	from := NormalizeStatus(artifact.Status)
	if !CanTransition(from, to) {
		return nil, fmt.Errorf("%w: %s -> %s", ErrInvalidStatusTransition, from, to)
	}

	artifact.Status = to
	artifact.syncStatusAttribute()
	artifact.Version++
	now := time.Now()
	artifact.ValidFrom = now
	artifact.UpdatedAt = now

	if err := s.repo.Update(artifact); err != nil {
		return nil, err
	}
	return artifact, nil
}
