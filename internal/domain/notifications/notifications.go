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
	// TypeBudgetThreshold fires when a workspace's month-to-date agent spend
	// crosses 80% or 100% of its monthly budget, alerting org admins (issue
	// #186). One row per admin, deduped to once per threshold per month.
	TypeBudgetThreshold = "budget_threshold"
	// TypeAccessChanged tells one person what changed about their OWN access:
	// added to a workspace or project, given a different role, or removed.
	// One type rather than three, because it answers a single question —
	// "what can I do now?" — and three would only make the preference list
	// longer without making any of them separately worth muting.
	TypeAccessChanged = "access_changed"
	// TypeMembershipChanged tells a workspace's admins who joined, who left
	// and whose role changed. Separate from TypeAccessChanged because the
	// audience and the reason differ: this one is workspace governance, and
	// an admin may reasonably want it when they do not want their own
	// access notifications, or the other way round.
	TypeMembershipChanged = "membership_changed"
	// TypeReleasePublished tells every account that the platform has been
	// updated, with the release's customer-facing notes. One row per account
	// per release, deduped by the release_announcements claim.
	TypeReleasePublished = "release_published"
	// TypeReleaseScheduled tells a stable-channel workspace's admins that a
	// stable release has been cut and when it turns on for them, and again
	// a day before it does (REQ-138).
	TypeReleaseScheduled = "release_scheduled"
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
	// Flagged is the member's own "keep this in reach". Independent of
	// ClearedAt: flagging does not exempt a row from a clear, and a flagged
	// row that was cleared is still found by the flagged view.
	Flagged bool `json:"flagged"`
	// ClearedAt is nil while the notification is in the inbox, and stamped
	// when the member clears it. Clearing archives rather than deletes, so
	// the history stays readable.
	ClearedAt *time.Time `json:"cleared_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// View names the slice of a member's notifications a listing returns.
type View string

const (
	// ViewInbox is everything not yet cleared — what the bell badge counts.
	ViewInbox View = "inbox"
	// ViewFlagged is everything flagged, cleared or not: flagging is how a
	// member keeps something reachable after clearing it.
	ViewFlagged View = "flagged"
	// ViewCleared is the history — what has been cleared.
	ViewCleared View = "cleared"
)

// ParseView maps the wire value to a view, defaulting to the inbox so an
// absent or unknown parameter behaves the way the endpoint always has.
func ParseView(raw string) View {
	switch View(raw) {
	case ViewFlagged:
		return ViewFlagged
	case ViewCleared:
		return ViewCleared
	default:
		return ViewInbox
	}
}

// ListQuery is one page of one view.
//
// Paging is a keyset cursor rather than an offset: the history only grows,
// and an offset would skip or repeat rows as new notifications arrive at the
// top of it. Before is the last row of the previous page — rows strictly older
// than it come next, with the id breaking ties between rows that share a
// timestamp.
type ListQuery struct {
	View       View
	UnreadOnly bool
	Limit      int
	BeforeTime time.Time
	BeforeID   string
}

// HasCursor reports whether the query continues a previous page.
func (q ListQuery) HasCursor() bool { return !q.BeforeTime.IsZero() && q.BeforeID != "" }

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
		Flagged:   false,
		CreatedAt: time.Now(),
	}
}

// Repository persists notifications. Every read/write is scoped by user id
// in the query itself, so a caller can never touch another user's rows.
type Repository interface {
	Insert(n *Notification) error
	// List returns one page of one view, newest first.
	List(userID string, q ListQuery) ([]*Notification, error)
	// MarkRead marks the given ids read for that user only; rows belonging
	// to other users are silently unaffected. Returns rows updated.
	MarkRead(userID string, ids []string) (int64, error)
	// MarkAllRead marks every unread row of the user read. Returns rows updated.
	MarkAllRead(userID string) (int64, error)
	// ClearInbox archives every uncleared row of the user — stamping
	// cleared_at and marking it read — and returns how many moved. The rows
	// stay readable in the cleared view.
	ClearInbox(userID string) (int64, error)
	// SetFlagged flags or unflags one row of the user's own. Returns whether
	// a row matched, so a stale id answers 404 instead of pretending.
	SetFlagged(userID, id string, flagged bool) (bool, error)
	// DeleteCleared permanently removes the user's cleared rows. This is the
	// only path that destroys a notification.
	DeleteCleared(userID string) (int64, error)
	// CountUnread counts unread rows still in the inbox: a cleared row never
	// contributes to the badge.
	CountUnread(userID string) (int, error)
}

// Service is the thin domain façade over the repository; API handlers and
// the notify subscriber depend on this interface so tests can fake it.
type Service interface {
	Create(n *Notification) error
	List(userID string, q ListQuery) ([]*Notification, error)
	MarkRead(userID string, ids []string) (int64, error)
	MarkAllRead(userID string) (int64, error)
	// ClearInbox archives the inbox into the cleared view. Recoverable in the
	// sense that matters: the rows are still there to read.
	ClearInbox(userID string) (int64, error)
	// SetFlagged flags or unflags one of the user's own notifications.
	SetFlagged(userID, id string, flagged bool) (bool, error)
	// DeleteCleared permanently removes what the user has already cleared.
	// This one cannot be undone, so callers ask first.
	DeleteCleared(userID string) (int64, error)
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

func (s *DefaultService) List(userID string, q ListQuery) ([]*Notification, error) {
	return s.repo.List(userID, q)
}

func (s *DefaultService) MarkRead(userID string, ids []string) (int64, error) {
	return s.repo.MarkRead(userID, ids)
}

func (s *DefaultService) MarkAllRead(userID string) (int64, error) {
	return s.repo.MarkAllRead(userID)
}

func (s *DefaultService) ClearInbox(userID string) (int64, error) {
	return s.repo.ClearInbox(userID)
}

func (s *DefaultService) SetFlagged(userID, id string, flagged bool) (bool, error) {
	return s.repo.SetFlagged(userID, id, flagged)
}

func (s *DefaultService) DeleteCleared(userID string) (int64, error) {
	return s.repo.DeleteCleared(userID)
}

func (s *DefaultService) CountUnread(userID string) (int, error) {
	return s.repo.CountUnread(userID)
}
