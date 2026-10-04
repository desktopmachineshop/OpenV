package guided

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/products"
)

// Guided session status values.
const (
	StatusInProgress = "in-progress"
	StatusCommitted  = "committed"
	StatusAbandoned  = "abandoned"
)

// Chat message roles for the in-wizard copilot conversation.
const (
	ChatRoleAssistant = "assistant"
	ChatRoleUser      = "user"
	ChatRoleSystem    = "system"
)

// Error definitions
var (
	ErrSessionNotFound    = errors.New("guided session not found")
	ErrSessionNotEditable = errors.New("guided session is not in progress")
	ErrInvalidChatRole    = errors.New("invalid chat message role")
)

// Session is one pass through the guided product definition flow.
type Session struct {
	ID               string                 `json:"id"`
	ProjectID        string                 `json:"project_id"`
	Status           string                 `json:"status"`
	CurrentStep      int                    `json:"current_step"`
	Answers          map[string]interface{} `json:"answers"`
	DraftArtifactIDs []string               `json:"draft_artifact_ids"`
	AgentRunID       *string                `json:"agent_run_id,omitempty"`
	// PendingNudge is the newest wizard nudge that arrived while a copilot
	// run was still in flight. Exactly one is kept — a later nudge overwrites
	// it — and the finishing run launches one turn from it and clears it, so
	// a burst of step saves costs one reply instead of several unanswered
	// ones. Nil when nothing is waiting.
	PendingNudge *PendingNudge `json:"pending_nudge,omitempty"`
	CreatedBy    *string       `json:"created_by,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

// PendingNudge is a wizard action the copilot still owes a reply to: the
// step the user was on, the wizard state at that moment, and a short phrase
// naming what they did ("saved step 3").
type PendingNudge struct {
	Step  int                    `json:"step"`
	State map[string]interface{} `json:"state,omitempty"`
	Event string                 `json:"event"`
}

// ChatMessage is one turn in a session's copilot conversation.
type ChatMessage struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// CommitResult is what a commit did: the session, now committed, and every
// draft the commit approved, as the approval left it. A draft that was
// already approved, or superseded, is not listed: the commit left it alone.
// The approvals are reported so the caller can record who signed them off,
// as the status route does (the event stream is the sign-off history, issue
// #127).
type CommitResult struct {
	Session  *Session
	Approved []*artifacts.Artifact
}

// DraftLink describes a link to create from a draft artifact.
type DraftLink struct {
	Type string `json:"type"`
	ToID string `json:"to_id"`
}

// DraftSpec describes one draft artifact to materialize.
type DraftSpec struct {
	Type       string                 `json:"type"`
	Title      string                 `json:"title"`
	Body       string                 `json:"body"`
	ParentID   *string                `json:"parent_id,omitempty"`
	SortOrder  *int                   `json:"sort_order,omitempty"`
	Attributes map[string]interface{} `json:"attributes"`
	Links      []DraftLink            `json:"links"`
}

// Repository defines persistence operations for guided sessions.
type Repository interface {
	Save(s *Session) error
	Update(s *Session) error
	FindByID(id string) (*Session, error)
	ListByProject(projectID string) ([]*Session, error)

	SaveChatMessage(m *ChatMessage) error
	ListChatMessages(sessionID string) ([]*ChatMessage, error)

	// SetPendingNudge parks (or, with nil, clears) the session's waiting
	// nudge, overwriting whatever was there.
	SetPendingNudge(sessionID string, nudge *PendingNudge) error
	// TakePendingNudge returns the waiting nudge and clears it in the same
	// statement, so two finishing runs can never launch one nudge twice.
	// Returns nil when nothing is waiting.
	TakePendingNudge(sessionID string) (*PendingNudge, error)
}

// Service defines guided flow domain logic.
type Service interface {
	StartSession(projectID string, createdBy *string) (*Session, error)
	GetSession(id string) (*Session, error)
	ListSessions(projectID string) ([]*Session, error)
	SaveStep(sessionID string, step int, answers map[string]interface{}) (*Session, error)
	MaterializeDrafts(sessionID string, drafts []DraftSpec) ([]string, error)
	// Commit approves the session's drafts and closes it. When it fails
	// after approving some drafts, it still returns them with the error.
	Commit(sessionID string) (*CommitResult, error)
	Abandon(sessionID string) (*Session, error)

	AppendChatMessage(sessionID, role, content string) (*ChatMessage, error)
	GetChatTranscript(sessionID string) ([]*ChatMessage, error)
	AttachAgentRun(sessionID, runID string) error

	// SetPendingNudge stores the nudge a session owes a reply to, replacing
	// any earlier one (nil clears it).
	SetPendingNudge(sessionID string, nudge *PendingNudge) error
	// TakePendingNudge hands back the waiting nudge and clears it; nil when
	// there is none.
	TakePendingNudge(sessionID string) (*PendingNudge, error)
}

// DefaultService implements the Service interface.
type DefaultService struct {
	repo            Repository
	artifactService artifacts.Service
	linkService     links.Service
	chatterService  chatter.Service
	productService  products.Service
	bus             events.Bus
}

// NewDefaultService creates a new guided flow service. bus may be nil.
func NewDefaultService(
	repo Repository,
	artifactService artifacts.Service,
	linkService links.Service,
	chatterService chatter.Service,
	productService products.Service,
	bus events.Bus,
) *DefaultService {
	return &DefaultService{
		repo:            repo,
		artifactService: artifactService,
		linkService:     linkService,
		chatterService:  chatterService,
		productService:  productService,
		bus:             bus,
	}
}

// StartSession creates a new in-progress guided session.
func (s *DefaultService) StartSession(projectID string, createdBy *string) (*Session, error) {
	if projectID == "" {
		return nil, errors.New("project id is required")
	}

	now := time.Now().UTC() // TIMESTAMP columns hold UTC wall clocks (#379 bug 162)
	session := &Session{
		ID:               uuid.New().String(),
		ProjectID:        projectID,
		Status:           StatusInProgress,
		CurrentStep:      0,
		Answers:          map[string]interface{}{},
		DraftArtifactIDs: []string{},
		CreatedBy:        createdBy,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := s.repo.Save(session); err != nil {
		return nil, err
	}

	return session, nil
}

// GetSession retrieves a guided session by ID.
func (s *DefaultService) GetSession(id string) (*Session, error) {
	return s.repo.FindByID(id)
}

// ListSessions retrieves all guided sessions for a project.
func (s *DefaultService) ListSessions(projectID string) ([]*Session, error) {
	return s.repo.ListByProject(projectID)
}

// SaveStep stores answers for one step under the "step_<n>" key and advances
// the session's current step.
func (s *DefaultService) SaveStep(sessionID string, step int, answers map[string]interface{}) (*Session, error) {
	session, err := s.repo.FindByID(sessionID)
	if err != nil {
		return nil, err
	}
	if session.Status != StatusInProgress {
		return nil, ErrSessionNotEditable
	}

	if session.Answers == nil {
		session.Answers = map[string]interface{}{}
	}
	// The client sends a merged answers map keyed however it likes
	// (conventionally "step_N" / "step_N_ids"); merge keys at the top level
	// so partial saves never clobber other steps' answers.
	for key, value := range answers {
		session.Answers[key] = value
	}
	session.CurrentStep = step
	session.UpdatedAt = time.Now().UTC() // a TIMESTAMP holding a UTC wall clock (#379 bug 162)

	if err := s.repo.Update(session); err != nil {
		return nil, err
	}

	return session, nil
}

// MaterializeDrafts creates draft artifacts (and their links) for a session
// and returns the created artifact IDs.
func (s *DefaultService) MaterializeDrafts(sessionID string, drafts []DraftSpec) ([]string, error) {
	session, err := s.repo.FindByID(sessionID)
	if err != nil {
		return nil, err
	}
	if session.Status != StatusInProgress {
		return nil, ErrSessionNotEditable
	}

	createdIDs := []string{}
	for _, draft := range drafts {
		attrs := map[string]interface{}{}
		for k, v := range draft.Attributes {
			attrs[k] = v
		}
		attrs["status"] = "draft"
		if _, ok := attrs["origin"]; !ok {
			attrs["origin"] = "guided-flow"
		}

		artifact := artifacts.NewArtifact(artifacts.CreateArtifactRequest{
			ProjectID:  session.ProjectID,
			ParentID:   draft.ParentID,
			Type:       draft.Type,
			Title:      draft.Title,
			Body:       draft.Body,
			SortOrder:  draft.SortOrder,
			Attributes: attrs,
		})

		if err := s.artifactService.CreateArtifact(artifact); err != nil {
			return createdIDs, err
		}
		createdIDs = append(createdIDs, artifact.ID)

		for _, dl := range draft.Links {
			link := links.NewLink(links.CreateLinkRequest{
				FromID: artifact.ID,
				ToID:   dl.ToID,
				Type:   dl.Type,
			})
			if err := s.linkService.CreateLink(link); err != nil {
				// Non-fatal: the draft artifact itself was created.
				fmt.Printf("Warning: failed to create draft link %s -> %s: %v\n", artifact.ID, dl.ToID, err)
			}
		}
	}

	if session.DraftArtifactIDs == nil {
		session.DraftArtifactIDs = []string{}
	}
	session.DraftArtifactIDs = append(session.DraftArtifactIDs, createdIDs...)
	session.UpdatedAt = time.Now().UTC()

	if err := s.repo.Update(session); err != nil {
		return createdIDs, err
	}

	return createdIDs, nil
}

// Commit approves all draft artifacts of a session and marks it committed.
// If it fails part way, the result it returns with the error lists the drafts
// approved before the failure (Session is then nil): those approvals stand,
// and a retry leaves them alone, so they must be recorded now.
func (s *DefaultService) Commit(sessionID string) (*CommitResult, error) {
	session, err := s.repo.FindByID(sessionID)
	if err != nil {
		return nil, err
	}
	if session.Status != StatusInProgress {
		return nil, ErrSessionNotEditable
	}

	approved := []*artifacts.Artifact{}
	for _, artifactID := range session.DraftArtifactIDs {
		artifact, err := s.artifactService.GetArtifact(artifactID)
		if err != nil {
			// Draft may have been deleted since materialization; skip it.
			continue
		}

		signedOff, err := s.approveDraft(artifact)
		if err != nil {
			return &CommitResult{Approved: approved}, err
		}
		if signedOff != nil {
			approved = append(approved, signedOff)
		}

		entry := chatter.NewChatterEntry(artifactID, "Created via guided definition", true, "guided-flow")
		if err := s.chatterService.CreateEntry(entry); err != nil {
			fmt.Printf("Warning: failed to create chatter entry for guided commit: %v\n", err)
		}
	}

	session.Status = StatusCommitted
	session.UpdatedAt = time.Now().UTC()

	if err := s.repo.Update(session); err != nil {
		return &CommitResult{Approved: approved}, err
	}
	s.clearPendingNudge(session)

	return &CommitResult{Session: session, Approved: approved}, nil
}

// approveDraft signs off one of a session's drafts and returns it as approved,
// or nil when it was left alone. The status column is the authority (issue
// #127) and an attribute write never moves it, so the approval goes through
// ChangeStatus one legal transition at a time, as a person would make it from
// the artifact's header: a draft is submitted for review and then approved,
// each step a new version with the note a status change leaves in the
// artifact's history. An artifact already approved is left as it is, and so
// is a superseded one, which no transition leaves.
//
// Nothing is published here: the caller records each approval it is handed
// in the event stream, with the committing user as its actor. The step into
// review has no event, as the notifier would read it as a request for a
// review nobody is waiting for.
func (s *DefaultService) approveDraft(artifact *artifacts.Artifact) (*artifacts.Artifact, error) {
	from := artifacts.NormalizeStatus(artifact.Status)
	var steps []string
	switch from {
	case artifacts.StatusDraft:
		steps = []string{artifacts.StatusInReview, artifacts.StatusApproved}
	case artifacts.StatusInReview:
		steps = []string{artifacts.StatusApproved}
	}

	var updated *artifacts.Artifact
	for _, to := range steps {
		var err error
		updated, err = s.artifactService.ChangeStatus(artifact.ID, to)
		if err != nil {
			return nil, err
		}
		note := fmt.Sprintf("Status changed: %s → %s (guided definition)", from, updated.Status)
		entry := chatter.NewChatterEntry(artifact.ID, note, true, "status-change")
		if err := s.chatterService.CreateEntry(entry); err != nil {
			slog.Warn("guided: failed to create chatter entry for a committed draft's status change",
				"artifact_id", artifact.ID, "error", err)
		}
		from = updated.Status
	}
	return updated, nil
}

// clearPendingNudge drops any wizard nudge still parked on a session that has
// just closed. A committed or abandoned session must not get a copilot turn,
// and the parked nudge is the one thing that could still launch one. The
// session is already closed by the time this runs, so a failure is logged
// rather than returned — the hooks refuse a closed session's nudge anyway.
func (s *DefaultService) clearPendingNudge(session *Session) {
	if err := s.repo.SetPendingNudge(session.ID, nil); err != nil {
		slog.Warn("guided: failed to clear the parked nudge of a closed session",
			"session_id", session.ID, "status", session.Status, "error", err)
		return
	}
	session.PendingNudge = nil
}

// AppendChatMessage adds a message to a session's copilot conversation.
func (s *DefaultService) AppendChatMessage(sessionID, role, content string) (*ChatMessage, error) {
	switch role {
	case ChatRoleAssistant, ChatRoleUser, ChatRoleSystem:
	default:
		return nil, ErrInvalidChatRole
	}

	if _, err := s.repo.FindByID(sessionID); err != nil {
		return nil, err
	}

	message := &ChatMessage{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		Role:      role,
		Content:   content,
		CreatedAt: time.Now().UTC(), // a TIMESTAMP holding a UTC wall clock (#379 bug 162)
	}

	if err := s.repo.SaveChatMessage(message); err != nil {
		return nil, err
	}

	return message, nil
}

// GetChatTranscript retrieves the copilot conversation for a session.
func (s *DefaultService) GetChatTranscript(sessionID string) ([]*ChatMessage, error) {
	return s.repo.ListChatMessages(sessionID)
}

// AttachAgentRun records the latest copilot turn run launched for a session.
func (s *DefaultService) AttachAgentRun(sessionID, runID string) error {
	session, err := s.repo.FindByID(sessionID)
	if err != nil {
		return err
	}
	session.AgentRunID = &runID
	session.UpdatedAt = time.Now().UTC()
	return s.repo.Update(session)
}

// SetPendingNudge parks the wizard nudge a session still owes a reply to.
func (s *DefaultService) SetPendingNudge(sessionID string, nudge *PendingNudge) error {
	return s.repo.SetPendingNudge(sessionID, nudge)
}

// TakePendingNudge returns the waiting nudge and clears it.
func (s *DefaultService) TakePendingNudge(sessionID string) (*PendingNudge, error) {
	return s.repo.TakePendingNudge(sessionID)
}

// Abandon marks a session abandoned, leaving any drafts in place.
func (s *DefaultService) Abandon(sessionID string) (*Session, error) {
	session, err := s.repo.FindByID(sessionID)
	if err != nil {
		return nil, err
	}

	session.Status = StatusAbandoned
	session.UpdatedAt = time.Now().UTC()

	if err := s.repo.Update(session); err != nil {
		return nil, err
	}
	s.clearPendingNudge(session)

	return session, nil
}
