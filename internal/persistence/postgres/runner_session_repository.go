package postgres

import (
	"database/sql"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/runnersessions"
)

// RunnerSessionRepository implements runnersessions.Repository: the pool of
// pre-warmed nodes and the leases members hold over them.
type RunnerSessionRepository struct {
	db *sql.DB
}

// NewRunnerSessionRepository creates a transient runner repository.
func NewRunnerSessionRepository(db *sql.DB) *RunnerSessionRepository {
	return &RunnerSessionRepository{db: db}
}

const nodeColumns = `id, pool, name, providers, status, session_id, last_seen_at, created_at`

func scanNode(row interface{ Scan(...interface{}) error }) (*runnersessions.Node, error) {
	n := new(runnersessions.Node)
	var providers string
	var sessionID sql.NullString
	if err := row.Scan(&n.ID, &n.Pool, &n.Name, &providers, &n.Status, &sessionID, &n.LastSeenAt, &n.CreatedAt); err != nil {
		return nil, err
	}
	if providers != "" {
		n.Providers = strings.Split(providers, ",")
	}
	if sessionID.Valid {
		v := sessionID.String
		n.SessionID = &v
	}
	return n, nil
}

// SaveNode upserts a node registration by id.
func (r *RunnerSessionRepository) SaveNode(n *runnersessions.Node) error {
	_, err := r.db.Exec(`
		INSERT INTO runner_pool_nodes (id, pool, name, providers, status, session_id, last_seen_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE SET
			providers = EXCLUDED.providers,
			status = EXCLUDED.status,
			session_id = EXCLUDED.session_id,
			last_seen_at = EXCLUDED.last_seen_at
	`, n.ID, n.Pool, n.Name, strings.Join(n.Providers, ","), n.Status, n.SessionID, n.LastSeenAt, n.CreatedAt)
	return err
}

// FindNodeByID returns a node, or nil.
func (r *RunnerSessionRepository) FindNodeByID(id string) (*runnersessions.Node, error) {
	n, err := scanNode(r.db.QueryRow(`SELECT `+nodeColumns+` FROM runner_pool_nodes WHERE id = $1`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return n, err
}

// FindNodeByName returns the pool's node with that name, or nil.
func (r *RunnerSessionRepository) FindNodeByName(pool, name string) (*runnersessions.Node, error) {
	n, err := scanNode(r.db.QueryRow(`SELECT `+nodeColumns+` FROM runner_pool_nodes WHERE pool = $1 AND name = $2`, pool, name))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return n, err
}

// TouchNode records a heartbeat and returns the node's current row, so a
// lease made since the last beat is visible to the caller in one round trip.
func (r *RunnerSessionRepository) TouchNode(id string, at time.Time) (*runnersessions.Node, error) {
	n, err := scanNode(r.db.QueryRow(`
		UPDATE runner_pool_nodes
		SET last_seen_at = $2,
		    -- A beat from a node marked offline brings it straight back into
		    -- the pool, unless it is mid-lease.
		    status = CASE WHEN status = 'offline' AND session_id IS NULL THEN 'idle' ELSE status END
		WHERE id = $1
		RETURNING `+nodeColumns, id, at))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return n, err
}

// SetNodeStatus moves a node between pool states.
func (r *RunnerSessionRepository) SetNodeStatus(id, status string, sessionID *string) error {
	_, err := r.db.Exec(`UPDATE runner_pool_nodes SET status = $2, session_id = $3 WHERE id = $1`, id, status, sessionID)
	return err
}

// ListNodes returns a pool's nodes, oldest first.
func (r *RunnerSessionRepository) ListNodes(pool string) ([]*runnersessions.Node, error) {
	rows, err := r.db.Query(`SELECT `+nodeColumns+` FROM runner_pool_nodes WHERE pool = $1 ORDER BY created_at`, pool)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*runnersessions.Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, rows.Err()
}

// MarkStaleNodesOffline flags nodes that stopped heartbeating and returns
// them (with the lease each was holding, so the caller can end it).
func (r *RunnerSessionRepository) MarkStaleNodesOffline(before time.Time) ([]*runnersessions.Node, error) {
	rows, err := r.db.Query(`
		UPDATE runner_pool_nodes SET status = 'offline'
		WHERE last_seen_at < $1 AND status <> 'offline'
		RETURNING `+nodeColumns, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*runnersessions.Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, rows.Err()
}

// LeaseIdleNode atomically reserves one idle, recently-seen node. The
// subquery + FOR UPDATE SKIP LOCKED is what makes two members clicking at the
// same moment take two different nodes instead of the same one.
func (r *RunnerSessionRepository) LeaseIdleNode(pool, sessionID string, seenSince time.Time) (*runnersessions.Node, error) {
	n, err := scanNode(r.db.QueryRow(`
		UPDATE runner_pool_nodes SET status = 'leased', session_id = $2
		WHERE id = (
			SELECT id FROM runner_pool_nodes
			WHERE pool = $1 AND status = 'idle' AND session_id IS NULL AND last_seen_at > $3
			ORDER BY last_seen_at DESC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING `+nodeColumns, pool, sessionID, seenSince))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return n, err
}

const sessionColumns = `s.id, s.org_id, s.user_id, COALESCE(s.node_id::text, ''), s.worker_key_id, s.status,
	s.started_at, s.expires_at, s.last_activity_at, s.ended_at, s.end_reason, s.idle_minutes`

func scanSession(row interface{ Scan(...interface{}) error }, extra ...interface{}) (*runnersessions.Session, error) {
	s := new(runnersessions.Session)
	var workerKeyID sql.NullString
	var endedAt sql.NullTime
	dest := []interface{}{&s.ID, &s.OrgID, &s.UserID, &s.NodeID, &workerKeyID, &s.Status,
		&s.StartedAt, &s.ExpiresAt, &s.LastActivityAt, &endedAt, &s.EndReason, &s.IdleMinutes}
	dest = append(dest, extra...)
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	if workerKeyID.Valid {
		v := workerKeyID.String
		s.WorkerKeyID = &v
	}
	if endedAt.Valid {
		t := endedAt.Time
		s.EndedAt = &t
	}
	return s, nil
}

// SaveSession inserts a lease.
func (r *RunnerSessionRepository) SaveSession(s *runnersessions.Session) error {
	_, err := r.db.Exec(`
		INSERT INTO runner_sessions (id, org_id, user_id, node_id, worker_key_id, status,
			started_at, expires_at, last_activity_at, ended_at, end_reason, idle_minutes)
		VALUES ($1, $2, $3, NULLIF($4, '')::uuid, $5, $6, $7, $8, $9, $10, $11, $12)
	`, s.ID, s.OrgID, s.UserID, s.NodeID, s.WorkerKeyID, s.Status,
		s.StartedAt, s.ExpiresAt, s.LastActivityAt, s.EndedAt, s.EndReason, s.IdleMinutes)
	return err
}

// UpdateSession rewrites a lease's mutable fields.
func (r *RunnerSessionRepository) UpdateSession(s *runnersessions.Session) error {
	_, err := r.db.Exec(`
		UPDATE runner_sessions SET worker_key_id = $2, status = $3, expires_at = $4,
			last_activity_at = $5, ended_at = $6, end_reason = $7
		WHERE id = $1
	`, s.ID, s.WorkerKeyID, s.Status, s.ExpiresAt, s.LastActivityAt, s.EndedAt, s.EndReason)
	return err
}

// nodeNameJoin decorates a session with its node's name for display.
const nodeNameJoin = `LEFT JOIN runner_pool_nodes n ON n.id = s.node_id`

// FindSessionByID returns a lease, or nil.
func (r *RunnerSessionRepository) FindSessionByID(id string) (*runnersessions.Session, error) {
	var nodeName string
	s, err := scanSession(r.db.QueryRow(`
		SELECT `+sessionColumns+`, COALESCE(n.name, '') FROM runner_sessions s `+nodeNameJoin+`
		WHERE s.id = $1
	`, id), &nodeName)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.NodeName = nodeName
	return s, nil
}

// FindLiveSessionForUser returns the member's current lease, or nil.
func (r *RunnerSessionRepository) FindLiveSessionForUser(orgID, userID string) (*runnersessions.Session, error) {
	var nodeName string
	s, err := scanSession(r.db.QueryRow(`
		SELECT `+sessionColumns+`, COALESCE(n.name, '') FROM runner_sessions s `+nodeNameJoin+`
		WHERE s.org_id = $1 AND s.user_id = $2 AND s.status IN ('starting', 'active')
		ORDER BY s.started_at DESC LIMIT 1
	`, orgID, userID), &nodeName)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.NodeName = nodeName
	return s, nil
}

// TouchSession bumps activity on a live lease.
func (r *RunnerSessionRepository) TouchSession(sessionID string, at time.Time) error {
	_, err := r.db.Exec(`
		UPDATE runner_sessions SET last_activity_at = $2
		WHERE id = $1 AND status IN ('starting', 'active')
	`, sessionID, at)
	return err
}

// ListLapsedSessions returns live leases that are past their hard expiry,
// past their idle window, or stuck waiting for a node that never arrived.
func (r *RunnerSessionRepository) ListLapsedSessions(now time.Time) ([]*runnersessions.Session, error) {
	rows, err := r.db.Query(`
		SELECT `+sessionColumns+`, COALESCE(n.name, '') FROM runner_sessions s `+nodeNameJoin+`
		WHERE s.status IN ('starting', 'active')
		  AND (
			s.expires_at <= $1
			OR s.last_activity_at + (s.idle_minutes * INTERVAL '1 minute') <= $1
			OR (s.status = 'starting' AND s.started_at <= $2)
		  )
		ORDER BY s.started_at
	`, now, now.Add(-runnersessions.StartTimeout))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return collectSessions(rows)
}

// ListLiveSessions returns a workspace's current leases.
func (r *RunnerSessionRepository) ListLiveSessions(orgID string) ([]*runnersessions.Session, error) {
	rows, err := r.db.Query(`
		SELECT `+sessionColumns+`, COALESCE(n.name, '') FROM runner_sessions s `+nodeNameJoin+`
		WHERE s.org_id = $1 AND s.status IN ('starting', 'active')
		ORDER BY s.started_at
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return collectSessions(rows)
}

func collectSessions(rows *sql.Rows) ([]*runnersessions.Session, error) {
	var result []*runnersessions.Session
	for rows.Next() {
		var nodeName string
		s, err := scanSession(rows, &nodeName)
		if err != nil {
			return nil, err
		}
		s.NodeName = nodeName
		result = append(result, s)
	}
	return result, rows.Err()
}
