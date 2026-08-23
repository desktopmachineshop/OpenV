package postgres

import (
	"database/sql"

	"github.com/openv/requirements-platform/internal/domain/interviews"
)

// InterviewRepository implements the interviews.Repository interface
type InterviewRepository struct {
	db *sql.DB
}

// NewInterviewRepository creates a new interview repository
func NewInterviewRepository(db *sql.DB) *InterviewRepository {
	return &InterviewRepository{db: db}
}

// SaveInterview inserts a new interview
func (r *InterviewRepository) SaveInterview(i *interviews.Interview) error {
	query := `
		INSERT INTO interviews (id, project_id, guided_session_id, persona_artifact_id, name, brief, agent_id, status, expires_at, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`

	_, err := r.db.Exec(
		query,
		i.ID,
		i.ProjectID,
		i.GuidedSessionID,
		i.PersonaArtifactID,
		i.Name,
		i.Brief,
		i.AgentID,
		i.Status,
		i.ExpiresAt,
		i.CreatedBy,
		i.CreatedAt,
		i.UpdatedAt,
	)

	return err
}

// UpdateInterview updates an existing interview
func (r *InterviewRepository) UpdateInterview(i *interviews.Interview) error {
	query := `
		UPDATE interviews
		SET guided_session_id = $2, persona_artifact_id = $3, name = $4, brief = $5, agent_id = $6, status = $7, expires_at = $8, updated_at = $9
		WHERE id = $1
	`

	_, err := r.db.Exec(
		query,
		i.ID,
		i.GuidedSessionID,
		i.PersonaArtifactID,
		i.Name,
		i.Brief,
		i.AgentID,
		i.Status,
		i.ExpiresAt,
		i.UpdatedAt,
	)

	return err
}

// scanInterview scans one interview row
func scanInterview(scan func(dest ...interface{}) error) (*interviews.Interview, error) {
	interview := &interviews.Interview{}
	err := scan(
		&interview.ID,
		&interview.ProjectID,
		&interview.GuidedSessionID,
		&interview.PersonaArtifactID,
		&interview.Name,
		&interview.Brief,
		&interview.AgentID,
		&interview.Status,
		&interview.ExpiresAt,
		&interview.CreatedBy,
		&interview.CreatedAt,
		&interview.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return interview, nil
}

// FindInterviewByID retrieves an interview by ID
func (r *InterviewRepository) FindInterviewByID(id string) (*interviews.Interview, error) {
	query := `
		SELECT id, project_id, guided_session_id, persona_artifact_id, name, brief, agent_id, status, expires_at, created_by, created_at, updated_at
		FROM interviews
		WHERE id = $1
	`

	row := r.db.QueryRow(query, id)
	interview, err := scanInterview(row.Scan)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, interviews.ErrInterviewNotFound
		}
		return nil, err
	}

	return interview, nil
}

// ListInterviewsByProject retrieves all interviews for a project, newest first
func (r *InterviewRepository) ListInterviewsByProject(projectID string) ([]*interviews.Interview, error) {
	query := `
		SELECT id, project_id, guided_session_id, persona_artifact_id, name, brief, agent_id, status, expires_at, created_by, created_at, updated_at
		FROM interviews
		WHERE project_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var interviewList []*interviews.Interview
	for rows.Next() {
		interview, err := scanInterview(rows.Scan)
		if err != nil {
			return nil, err
		}
		interviewList = append(interviewList, interview)
	}

	return interviewList, rows.Err()
}

// SaveInvite inserts a new interview invite
func (r *InterviewRepository) SaveInvite(inv *interviews.Invite) error {
	query := `
		INSERT INTO interview_invites (id, interview_id, token_hash, invitee_label, expires_at, revoked, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err := r.db.Exec(
		query,
		inv.ID,
		inv.InterviewID,
		inv.TokenHash,
		inv.InviteeLabel,
		inv.ExpiresAt,
		inv.Revoked,
		inv.CreatedAt,
	)

	return err
}

// scanInvite scans one invite row
func scanInvite(scan func(dest ...interface{}) error) (*interviews.Invite, error) {
	invite := &interviews.Invite{}
	err := scan(
		&invite.ID,
		&invite.InterviewID,
		&invite.TokenHash,
		&invite.InviteeLabel,
		&invite.ExpiresAt,
		&invite.Revoked,
		&invite.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return invite, nil
}

// FindInviteByTokenHash retrieves an invite by its token hash; nil, nil when unknown
// FindInviteByID retrieves an invite by ID (nil, nil when none)
func (r *InterviewRepository) FindInviteByID(id string) (*interviews.Invite, error) {
	query := `
		SELECT id, interview_id, token_hash, invitee_label, expires_at, revoked, created_at
		FROM interview_invites
		WHERE id = $1
	`

	row := r.db.QueryRow(query, id)
	invite, err := scanInvite(row.Scan)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return invite, nil
}

func (r *InterviewRepository) FindInviteByTokenHash(tokenHash string) (*interviews.Invite, error) {
	query := `
		SELECT id, interview_id, token_hash, invitee_label, expires_at, revoked, created_at
		FROM interview_invites
		WHERE token_hash = $1
	`

	row := r.db.QueryRow(query, tokenHash)
	invite, err := scanInvite(row.Scan)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return invite, nil
}

// ListInvitesByInterview retrieves all invites for an interview, newest first
func (r *InterviewRepository) ListInvitesByInterview(interviewID string) ([]*interviews.Invite, error) {
	query := `
		SELECT id, interview_id, token_hash, invitee_label, expires_at, revoked, created_at
		FROM interview_invites
		WHERE interview_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(query, interviewID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invites []*interviews.Invite
	for rows.Next() {
		invite, err := scanInvite(rows.Scan)
		if err != nil {
			return nil, err
		}
		invites = append(invites, invite)
	}

	return invites, rows.Err()
}

// RevokeInvite marks an invite revoked
func (r *InterviewRepository) RevokeInvite(id string) error {
	query := `UPDATE interview_invites SET revoked = TRUE WHERE id = $1`
	_, err := r.db.Exec(query, id)
	return err
}

// SaveSession inserts a new interview session
func (r *InterviewRepository) SaveSession(s *interviews.Session) error {
	query := `
		INSERT INTO interview_sessions (id, interview_id, invite_id, participant_name, status, summary, started_at, ended_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, err := r.db.Exec(
		query,
		s.ID,
		s.InterviewID,
		s.InviteID,
		s.ParticipantName,
		s.Status,
		s.Summary,
		s.StartedAt,
		s.EndedAt,
	)

	return err
}

// UpdateSession updates an existing interview session
func (r *InterviewRepository) UpdateSession(s *interviews.Session) error {
	query := `
		UPDATE interview_sessions
		SET participant_name = $2, status = $3, summary = $4, ended_at = $5
		WHERE id = $1
	`

	_, err := r.db.Exec(
		query,
		s.ID,
		s.ParticipantName,
		s.Status,
		s.Summary,
		s.EndedAt,
	)

	return err
}

// scanInterviewSession scans one interview session row
func scanInterviewSession(scan func(dest ...interface{}) error) (*interviews.Session, error) {
	session := &interviews.Session{}
	err := scan(
		&session.ID,
		&session.InterviewID,
		&session.InviteID,
		&session.ParticipantName,
		&session.Status,
		&session.Summary,
		&session.StartedAt,
		&session.EndedAt,
	)
	if err != nil {
		return nil, err
	}
	return session, nil
}

// FindSessionByID retrieves an interview session by ID
func (r *InterviewRepository) FindSessionByID(id string) (*interviews.Session, error) {
	query := `
		SELECT id, interview_id, invite_id, participant_name, status, summary, started_at, ended_at
		FROM interview_sessions
		WHERE id = $1
	`

	row := r.db.QueryRow(query, id)
	session, err := scanInterviewSession(row.Scan)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, interviews.ErrSessionNotFound
		}
		return nil, err
	}

	return session, nil
}

// FindActiveSessionByInvite retrieves the active session for an invite; nil, nil when none
func (r *InterviewRepository) FindActiveSessionByInvite(inviteID string) (*interviews.Session, error) {
	query := `
		SELECT id, interview_id, invite_id, participant_name, status, summary, started_at, ended_at
		FROM interview_sessions
		WHERE invite_id = $1 AND status = 'active'
		ORDER BY started_at DESC
		LIMIT 1
	`

	row := r.db.QueryRow(query, inviteID)
	session, err := scanInterviewSession(row.Scan)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return session, nil
}

// ListSessionsByInterview retrieves all sessions for an interview, newest first
func (r *InterviewRepository) ListSessionsByInterview(interviewID string) ([]*interviews.Session, error) {
	query := `
		SELECT id, interview_id, invite_id, participant_name, status, summary, started_at, ended_at
		FROM interview_sessions
		WHERE interview_id = $1
		ORDER BY started_at DESC
	`

	rows, err := r.db.Query(query, interviewID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*interviews.Session
	for rows.Next() {
		session, err := scanInterviewSession(rows.Scan)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}

	return sessions, rows.Err()
}

// SaveMessage inserts a new interview message
func (r *InterviewRepository) SaveMessage(m *interviews.Message) error {
	query := `
		INSERT INTO interview_messages (id, session_id, role, content, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, err := r.db.Exec(
		query,
		m.ID,
		m.SessionID,
		m.Role,
		m.Content,
		m.CreatedAt,
	)

	return err
}

// ListMessagesBySession retrieves the transcript for a session, oldest first
func (r *InterviewRepository) ListMessagesBySession(sessionID string) ([]*interviews.Message, error) {
	query := `
		SELECT id, session_id, role, content, created_at
		FROM interview_messages
		WHERE session_id = $1
		ORDER BY created_at ASC
	`

	rows, err := r.db.Query(query, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*interviews.Message
	for rows.Next() {
		message := new(interviews.Message)
		err := rows.Scan(
			&message.ID,
			&message.SessionID,
			&message.Role,
			&message.Content,
			&message.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}

	return messages, rows.Err()
}
