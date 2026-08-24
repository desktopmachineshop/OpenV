package postgres

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/lib/pq"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
)

// AgentRunRepository implements agentruns.Repository.
type AgentRunRepository struct {
	db *sql.DB
}

// NewAgentRunRepository creates a new run repository.
func NewAgentRunRepository(db *sql.DB) *AgentRunRepository {
	return &AgentRunRepository{db: db}
}

const runColumns = `r.id, COALESCE(r.org_id::text, ''), r.agent_id, r.project_id, r.automation_id, r.trigger_event_id, r.team_id, r.team_node_id, r.parent_run_id, r.work_item_id, r.interview_session_id, r.guided_session_id,
	r.status, r.cancel_requested, r.priority, r.prompt, r.run_token_hash, r.worker_id, r.heartbeat_at, r.started_at, r.finished_at, r.exit_code,
	r.final_text, r.error, r.tokens_in, r.tokens_out, r.cost_usd, r.artifacts_touched, r.launched_by, r.created_at, a.name, a.provider`

func scanRun(row interface{ Scan(...interface{}) error }) (*agentruns.Run, error) {
	r := new(agentruns.Run)
	var projectID, automationID, triggerEventID, teamID, teamNodeID, parentRunID, workItemID, interviewSessionID, guidedSessionID, launchedBy sql.NullString
	var heartbeatAt, startedAt, finishedAt sql.NullTime
	var exitCode sql.NullInt64
	var costUSD sql.NullFloat64
	var touched []byte

	err := row.Scan(&r.ID, &r.OrgID, &r.AgentID, &projectID, &automationID, &triggerEventID, &teamID, &teamNodeID, &parentRunID, &workItemID, &interviewSessionID, &guidedSessionID,
		&r.Status, &r.CancelRequested, &r.Priority, &r.Prompt, &r.RunTokenHash, &r.WorkerID, &heartbeatAt, &startedAt, &finishedAt, &exitCode,
		&r.FinalText, &r.Error, &r.TokensIn, &r.TokensOut, &costUSD, &touched, &launchedBy, &r.CreatedAt, &r.AgentName, &r.AgentProvider)
	if err != nil {
		return nil, err
	}

	setStr := func(v sql.NullString) *string {
		if v.Valid {
			s := v.String
			return &s
		}
		return nil
	}
	setTime := func(v sql.NullTime) *time.Time {
		if v.Valid {
			t := v.Time
			return &t
		}
		return nil
	}

	r.ProjectID = setStr(projectID)
	r.AutomationID = setStr(automationID)
	r.TriggerEventID = setStr(triggerEventID)
	r.TeamID = setStr(teamID)
	r.TeamNodeID = setStr(teamNodeID)
	r.ParentRunID = setStr(parentRunID)
	r.WorkItemID = setStr(workItemID)
	r.InterviewSessionID = setStr(interviewSessionID)
	r.GuidedSessionID = setStr(guidedSessionID)
	r.LaunchedBy = setStr(launchedBy)
	r.HeartbeatAt = setTime(heartbeatAt)
	r.StartedAt = setTime(startedAt)
	r.FinishedAt = setTime(finishedAt)
	if exitCode.Valid {
		v := int(exitCode.Int64)
		r.ExitCode = &v
	}
	if costUSD.Valid {
		v := costUSD.Float64
		r.CostUSD = &v
	}
	if err := json.Unmarshal(touched, &r.ArtifactsTouched); err != nil || r.ArtifactsTouched == nil {
		r.ArtifactsTouched = []map[string]interface{}{}
	}
	return r, nil
}

// Save inserts a run.
func (rep *AgentRunRepository) Save(r *agentruns.Run) error {
	touched, err := json.Marshal(r.ArtifactsTouched)
	if err != nil {
		return err
	}
	_, err = rep.db.Exec(`
		INSERT INTO agent_runs (id, org_id, agent_id, project_id, automation_id, trigger_event_id, team_id, team_node_id, parent_run_id, work_item_id, interview_session_id, guided_session_id,
			status, cancel_requested, priority, prompt, run_token_hash, worker_id, heartbeat_at, started_at, finished_at, exit_code,
			final_text, error, tokens_in, tokens_out, cost_usd, artifacts_touched, launched_by, created_at)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30)
	`, r.ID, r.OrgID, r.AgentID, r.ProjectID, r.AutomationID, r.TriggerEventID, r.TeamID, r.TeamNodeID, r.ParentRunID, r.WorkItemID, r.InterviewSessionID, r.GuidedSessionID,
		r.Status, r.CancelRequested, r.Priority, r.Prompt, r.RunTokenHash, r.WorkerID, r.HeartbeatAt, r.StartedAt, r.FinishedAt, r.ExitCode,
		r.FinalText, r.Error, r.TokensIn, r.TokensOut, r.CostUSD, touched, r.LaunchedBy, r.CreatedAt)
	if err != nil {
		return err
	}
	if r.PreferredUserID != nil {
		_, err = rep.db.Exec(`UPDATE agent_runs SET preferred_user_id = $2, hosted_after = $3 WHERE id = $1`,
			r.ID, r.PreferredUserID, r.HostedAfter)
	}
	return err
}

// Update rewrites a run's mutable fields.
func (rep *AgentRunRepository) Update(r *agentruns.Run) error {
	touched, err := json.Marshal(r.ArtifactsTouched)
	if err != nil {
		return err
	}
	_, err = rep.db.Exec(`
		UPDATE agent_runs SET status = $2, cancel_requested = $3, worker_id = $4, heartbeat_at = $5, started_at = $6, finished_at = $7,
			exit_code = $8, final_text = $9, error = $10, tokens_in = $11, tokens_out = $12, cost_usd = $13, artifacts_touched = $14
		WHERE id = $1
	`, r.ID, r.Status, r.CancelRequested, r.WorkerID, r.HeartbeatAt, r.StartedAt, r.FinishedAt,
		r.ExitCode, r.FinalText, r.Error, r.TokensIn, r.TokensOut, r.CostUSD, touched)
	return err
}

// FindByID returns a run, or nil.
func (rep *AgentRunRepository) FindByID(id string) (*agentruns.Run, error) {
	r, err := scanRun(rep.db.QueryRow(`SELECT `+runColumns+` FROM agent_runs r JOIN agents a ON a.id = r.agent_id WHERE r.id = $1`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

// FindByTokenHash returns the run holding the token, or nil.
func (rep *AgentRunRepository) FindByTokenHash(hash string) (*agentruns.Run, error) {
	r, err := scanRun(rep.db.QueryRow(`SELECT `+runColumns+` FROM agent_runs r JOIN agents a ON a.id = r.agent_id WHERE r.run_token_hash = $1`, hash))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

// List returns runs matching the filter, newest first.
func (rep *AgentRunRepository) List(filter agentruns.ListFilter) ([]*agentruns.Run, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := rep.db.Query(`
		SELECT `+runColumns+` FROM agent_runs r JOIN agents a ON a.id = r.agent_id
		WHERE ($1 = '' OR r.agent_id = $1::uuid)
		  AND ($2 = '' OR r.project_id = $2::uuid)
		  AND ($3 = '' OR r.status = $3)
		  AND ($4 = '' OR r.parent_run_id = $4::uuid)
		  AND ($5 = '' OR r.work_item_id = $5::uuid)
		ORDER BY r.created_at DESC
		LIMIT $6
	`, filter.AgentID, filter.ProjectID, filter.Status, filter.ParentID, filter.WorkItemID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRuns(rows)
}

// ListChildren returns direct child runs, oldest first.
func (rep *AgentRunRepository) ListChildren(parentRunID string) ([]*agentruns.Run, error) {
	rows, err := rep.db.Query(`
		SELECT `+runColumns+` FROM agent_runs r JOIN agents a ON a.id = r.agent_id
		WHERE r.parent_run_id = $1 ORDER BY r.created_at
	`, parentRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRuns(rows)
}

func collectRuns(rows *sql.Rows) ([]*agentruns.Run, error) {
	var result []*agentruns.Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// Claim atomically claims the best queued run in the worker's org.
// Personal runners (workerUserID != "") claim only runs launched by their
// user; workspace/hosted runners skip runs still reserved for a personal
// runner (until hosted_after) and, when excludeRepoAccess is set, runs
// whose agent needs repo access.
func (rep *AgentRunRepository) Claim(workerID string, orgID string, workerUserID string, providers []string, minPriority int, excludeRepoAccess bool) (*agentruns.Run, error) {
	row := rep.db.QueryRow(`
		UPDATE agent_runs SET status = 'claimed', worker_id = $1, heartbeat_at = NOW()
		WHERE id = (
			SELECT r.id FROM agent_runs r
			JOIN agents a ON a.id = r.agent_id
			WHERE r.status = 'queued'
			  AND r.org_id = $4::uuid
			  AND a.provider = ANY($2)
			  AND r.priority >= $3
			  AND (
			    -- Personal runners take their owner's runs plus ownerless
			    -- (system-launched) ones, so board/automation work load-shares
			    -- across every live runner. Runs another member launched stay
			    -- reserved for that member's runner or the workspace pool.
			    ($5 <> '' AND (r.launched_by = $5::uuid OR r.launched_by IS NULL))
			    OR
			    ($5 = '' AND (r.preferred_user_id IS NULL OR r.hosted_after <= NOW()))
			  )
			  AND (NOT $6 OR a.repo_access = FALSE)
			ORDER BY r.priority DESC, r.created_at
			LIMIT 1
			FOR UPDATE OF r SKIP LOCKED
		)
		RETURNING id
	`, workerID, pq.Array(providers), minPriority, orgID, workerUserID, excludeRepoAccess)

	var id string
	if err := row.Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return rep.FindByID(id)
}

// UpdateWorkItemID links a run to its kanban card.
func (rep *AgentRunRepository) UpdateWorkItemID(runID, workItemID string) error {
	_, err := rep.db.Exec(`UPDATE agent_runs SET work_item_id = $2 WHERE id = $1`, runID, workItemID)
	return err
}

// UpdateTokenHash rotates a run's token hash.
func (rep *AgentRunRepository) UpdateTokenHash(runID, hash string) error {
	_, err := rep.db.Exec(`UPDATE agent_runs SET run_token_hash = $2 WHERE id = $1`, runID, hash)
	return err
}

// Heartbeat refreshes liveness.
func (rep *AgentRunRepository) Heartbeat(runID string, at time.Time) error {
	_, err := rep.db.Exec(`UPDATE agent_runs SET heartbeat_at = $2 WHERE id = $1`, runID, at)
	return err
}

// FailStale fails claimed/running runs whose heartbeat predates cutoff.
func (rep *AgentRunRepository) FailStale(cutoff time.Time) ([]string, error) {
	rows, err := rep.db.Query(`
		UPDATE agent_runs SET status = 'failed', error = 'worker lost (heartbeat timeout)', finished_at = NOW()
		WHERE status IN ('claimed', 'running') AND heartbeat_at < $1
		RETURNING id
	`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// AppendLogs inserts a batch of log entries.
func (rep *AgentRunRepository) AppendLogs(runID string, entries []agentruns.LogEntry) error {
	tx, err := rep.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO agent_run_logs (run_id, seq, kind, payload, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (run_id, seq) DO NOTHING
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range entries {
		payload, err := json.Marshal(e.Payload)
		if err != nil {
			return err
		}
		createdAt := e.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now()
		}
		if _, err := stmt.Exec(runID, e.Seq, e.Kind, payload, createdAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListLogs returns log entries after a sequence number.
func (rep *AgentRunRepository) ListLogs(runID string, afterSeq int) ([]agentruns.LogEntry, error) {
	rows, err := rep.db.Query(`
		SELECT run_id, seq, kind, payload, created_at
		FROM agent_run_logs WHERE run_id = $1 AND seq > $2 ORDER BY seq
	`, runID, afterSeq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []agentruns.LogEntry
	for rows.Next() {
		var e agentruns.LogEntry
		var payload []byte
		if err := rows.Scan(&e.RunID, &e.Seq, &e.Kind, &payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &e.Payload); err != nil || e.Payload == nil {
			e.Payload = map[string]interface{}{}
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

// CountRunsSince counts an automation's runs created after since.
func (rep *AgentRunRepository) CountRunsSince(automationID string, since time.Time) (int, error) {
	var count int
	err := rep.db.QueryRow(`SELECT COUNT(*) FROM agent_runs WHERE automation_id = $1 AND created_at > $2`, automationID, since).Scan(&count)
	return count, err
}

// QueueStats summarizes an org's queued runs: total, age of the oldest, and
// how many need repo access (which hosted runners refuse).
func (rep *AgentRunRepository) QueueStats(orgID string) (agentruns.QueueStats, error) {
	var stats agentruns.QueueStats
	var oldest float64
	err := rep.db.QueryRow(`
		SELECT COUNT(*),
			COALESCE(EXTRACT(EPOCH FROM (NOW() - MIN(r.created_at))), 0),
			COUNT(*) FILTER (WHERE a.repo_access)
		FROM agent_runs r JOIN agents a ON a.id = r.agent_id
		WHERE r.status = 'queued' AND r.org_id = $1::uuid
	`, orgID).Scan(&stats.Queued, &oldest, &stats.QueuedRepoAccess)
	if err != nil {
		return agentruns.QueueStats{}, err
	}
	stats.OldestQueuedSeconds = int(oldest)
	return stats, nil
}

// CountPendingProposals counts a run's unreviewed proposals.
func (rep *AgentRunRepository) CountPendingProposals(runID string) (int, error) {
	var count int
	err := rep.db.QueryRow(`SELECT COUNT(*) FROM agent_proposals WHERE run_id = $1 AND status = 'pending'`, runID).Scan(&count)
	return count, err
}
