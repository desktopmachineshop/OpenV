package interviews

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Interview status values.
const (
	InterviewStatusOpen   = "open"
	InterviewStatusClosed = "closed"
)

// Interview session status values.
const (
	SessionStatusActive    = "active"
	SessionStatusCompleted = "completed"
)

// Message roles.
const (
	RoleAssistant   = "assistant"
	RoleParticipant = "participant"
	RoleSystem      = "system"
)

// Error definitions
var (
	ErrInterviewNotFound = errors.New("interview not found")
	ErrInterviewClosed   = errors.New("interview is closed")
	ErrInviteNotFound    = errors.New("invite not found")
	ErrInviteRevoked     = errors.New("invite has been revoked")
	ErrInviteExpired     = errors.New("invite has expired")
	ErrSessionNotFound   = errors.New("interview session not found")
	ErrInvalidRole       = errors.New("invalid message role")
)

// Interview is an agent-led stakeholder interview campaign. Many interviews
// may link to the same persona artifact, so the interviews for one persona
// (e.g. a team of design engineers) can be compared side by side.
type Interview struct {
	ID                string     `json:"id"`
	ProjectID         string     `json:"project_id"`
	GuidedSessionID   *string    `json:"guided_session_id,omitempty"`
	PersonaArtifactID *string    `json:"persona_artifact_id,omitempty"`
	Name              string     `json:"name"`
	Brief             string     `json:"brief"`
	AgentID           *string    `json:"agent_id,omitempty"`
	Status            string     `json:"status"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	CreatedBy         *string    `json:"created_by,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// Invite is a tokenized invitation to join an interview. Only the SHA-256
// hash of the token is stored.
type Invite struct {
	ID           string     `json:"id"`
	InterviewID  string     `json:"interview_id"`
	TokenHash    string     `json:"-"`
	InviteeLabel string     `json:"invitee_label"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	Revoked      bool       `json:"revoked"`
	CreatedAt    time.Time  `json:"created_at"`
}

// Session is one participant's conversation within an interview.
type Session struct {
	ID              string     `json:"id"`
	InterviewID     string     `json:"interview_id"`
	InviteID        string     `json:"invite_id"`
	ParticipantName string     `json:"participant_name"`
	Status          string     `json:"status"`
	Summary         string     `json:"summary"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
}

// Message is a single turn in an interview session transcript.
type Message struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// Repository defines persistence operations for interviews.
type Repository interface {
	SaveInterview(i *Interview) error
	UpdateInterview(i *Interview) error
	FindInterviewByID(id string) (*Interview, error)
	ListInterviewsByProject(projectID string) ([]*Interview, error)

	SaveInvite(inv *Invite) error
	FindInviteByID(id string) (*Invite, error) // nil, nil when none
	FindInviteByTokenHash(tokenHash string) (*Invite, error)
	ListInvitesByInterview(interviewID string) ([]*Invite, error)
	RevokeInvite(id string) error

	SaveSession(s *Session) error
	UpdateSession(s *Session) error
	// SetParticipantName fills the participant name on a still-anonymous
	// session with a targeted single-column write (only when the stored name is
	// NULL or empty), so a name backfill can never resurrect a stale
	// status/summary/ended_at over a concurrent CompleteSession.
	SetParticipantName(sessionID, name string) error
	FindSessionByID(id string) (*Session, error)
	FindActiveSessionByInvite(inviteID string) (*Session, error) // nil, nil when none
	ListSessionsByInterview(interviewID string) ([]*Session, error)
	// ListSessionsByProject returns the most recent sessions across every
	// interview in a project, newest first, at most limit rows.
	ListSessionsByProject(projectID string, limit int) ([]*Session, error)

	SaveMessage(m *Message) error
	ListMessagesBySession(sessionID string) ([]*Message, error)
}

// Service defines interview domain logic. Turn-by-turn agent orchestration
// lives outside this package.
type Service interface {
	CreateInterview(projectID, name, brief string, agentID *string, guidedSessionID *string, personaArtifactID *string, createdBy *string) (*Interview, error)
	GetInterview(id string) (*Interview, error)
	ListInterviews(projectID string) ([]*Interview, error)
	CloseInterview(id string) (*Interview, error)
	SetInterviewPersona(id string, personaArtifactID *string) (*Interview, error)

	CreateInvite(interviewID, inviteeLabel string, expiresAt *time.Time) (*Invite, string, error)
	GetInvite(id string) (*Invite, error)
	ListInvites(interviewID string) ([]*Invite, error)
	RevokeInvite(id string) error
	ResolveInviteToken(rawToken string) (*Interview, *Invite, error)

	StartOrResumeSession(inviteID, interviewID, participantName string) (*Session, error)
	FindActiveSession(inviteID string) (*Session, error) // nil, nil when none
	GetSession(id string) (*Session, error)
	ListSessions(interviewID string) ([]*Session, error)
	ListProjectSessions(projectID string, limit int) ([]*Session, error)
	AppendMessage(sessionID, role, content string) (*Message, error)
	GetTranscript(sessionID string) ([]*Message, error)
	CompleteSession(sessionID, summary string) error
}

// DefaultService implements the Service interface.
type DefaultService struct {
	repo Repository
}

// NewDefaultService creates a new interview service.
func NewDefaultService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo}
}

// generateToken returns a 32-byte random token as hex.
func generateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// hashToken returns the SHA-256 hex digest of a raw token.
func hashToken(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}

// CreateInterview creates a new open interview.
func (s *DefaultService) CreateInterview(projectID, name, brief string, agentID *string, guidedSessionID *string, personaArtifactID *string, createdBy *string) (*Interview, error) {
	if projectID == "" {
		return nil, errors.New("project id is required")
	}
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("interview name is required")
	}

	now := time.Now()
	interview := &Interview{
		ID:                uuid.New().String(),
		ProjectID:         projectID,
		GuidedSessionID:   guidedSessionID,
		PersonaArtifactID: personaArtifactID,
		Name:              name,
		Brief:             brief,
		AgentID:           agentID,
		Status:            InterviewStatusOpen,
		CreatedBy:         createdBy,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if err := s.repo.SaveInterview(interview); err != nil {
		return nil, err
	}

	return interview, nil
}

// GetInterview retrieves an interview by ID.
func (s *DefaultService) GetInterview(id string) (*Interview, error) {
	return s.repo.FindInterviewByID(id)
}

// ListInterviews retrieves all interviews for a project.
func (s *DefaultService) ListInterviews(projectID string) ([]*Interview, error) {
	return s.repo.ListInterviewsByProject(projectID)
}

// CloseInterview marks an interview closed.
func (s *DefaultService) CloseInterview(id string) (*Interview, error) {
	interview, err := s.repo.FindInterviewByID(id)
	if err != nil {
		return nil, err
	}

	interview.Status = InterviewStatusClosed
	interview.UpdatedAt = time.Now()

	if err := s.repo.UpdateInterview(interview); err != nil {
		return nil, err
	}

	return interview, nil
}

// SetInterviewPersona links an interview to a persona artifact, or clears
// the link when personaArtifactID is nil. Callers are responsible for
// validating that the artifact is a persona in the interview's project.
func (s *DefaultService) SetInterviewPersona(id string, personaArtifactID *string) (*Interview, error) {
	interview, err := s.repo.FindInterviewByID(id)
	if err != nil {
		return nil, err
	}

	interview.PersonaArtifactID = personaArtifactID
	interview.UpdatedAt = time.Now()

	if err := s.repo.UpdateInterview(interview); err != nil {
		return nil, err
	}

	return interview, nil
}

// CreateInvite creates an invite for an interview and returns it along with
// the raw token. The raw token is returned exactly once and never stored.
func (s *DefaultService) CreateInvite(interviewID, inviteeLabel string, expiresAt *time.Time) (*Invite, string, error) {
	interview, err := s.repo.FindInterviewByID(interviewID)
	if err != nil {
		return nil, "", err
	}
	if interview.Status != InterviewStatusOpen {
		return nil, "", ErrInterviewClosed
	}

	rawToken, err := generateToken()
	if err != nil {
		return nil, "", err
	}

	invite := &Invite{
		ID:           uuid.New().String(),
		InterviewID:  interviewID,
		TokenHash:    hashToken(rawToken),
		InviteeLabel: inviteeLabel,
		ExpiresAt:    expiresAt,
		Revoked:      false,
		CreatedAt:    time.Now(),
	}

	if err := s.repo.SaveInvite(invite); err != nil {
		return nil, "", err
	}

	return invite, rawToken, nil
}

// GetInvite retrieves an invite by ID.
func (s *DefaultService) GetInvite(id string) (*Invite, error) {
	invite, err := s.repo.FindInviteByID(id)
	if err != nil {
		return nil, err
	}
	if invite == nil {
		return nil, ErrInviteNotFound
	}
	return invite, nil
}

// ListInvites retrieves all invites for an interview.
func (s *DefaultService) ListInvites(interviewID string) ([]*Invite, error) {
	return s.repo.ListInvitesByInterview(interviewID)
}

// RevokeInvite marks an invite revoked.
func (s *DefaultService) RevokeInvite(id string) error {
	return s.repo.RevokeInvite(id)
}

// ResolveInviteToken validates a raw invite token and returns the matching
// interview and invite. Errors when the token is unknown, revoked, expired,
// or the interview is closed.
func (s *DefaultService) ResolveInviteToken(rawToken string) (*Interview, *Invite, error) {
	invite, err := s.repo.FindInviteByTokenHash(hashToken(rawToken))
	if err != nil {
		return nil, nil, err
	}
	if invite == nil {
		return nil, nil, ErrInviteNotFound
	}
	if invite.Revoked {
		return nil, nil, ErrInviteRevoked
	}
	now := time.Now()
	if invite.ExpiresAt != nil && invite.ExpiresAt.Before(now) {
		return nil, nil, ErrInviteExpired
	}

	interview, err := s.repo.FindInterviewByID(invite.InterviewID)
	if err != nil {
		return nil, nil, err
	}
	if interview.Status != InterviewStatusOpen {
		return nil, nil, ErrInterviewClosed
	}
	if interview.ExpiresAt != nil && interview.ExpiresAt.Before(now) {
		return nil, nil, ErrInterviewClosed
	}

	return interview, invite, nil
}

// StartOrResumeSession returns the active session for an invite, creating a
// new one when none exists.
func (s *DefaultService) StartOrResumeSession(inviteID, interviewID, participantName string) (*Session, error) {
	existing, err := s.repo.FindActiveSessionByInvite(inviteID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		// Backfill the participant name onto a still-anonymous session. The
		// SSE stream connects (and creates the session) before the name gate
		// submits, so the active session is usually created anonymously; the
		// first message then carries the real name and must persist it, or the
		// reviewer only ever sees "Anonymous participant". Only fill a blank —
		// never overwrite a name already recorded. (#205)
		name := strings.TrimSpace(participantName)
		if name != "" && strings.TrimSpace(existing.ParticipantName) == "" {
			// Targeted single-column write, not a full-row UpdateSession from
			// this (possibly stale) read: a concurrent CompleteSession may have
			// set status/summary/ended_at since we loaded `existing`, and a
			// full-row update would revert them. (#212)
			if err := s.repo.SetParticipantName(existing.ID, name); err != nil {
				return nil, err
			}
			existing.ParticipantName = name
		}
		return existing, nil
	}

	session := &Session{
		ID:              uuid.New().String(),
		InterviewID:     interviewID,
		InviteID:        inviteID,
		ParticipantName: participantName,
		Status:          SessionStatusActive,
		StartedAt:       time.Now(),
	}

	if err := s.repo.SaveSession(session); err != nil {
		return nil, err
	}

	return session, nil
}

// FindActiveSession returns the active session for an invite without
// creating one; nil, nil when the invite has no active session. Used by
// read-only public endpoints so an unauthenticated page view never writes.
func (s *DefaultService) FindActiveSession(inviteID string) (*Session, error) {
	return s.repo.FindActiveSessionByInvite(inviteID)
}

// GetSession retrieves a session by ID.
func (s *DefaultService) GetSession(id string) (*Session, error) {
	return s.repo.FindSessionByID(id)
}

// ListSessions retrieves all sessions for an interview.
func (s *DefaultService) ListSessions(interviewID string) ([]*Session, error) {
	return s.repo.ListSessionsByInterview(interviewID)
}

// Page-size bounds for project-wide session listings.
const (
	DefaultProjectSessionLimit = 20
	MaxProjectSessionLimit     = 100
)

// ListProjectSessions retrieves the most recent sessions across all of a
// project's interviews, newest first. A non-positive limit falls back to
// DefaultProjectSessionLimit; limits above MaxProjectSessionLimit are capped.
func (s *DefaultService) ListProjectSessions(projectID string, limit int) ([]*Session, error) {
	if limit <= 0 {
		limit = DefaultProjectSessionLimit
	}
	if limit > MaxProjectSessionLimit {
		limit = MaxProjectSessionLimit
	}
	return s.repo.ListSessionsByProject(projectID, limit)
}

// AppendMessage adds a message to a session transcript.
func (s *DefaultService) AppendMessage(sessionID, role, content string) (*Message, error) {
	switch role {
	case RoleAssistant, RoleParticipant, RoleSystem:
	default:
		return nil, ErrInvalidRole
	}

	if _, err := s.repo.FindSessionByID(sessionID); err != nil {
		return nil, err
	}

	message := &Message{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		Role:      role,
		Content:   content,
		CreatedAt: time.Now(),
	}

	if err := s.repo.SaveMessage(message); err != nil {
		return nil, err
	}

	return message, nil
}

// GetTranscript retrieves the full message history for a session.
func (s *DefaultService) GetTranscript(sessionID string) ([]*Message, error) {
	return s.repo.ListMessagesBySession(sessionID)
}

// CompleteSession marks a session completed and stores its summary.
func (s *DefaultService) CompleteSession(sessionID, summary string) error {
	session, err := s.repo.FindSessionByID(sessionID)
	if err != nil {
		return err
	}

	now := time.Now()
	session.Status = SessionStatusCompleted
	session.Summary = summary
	session.EndedAt = &now

	return s.repo.UpdateSession(session)
}
