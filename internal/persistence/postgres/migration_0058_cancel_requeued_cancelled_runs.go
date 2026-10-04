package postgres

import "database/sql"

// 0058: a run whose cancel was requested is not left in the queue (#379 bug
// 148). ReleaseClaim put a claimed or running run back in the queue even
// when its cancel had been requested, its flag still set, and the next
// claim started it again; it now ends such a run cancelled, and a claim
// never takes a run whose cancel was requested. The runs the old release
// left queued with the flag set would then wait in the queue for ever, so
// each is ended as cancelling a queued run ends it: cancelled, finished
// now, its token revoked. Every other run is left as it was. A second run
// finds none left to end.
func m0058CancelRequeuedCancelledRuns(tx *sql.Tx) error {
	_, err := tx.Exec(`
		UPDATE agent_runs SET status = 'cancelled', finished_at = NOW(), run_token_hash = '', partial_text = ''
		WHERE status = 'queued' AND cancel_requested
	`)
	return err
}
