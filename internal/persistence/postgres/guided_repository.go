package postgres

import (
	"database/sql"
	"encoding/json"

	"github.com/openv/requirements-platform/internal/domain/guided"
)

// GuidedRepository implements the guided.Repository interface
type GuidedRepository struct {
	db *sql.DB
}

// NewGuidedRepository creates a new guided session repository
func NewGuidedRepository(db *sql.DB) *GuidedRepository {
	return &GuidedRepository{db: db}
}

// Save inserts a new guided session
func (r *GuidedRepository) Save(s *guided.Session) error {
	answersJSON, draftIDsJSON, err := marshalSessionJSON(s)
	if err != nil {
		return err
	}

	// org_id derives from the owning project so sessions stay tenant-scoped
	// even after the org_id column is promoted NOT NULL.
	query := `
		INSERT INTO guided_sessions (id, org_id, project_id, status, current_step, answers, draft_artifact_ids, agent_run_id, created_by, created_at, updated_at)
		VALUES ($1, (SELECT org_id FROM projects WHERE id = $2::uuid), $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	_, err = r.db.Exec(
		query,
		s.ID,
		s.ProjectID,
		s.Status,
		s.CurrentStep,
		answersJSON,
		draftIDsJSON,
		s.AgentRunID,
		s.CreatedBy,
		s.CreatedAt,
		s.UpdatedAt,
	)

	return err
}

// Update updates an existing guided session
func (r *GuidedRepository) Update(s *guided.Session) error {
	answersJSON, draftIDsJSON, err := marshalSessionJSON(s)
	if err != nil {
		return err
	}

	query := `
		UPDATE guided_sessions
		SET status = $2, current_step = $3, answers = $4, draft_artifact_ids = $5, agent_run_id = $6, updated_at = $7
		WHERE id = $1
	`

	_, err = r.db.Exec(
		query,
		s.ID,
		s.Status,
		s.CurrentStep,
		answersJSON,
		draftIDsJSON,
		s.AgentRunID,
		s.UpdatedAt,
	)

	return err
}

// SetPendingNudge parks the newest wizard nudge on the session (nil clears
// it). A single-column write: it races the session's own updates on purpose,
// since a nudge arrives from a different action than a step save.
func (r *GuidedRepository) SetPendingNudge(sessionID string, nudge *guided.PendingNudge) error {
	if nudge == nil {
		_, err := r.db.Exec(`UPDATE guided_sessions SET pending_nudge = NULL WHERE id = $1`, sessionID)
		return err
	}
	payload, err := json.Marshal(nudge)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`UPDATE guided_sessions SET pending_nudge = $2 WHERE id = $1`, sessionID, payload)
	return err
}

// TakePendingNudge returns the waiting nudge and clears it in one statement,
// so two finishing runs cannot both launch the same nudge.
func (r *GuidedRepository) TakePendingNudge(sessionID string) (*guided.PendingNudge, error) {
	// RETURNING reports the NEW row, which is the cleared one, so the old
	// value is read through a self-join that snapshots the row as it was.
	var payload []byte
	err := r.db.QueryRow(`
		UPDATE guided_sessions g SET pending_nudge = NULL
		FROM guided_sessions old
		WHERE g.id = $1 AND old.id = g.id AND g.pending_nudge IS NOT NULL
		RETURNING old.pending_nudge
	`, sessionID).Scan(&payload)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	nudge := &guided.PendingNudge{}
	if err := json.Unmarshal(payload, nudge); err != nil {
		return nil, nil
	}
	return nudge, nil
}

// marshalSessionJSON marshals the JSONB fields of a session with non-nil defaults
func marshalSessionJSON(s *guided.Session) ([]byte, []byte, error) {
	answers := s.Answers
	if answers == nil {
		answers = map[string]interface{}{}
	}
	answersJSON, err := json.Marshal(answers)
	if err != nil {
		return nil, nil, err
	}

	draftIDs := s.DraftArtifactIDs
	if draftIDs == nil {
		draftIDs = []string{}
	}
	draftIDsJSON, err := json.Marshal(draftIDs)
	if err != nil {
		return nil, nil, err
	}

	return answersJSON, draftIDsJSON, nil
}

// scanGuidedSession scans one guided session row
func scanGuidedSession(scan func(dest ...interface{}) error) (*guided.Session, error) {
	session := &guided.Session{}
	var projectID sql.NullString
	var answersJSON, draftIDsJSON, pendingNudgeJSON []byte

	err := scan(
		&session.ID,
		&projectID,
		&session.Status,
		&session.CurrentStep,
		&answersJSON,
		&draftIDsJSON,
		&session.AgentRunID,
		&pendingNudgeJSON,
		&session.CreatedBy,
		&session.CreatedAt,
		&session.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if projectID.Valid {
		session.ProjectID = projectID.String
	}

	session.Answers = map[string]interface{}{}
	if len(answersJSON) > 0 {
		if err := json.Unmarshal(answersJSON, &session.Answers); err != nil {
			return nil, err
		}
	}

	session.DraftArtifactIDs = []string{}
	if len(draftIDsJSON) > 0 {
		if err := json.Unmarshal(draftIDsJSON, &session.DraftArtifactIDs); err != nil {
			return nil, err
		}
	}

	// A NULL (or unreadable) pending nudge means nothing is waiting — never a
	// reason to fail the read of an otherwise good session.
	if len(pendingNudgeJSON) > 0 {
		nudge := &guided.PendingNudge{}
		if err := json.Unmarshal(pendingNudgeJSON, nudge); err == nil {
			session.PendingNudge = nudge
		}
	}

	return session, nil
}

// FindByID retrieves a guided session by ID
func (r *GuidedRepository) FindByID(id string) (*guided.Session, error) {
	query := `
		SELECT id, project_id, status, current_step, answers, draft_artifact_ids, agent_run_id, pending_nudge, created_by, created_at, updated_at
		FROM guided_sessions
		WHERE id = $1
	`

	row := r.db.QueryRow(query, id)
	session, err := scanGuidedSession(row.Scan)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, guided.ErrSessionNotFound
		}
		return nil, err
	}

	return session, nil
}

// SaveChatMessage inserts one copilot chat message
func (r *GuidedRepository) SaveChatMessage(m *guided.ChatMessage) error {
	_, err := r.db.Exec(`
		INSERT INTO guided_session_messages (id, session_id, role, content, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, m.ID, m.SessionID, m.Role, m.Content, m.CreatedAt)
	return err
}

// ListChatMessages retrieves a session's copilot messages, oldest first
func (r *GuidedRepository) ListChatMessages(sessionID string) ([]*guided.ChatMessage, error) {
	rows, err := r.db.Query(`
		SELECT id, session_id, role, content, created_at
		FROM guided_session_messages
		WHERE session_id = $1
		ORDER BY created_at
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := []*guided.ChatMessage{}
	for rows.Next() {
		m := &guided.ChatMessage{}
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}

	return messages, rows.Err()
}

// ListByProject retrieves all guided sessions for a project, newest first
func (r *GuidedRepository) ListByProject(projectID string) ([]*guided.Session, error) {
	query := `
		SELECT id, project_id, status, current_step, answers, draft_artifact_ids, agent_run_id, pending_nudge, created_by, created_at, updated_at
		FROM guided_sessions
		WHERE project_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*guided.Session
	for rows.Next() {
		session, err := scanGuidedSession(rows.Scan)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}

	return sessions, rows.Err()
}
