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

const runColumns = `r.id, COALESCE(r.org_id::text, ''), r.agent_id, r.project_id, r.automation_id, r.trigger_event_id, r.team_id, r.team_node_id, r.parent_run_id, r.work_item_id, r.interview_session_id, r.guided_session_id, r.retried_from_run_id,
	r.status, r.cancel_requested, r.priority, r.prompt, r.run_token_hash, r.worker_id, r.heartbeat_at, r.started_at, r.finished_at, r.exit_code,
	r.final_text, r.error, r.error_class, r.attempt_count, r.max_attempts, r.next_attempt_at, r.tokens_in, r.tokens_out, r.cost_usd, r.artifacts_touched, r.launched_by, r.created_at, r.preferred_user_id, r.hosted_after, a.name, a.provider`

func scanRun(row interface{ Scan(...interface{}) error }) (*agentruns.Run, error) {
	r := new(agentruns.Run)
	var projectID, automationID, triggerEventID, teamID, teamNodeID, parentRunID, workItemID, interviewSessionID, guidedSessionID, retriedFromRunID, launchedBy, preferredUserID sql.NullString
	var heartbeatAt, startedAt, finishedAt, hostedAfter, nextAttemptAt sql.NullTime
	var exitCode sql.NullInt64
	var costUSD sql.NullFloat64
	var touched []byte

	err := row.Scan(&r.ID, &r.OrgID, &r.AgentID, &projectID, &automationID, &triggerEventID, &teamID, &teamNodeID, &parentRunID, &workItemID, &interviewSessionID, &guidedSessionID, &retriedFromRunID,
		&r.Status, &r.CancelRequested, &r.Priority, &r.Prompt, &r.RunTokenHash, &r.WorkerID, &heartbeatAt, &startedAt, &finishedAt, &exitCode,
		&r.FinalText, &r.Error, &r.ErrorClass, &r.AttemptCount, &r.MaxAttempts, &nextAttemptAt, &r.TokensIn, &r.TokensOut, &costUSD, &touched, &launchedBy, &r.CreatedAt, &preferredUserID, &hostedAfter, &r.AgentName, &r.AgentProvider)
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
	r.RetriedFromRunID = setStr(retriedFromRunID)
	r.LaunchedBy = setStr(launchedBy)
	r.PreferredUserID = setStr(preferredUserID)
	r.HostedAfter = setTime(hostedAfter)
	r.NextAttemptAt = setTime(nextAttemptAt)
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
	// created_at is a naive TIMESTAMP column: Postgres stores whatever
	// wall-clock the driver sends and discards the offset. Normalize to UTC so
	// the stored wall-clock is the UTC instant regardless of the server
	// process's local timezone; the Usage day-bucketing (and every reader,
	// which labels naive timestamps UTC) then reflects true UTC days.
	createdAt := r.CreatedAt.UTC()
	_, err = rep.db.Exec(`
		INSERT INTO agent_runs (id, org_id, agent_id, project_id, automation_id, trigger_event_id, team_id, team_node_id, parent_run_id, work_item_id, interview_session_id, guided_session_id, retried_from_run_id,
			status, cancel_requested, priority, prompt, run_token_hash, worker_id, heartbeat_at, started_at, finished_at, exit_code,
			final_text, error, error_class, attempt_count, max_attempts, next_attempt_at, tokens_in, tokens_out, cost_usd, artifacts_touched, launched_by, created_at, preferred_user_id, hosted_after)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37)
	`, r.ID, r.OrgID, r.AgentID, r.ProjectID, r.AutomationID, r.TriggerEventID, r.TeamID, r.TeamNodeID, r.ParentRunID, r.WorkItemID, r.InterviewSessionID, r.GuidedSessionID, r.RetriedFromRunID,
		r.Status, r.CancelRequested, r.Priority, r.Prompt, r.RunTokenHash, r.WorkerID, r.HeartbeatAt, r.StartedAt, r.FinishedAt, r.ExitCode,
		r.FinalText, r.Error, r.ErrorClass, r.AttemptCount, r.MaxAttempts, r.NextAttemptAt, r.TokensIn, r.TokensOut, r.CostUSD, touched, r.LaunchedBy, createdAt, r.PreferredUserID, r.HostedAfter)
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

// FindByTokenHash returns the run holding the token, or nil. Runs that have
// reached a terminal state (or handed their result to proposal review) no
// longer authenticate: their token is revoked at finish time, and this lookup
// refuses them regardless as defense in depth.
func (rep *AgentRunRepository) FindByTokenHash(hash string) (*agentruns.Run, error) {
	r, err := scanRun(rep.db.QueryRow(`
		SELECT `+runColumns+` FROM agent_runs r JOIN agents a ON a.id = r.agent_id
		WHERE r.run_token_hash = $1 AND r.run_token_hash <> ''
		  AND r.status NOT IN ('succeeded', 'failed', 'cancelled', 'timed_out', 'awaiting_approval')
	`, hash))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

// List returns runs matching the filter, newest first. The org filter is
// mandatory and fails closed (like EventRepository.List): an empty OrgID
// matches no rows, so a caller that could not resolve an active workspace can
// never page across tenants. Every caller must supply the run's org — the
// board trigger derives it from the work item's project.
func (rep *AgentRunRepository) List(filter agentruns.ListFilter) ([]*agentruns.Run, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := rep.db.Query(`
		SELECT `+runColumns+` FROM agent_runs r JOIN agents a ON a.id = r.agent_id
		WHERE r.org_id = NULLIF($6, '')::uuid
		  AND ($1 = '' OR r.agent_id = $1::uuid)
		  AND ($2 = '' OR r.project_id = $2::uuid)
		  AND ($3 = '' OR r.status = $3)
		  AND ($4 = '' OR r.parent_run_id = $4::uuid)
		  AND ($5 = '' OR r.work_item_id = $5::uuid)
		  AND ($7 = '' OR r.launched_by = $7::uuid)
		ORDER BY r.created_at DESC
		LIMIT $8
	`, filter.AgentID, filter.ProjectID, filter.Status, filter.ParentID, filter.WorkItemID, filter.OrgID, filter.LaunchedBy, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRuns(rows)
}

// listChildrenLimit bounds a single node's direct children. Run trees are
// normally small (retries are siblings, not children), so this only caps a
// pathological fan-out; the Tree walk would otherwise pull an unbounded set
// per node.
const listChildrenLimit = 1000

// ListChildren returns direct child runs, oldest first (bounded).
func (rep *AgentRunRepository) ListChildren(parentRunID string) ([]*agentruns.Run, error) {
	rows, err := rep.db.Query(`
		SELECT `+runColumns+` FROM agent_runs r JOIN agents a ON a.id = r.agent_id
		WHERE r.parent_run_id = $1 ORDER BY r.created_at
		LIMIT $2
	`, parentRunID, listChildrenLimit)
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
			  -- Retry backoff: a re-enqueued attempt is not claimable until its
			  -- next_attempt_at elapses (NULL for runs with no backoff).
			  AND (r.next_attempt_at IS NULL OR r.next_attempt_at <= NOW())
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

// ReleaseClaim conditionally returns a run to the queue, but only while it is
// still owned by workerID and non-terminal (claimed or running); reports
// whether it was applied. Running is included so a worker shutting down
// mid-run can hand the run back instead of failing it.
func (rep *AgentRunRepository) ReleaseClaim(runID, workerID string) (bool, error) {
	res, err := rep.db.Exec(`
		UPDATE agent_runs SET status = 'queued', worker_id = '', heartbeat_at = NULL, started_at = NULL
		WHERE id = $1 AND status IN ('claimed', 'running') AND worker_id = $2
	`, runID, workerID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// CancelQueued conditionally cancels a run only while it is still queued,
// revoking its token; reports whether it was applied.
func (rep *AgentRunRepository) CancelQueued(id string) (bool, error) {
	res, err := rep.db.Exec(`
		UPDATE agent_runs SET status = 'cancelled', cancel_requested = TRUE, finished_at = NOW(), run_token_hash = ''
		WHERE id = $1 AND status = 'queued'
	`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// SetCancelRequested flags a claimed/running run for cooperative
// cancellation; reports whether the flag was applied.
func (rep *AgentRunRepository) SetCancelRequested(id string) (bool, error) {
	res, err := rep.db.Exec(`
		UPDATE agent_runs SET cancel_requested = TRUE
		WHERE id = $1 AND status IN ('claimed', 'running')
	`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// UpdateTerminal writes a run's terminal result fields and revokes its run
// token, but only while the stored status is still non-terminal; reports
// whether the transition was applied.
func (rep *AgentRunRepository) UpdateTerminal(r *agentruns.Run) (bool, error) {
	touched, err := json.Marshal(r.ArtifactsTouched)
	if err != nil {
		return false, err
	}
	res, err := rep.db.Exec(`
		UPDATE agent_runs SET status = $2, cancel_requested = $3, worker_id = $4, heartbeat_at = $5, started_at = $6, finished_at = $7,
			exit_code = $8, final_text = $9, error = $10, tokens_in = $11, tokens_out = $12, cost_usd = $13, artifacts_touched = $14,
			error_class = $15, run_token_hash = ''
		WHERE id = $1 AND status NOT IN ('succeeded', 'failed', 'cancelled', 'timed_out', 'awaiting_approval')
	`, r.ID, r.Status, r.CancelRequested, r.WorkerID, r.HeartbeatAt, r.StartedAt, r.FinishedAt,
		r.ExitCode, r.FinalText, r.Error, r.TokensIn, r.TokensOut, r.CostUSD, touched, r.ErrorClass)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
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

// MarkRunning conditionally transitions a run from claimed to running,
// stamping started_at/heartbeat_at; reports whether the transition was
// applied. A run the reaper failed (or that was cancelled) in the meantime
// is left untouched.
func (rep *AgentRunRepository) MarkRunning(runID string, at time.Time) (bool, error) {
	res, err := rep.db.Exec(`
		UPDATE agent_runs SET status = 'running', started_at = $2, heartbeat_at = $2
		WHERE id = $1 AND status = 'claimed'
	`, runID, at)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// Heartbeat refreshes liveness while the run is still live; a heartbeat
// arriving after a terminal transition is a no-op so it can never make a
// finished run look alive again. Reports whether the refresh was applied.
func (rep *AgentRunRepository) Heartbeat(runID string, at time.Time) (bool, error) {
	res, err := rep.db.Exec(`
		UPDATE agent_runs SET heartbeat_at = $2
		WHERE id = $1 AND status IN ('claimed', 'running')
	`, runID, at)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// FailStale fails claimed/running runs whose heartbeat predates cutoff,
// revoking their run tokens along with the terminal transition.
func (rep *AgentRunRepository) FailStale(cutoff time.Time) ([]string, error) {
	rows, err := rep.db.Query(`
		UPDATE agent_runs SET status = 'failed', error = 'worker lost (heartbeat timeout)', error_class = 'worker_error', finished_at = NOW(), run_token_hash = ''
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

// listLogsPageLimit bounds a single ListLogs read. A busy run can accumulate
// tens of thousands of log rows; callers page with afterSeq (the last seq they
// saw), so a bounded page keeps each round-trip cheap while staying replayable
// — the caller re-requests from the new high-water mark to drain the rest.
const listLogsPageLimit = 2000

// ListLogs returns up to listLogsPageLimit log entries after a sequence
// number, ordered by seq. Because entries are ordered and afterSeq is the
// cursor, a caller that keeps calling with the last returned seq walks every
// entry across pages without gaps or repeats.
func (rep *AgentRunRepository) ListLogs(runID string, afterSeq int) ([]agentruns.LogEntry, error) {
	rows, err := rep.db.Query(`
		SELECT run_id, seq, kind, payload, created_at
		FROM agent_run_logs WHERE run_id = $1 AND seq > $2 ORDER BY seq
		LIMIT $3
	`, runID, afterSeq, listLogsPageLimit)
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

// Usage aggregates an org's runs created at/after since: once grouped by
// agent slug (biggest token consumers first) and once by calendar day (UTC,
// ascending). Runs of every status count; token/cost columns are zero until
// the worker's terminal report lands, so live runs add runs but no spend.
func (rep *AgentRunRepository) Usage(orgID string, since time.Time) ([]agentruns.AgentUsage, []agentruns.DailyUsage, error) {
	agentRows, err := rep.db.Query(`
		SELECT a.slug, a.name, COUNT(*),
			COALESCE(SUM(r.tokens_in), 0), COALESCE(SUM(r.tokens_out), 0),
			COALESCE(SUM(r.cost_usd), 0)::float8
		FROM agent_runs r JOIN agents a ON a.id = r.agent_id
		WHERE r.org_id = $1::uuid AND r.created_at >= $2
		GROUP BY a.slug, a.name
		ORDER BY SUM(r.tokens_in) + SUM(r.tokens_out) DESC, a.slug
	`, orgID, since)
	if err != nil {
		return nil, nil, err
	}
	defer agentRows.Close()

	var byAgent []agentruns.AgentUsage
	for agentRows.Next() {
		var u agentruns.AgentUsage
		if err := agentRows.Scan(&u.AgentSlug, &u.AgentName, &u.Runs, &u.TokensIn, &u.TokensOut, &u.CostUSD); err != nil {
			return nil, nil, err
		}
		byAgent = append(byAgent, u)
	}
	if err := agentRows.Err(); err != nil {
		return nil, nil, err
	}

	// Bucket by true UTC calendar day (issue #190). created_at is a naive
	// TIMESTAMP that stores the UTC wall-clock of each run (Save normalizes it
	// with .UTC() before persisting). Casting a naive timestamp to ::date takes
	// its date part with NO timezone conversion, so the bucket is that UTC day
	// regardless of the query session's TimeZone.
	//
	// Do NOT rewrite this as `(created_at AT TIME ZONE 'UTC')::date`: on a naive
	// column AT TIME ZONE 'UTC' yields a timestamptz whose ::date THEN folds
	// through the session TimeZone, so under a non-UTC session it reports the
	// wrong day — the opposite of the intent. That form is only correct for a
	// timestamptz column, which this is not.
	dayRows, err := rep.db.Query(`
		SELECT TO_CHAR(r.created_at::date, 'YYYY-MM-DD'), COUNT(*),
			COALESCE(SUM(r.tokens_in), 0), COALESCE(SUM(r.tokens_out), 0),
			COALESCE(SUM(r.cost_usd), 0)::float8
		FROM agent_runs r
		WHERE r.org_id = $1::uuid AND r.created_at >= $2
		GROUP BY r.created_at::date
		ORDER BY r.created_at::date
	`, orgID, since)
	if err != nil {
		return nil, nil, err
	}
	defer dayRows.Close()

	var byDay []agentruns.DailyUsage
	for dayRows.Next() {
		var u agentruns.DailyUsage
		if err := dayRows.Scan(&u.Day, &u.Runs, &u.TokensIn, &u.TokensOut, &u.CostUSD); err != nil {
			return nil, nil, err
		}
		byDay = append(byDay, u)
	}
	return byAgent, byDay, dayRows.Err()
}

// MonthlySpend sums an org's cost_usd over runs created at/after monthStart —
// the workspace's month-to-date spend. NULL costs (runs not yet finished)
// count as zero. It scopes on created_at, matching the Usage rollup's
// bucketing, so the budget figure agrees with what the usage tab shows.
func (rep *AgentRunRepository) MonthlySpend(orgID string, monthStart time.Time) (float64, error) {
	var spend float64
	err := rep.db.QueryRow(`
		SELECT COALESCE(SUM(cost_usd), 0)::float8
		FROM agent_runs
		WHERE org_id = $1::uuid AND created_at >= $2
	`, orgID, monthStart).Scan(&spend)
	return spend, err
}

// CountPendingProposals counts a run's unreviewed proposals.
func (rep *AgentRunRepository) CountPendingProposals(runID string) (int, error) {
	var count int
	err := rep.db.QueryRow(`SELECT COUNT(*) FROM agent_proposals WHERE run_id = $1 AND status = 'pending'`, runID).Scan(&count)
	return count, err
}

// CountApplyFailedProposals counts a run's approved proposals whose write
// failed to apply.
func (rep *AgentRunRepository) CountApplyFailedProposals(runID string) (int, error) {
	var count int
	err := rep.db.QueryRow(`SELECT COUNT(*) FROM agent_proposals WHERE run_id = $1 AND status = 'apply_failed'`, runID).Scan(&count)
	return count, err
}

// FinalizeApproval transitions an awaiting_approval run to a terminal status
// once its proposals are resolved, revoking its run token. The write is
// conditional on the run still being awaiting_approval so a concurrent
// resolver can never double-finalize; reports whether it was applied.
func (rep *AgentRunRepository) FinalizeApproval(runID, status string, at time.Time) (bool, error) {
	res, err := rep.db.Exec(`
		UPDATE agent_runs SET status = $2, finished_at = $3, run_token_hash = ''
		WHERE id = $1 AND status = 'awaiting_approval'
	`, runID, status, at)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
