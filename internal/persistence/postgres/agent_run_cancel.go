// AgentRunRepository's cancel: what asking a run to stop writes, by the
// run's status when the write lands (RequestCancel), and the cancel of a
// project's runs inside the transaction that deletes the project
// (cancelProjectRuns). ReleaseClaim ends a run whose cancel was requested
// as cancelQueuedRun writes it.

package postgres

import (
	"database/sql"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
)

// cancelQueuedRun is what cancelling a queued run writes: the run is
// cancelled at once and its token revoked. RequestCancel and a project's
// delete (cancelProjectRuns) write it alike; so do the delete for a run
// awaiting approval, and ReleaseClaim for a run whose cancel was requested,
// neither of which any worker will report.
const cancelQueuedRun = `status = 'cancelled', cancel_requested = TRUE, finished_at = NOW(), run_token_hash = ''`

// requestLiveRunCancel is what asking a claimed or running run to stop
// writes: the flag its worker reads on its next log push or heartbeat, after
// which it stops the agent and reports the run cancelled.
const requestLiveRunCancel = `cancel_requested = TRUE`

// RequestCancel cancels a run as its status asks when the write lands: a
// queued run is cancelled, its token revoked (cancelQueuedRun); a claimed
// or running run is asked to stop (requestLiveRunCancel); a run in any
// other status is left as it is. Reports whether it wrote.
//
// The run's row is locked from the read of its status to the write, so the
// write is the one its status at that moment asks for. A claim or a release
// that commits between the caller's read and this one changes which write
// that is, never whether there is one (#379 bug 165: a cancel that read a
// run as claimed, written only while the run was claimed or running, found
// it back in the queue after a release and matched nothing; the run was
// claimed and started again). A claim skips the row while it is locked
// here, and a release waits for it, then finds the cancel and ends the run
// cancelled (#379 bug 148).
func (rep *AgentRunRepository) RequestCancel(id string) (bool, error) {
	tx, err := rep.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var status string
	err = tx.QueryRow(`SELECT status FROM agent_runs WHERE id = $1 FOR UPDATE`, id).Scan(&status)
	if noRow(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var cancel string
	switch status {
	case agentruns.StatusQueued:
		cancel = cancelQueuedRun
	case agentruns.StatusClaimed, agentruns.StatusRunning:
		cancel = requestLiveRunCancel
	default:
		return false, nil
	}
	if _, err := tx.Exec(`UPDATE agent_runs SET `+cancel+` WHERE id = $1`, id); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// cancelProjectRuns cancels a project's live runs inside the transaction that
// deletes the project (#379 bug 137: they went on with no project, their
// token reaching nothing), as RequestCancel cancels one: a queued run is
// cancelled, with its token revoked; a claimed or running run is asked to
// stop, and its token is revoked at once besides, since the project it
// acted in is gone.
// The worker reports to the server with its own key, not the run's token,
// so it still reads the flag and reports the run cancelled. A run awaiting
// approval is cancelled as a queued one is (#379 bug 146): its proposals go
// with the project, so no review could ever finalise it. It answers the ids
// of the runs it cancelled or asked to stop, for the caller to announce
// once the transaction has committed.
//
// The queued runs go first. A claim takes a queued run with FOR UPDATE SKIP
// LOCKED, so it skips the runs the first statement holds, and after the
// commit finds them cancelled; a claim that took one of them before that
// statement reached it made it claimed, which the second statement, reading
// afresh, then asks to stop. The other order would let a run queued when
// the live runs were read be claimed before the queued ones were, and escape
// both.
func cancelProjectRuns(tx *sql.Tx, projectID string) ([]string, error) {
	var ids []string
	for _, stmt := range []string{
		`UPDATE agent_runs SET ` + cancelQueuedRun + ` WHERE project_id = $1 AND status = 'queued' RETURNING id`,
		`UPDATE agent_runs SET ` + requestLiveRunCancel + `, run_token_hash = '' WHERE project_id = $1 AND status IN ('claimed', 'running') RETURNING id`,
		`UPDATE agent_runs SET ` + cancelQueuedRun + ` WHERE project_id = $1 AND status = 'awaiting_approval' RETURNING id`,
	} {
		rows, err := tx.Query(stmt, projectID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return ids, nil
}
