// Package notifications holds the in-app notification domain (issue #132):
// a per-user inbox row created by the notify fan-out subscriber and read /
// acknowledged through the API. Delivery to open browser tabs happens over
// the shared SSE hub (key "notify:<user_id>"); the rows here are the durable
// backlog behind the bell badge.
package notifications

import (
	"time"

	"github.com/google/uuid"
)

// Notification types.
const (
	TypeProposalPending    = "proposal_pending"
	TypeRunFailed          = "run_failed"
	TypeInterviewCompleted = "interview_completed"
	TypeMention            = "mention"
	// TypeReviewRequested fires when an artifact enters the in_review state,
	// telling project reviewers (editors+) it is waiting in the review queue
	// (issue #183).
	TypeReviewRequested = "review_requested"
)

// Notification is one inbox entry for one user. EntityRef points the
// frontend at the subject ("kind" plus ids, e.g. {"kind":"run",
// "run_id":..., "project_id":...}) so clicking the notification can
// navigate; it is stored as jsonb and never interpreted server-side.
type Notification struct {
	ID        string                 `json:"id"`
	OrgID     string                 `json:"org_id,omitempty"`
	UserID    string                 `json:"user_id"`
	Type      string                 `json:"type"`
	Title     string                 `json:"title"`
	Body      string                 `json:"body,omitempty"`
	EntityRef map[string]interface{} `json:"entity_ref"`
	Read      bool                   `json:"read"`
	CreatedAt time.Time              `json:"created_at"`
}

// New creates an unread notification with a fresh id and timestamp.
func New(orgID, userID, ntype, title, body string, entityRef map[string]interface{}) *Notification {
	if entityRef == nil {
		entityRef = map[string]interface{}{}
	}
	return &Notification{
		ID:        uuid.New().String(),
		OrgID:     orgID,
		UserID:    userID,
		Type:      ntype,
		Title:     title,
		Body:      body,
		EntityRef: entityRef,
		Read:      false,
		CreatedAt: time.Now(),
	}
}

// Repository persists notifications. Every read/write is scoped by user id
// in the query itself, so a caller can never touch another user's rows.
type Repository interface {
	Insert(n *Notification) error
	// ListForUser returns the user's notifications, newest first.
	ListForUser(userID string, unreadOnly bool, limit int) ([]*Notification, error)
	// MarkRead marks the given ids read for that user only; rows belonging
	// to other users are silently unaffected. Returns rows updated.
	MarkRead(userID string, ids []string) (int64, error)
	// MarkAllRead marks every unread row of the user read. Returns rows updated.
	MarkAllRead(userID string) (int64, error)
	CountUnread(userID string) (int, error)
}

// Service is the thin domain façade over the repository; API handlers and
// the notify subscriber depend on this interface so tests can fake it.
type Service interface {
	Create(n *Notification) error
	ListForUser(userID string, unreadOnly bool, limit int) ([]*Notification, error)
	MarkRead(userID string, ids []string) (int64, error)
	MarkAllRead(userID string) (int64, error)
	CountUnread(userID string) (int, error)
}

// DefaultService implements Service.
type DefaultService struct {
	repo Repository
}

// NewDefaultService creates the service.
func NewDefaultService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo}
}

func (s *DefaultService) Create(n *Notification) error { return s.repo.Insert(n) }

func (s *DefaultService) ListForUser(userID string, unreadOnly bool, limit int) ([]*Notification, error) {
	return s.repo.ListForUser(userID, unreadOnly, limit)
}

func (s *DefaultService) MarkRead(userID string, ids []string) (int64, error) {
	return s.repo.MarkRead(userID, ids)
}

func (s *DefaultService) MarkAllRead(userID string) (int64, error) {
	return s.repo.MarkAllRead(userID)
}

func (s *DefaultService) CountUnread(userID string) (int, error) {
	return s.repo.CountUnread(userID)
}
