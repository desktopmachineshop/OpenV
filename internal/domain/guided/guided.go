package guided

import (
	"errors"
	"fmt"
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
	ErrSessionNotFound   = errors.New("guided session not found")
	ErrSessionNotEditable = errors.New("guided session is not in progress")
	ErrInvalidChatRole   = errors.New("invalid chat message role")
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
	CreatedBy        *string                `json:"created_by,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

// ChatMessage is one turn in a session's copilot conversation.
type ChatMessage struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
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
}

// Service defines guided flow domain logic.
type Service interface {
	StartSession(projectID string, createdBy *string) (*Session, error)
	GetSession(id string) (*Session, error)
	ListSessions(projectID string) ([]*Session, error)
	SaveStep(sessionID string, step int, answers map[string]interface{}) (*Session, error)
	MaterializeDrafts(sessionID string, drafts []DraftSpec) ([]string, error)
	Commit(sessionID string) (*Session, error)
	Abandon(sessionID string) (*Session, error)

	AppendChatMessage(sessionID, role, content string) (*ChatMessage, error)
	GetChatTranscript(sessionID string) ([]*ChatMessage, error)
	AttachAgentRun(sessionID, runID string) error
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

	now := time.Now()
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
	session.UpdatedAt = time.Now()

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
	session.UpdatedAt = time.Now()

	if err := s.repo.Update(session); err != nil {
		return createdIDs, err
	}

	return createdIDs, nil
}

// Commit approves all draft artifacts of a session and marks it committed.
func (s *DefaultService) Commit(sessionID string) (*Session, error) {
	session, err := s.repo.FindByID(sessionID)
	if err != nil {
		return nil, err
	}
	if session.Status != StatusInProgress {
		return nil, ErrSessionNotEditable
	}

	for _, artifactID := range session.DraftArtifactIDs {
		artifact, err := s.artifactService.GetArtifact(artifactID)
		if err != nil {
			// Draft may have been deleted since materialization; skip it.
			continue
		}

		attrs := artifact.Attributes
		if attrs == nil {
			attrs = map[string]interface{}{}
		}
		attrs["status"] = "approved"

		_, err = s.artifactService.UpdateArtifact(artifactID, artifacts.UpdateArtifactRequest{
			ParentID:   artifact.ParentID,
			Type:       artifact.Type,
			Title:      artifact.Title,
			Body:       artifact.Body,
			SortOrder:  &artifact.SortOrder,
			Attributes: attrs,
		})
		if err != nil {
			return nil, err
		}

		entry := chatter.NewChatterEntry(artifactID, "Created via guided definition", true, "guided-flow")
		if err := s.chatterService.CreateEntry(entry); err != nil {
			fmt.Printf("Warning: failed to create chatter entry for guided commit: %v\n", err)
		}
	}

	session.Status = StatusCommitted
	session.UpdatedAt = time.Now()

	if err := s.repo.Update(session); err != nil {
		return nil, err
	}

	return session, nil
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
		CreatedAt: time.Now(),
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
	session.UpdatedAt = time.Now()
	return s.repo.Update(session)
}

// Abandon marks a session abandoned, leaving any drafts in place.
func (s *DefaultService) Abandon(sessionID string) (*Session, error) {
	session, err := s.repo.FindByID(sessionID)
	if err != nil {
		return nil, err
	}

	session.Status = StatusAbandoned
	session.UpdatedAt = time.Now()

	if err := s.repo.Update(session); err != nil {
		return nil, err
	}

	return session, nil
}
