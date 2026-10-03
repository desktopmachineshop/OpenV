package postgres

import "database/sql"

// 0049: a test result's history (REQ-13). A result recorded again for a
// case its run already has one for is a row of its own, and the newest
// is the case's current result, where the upsert on (run_id, test_case_id)
// wrote over the row and kept no trace of the outcome it replaced.
//
// The pair stays indexed under the same name, no longer unique. The name
// matters: the 0001 baseline re-runs on every boot and creates
// idx_test_results_run_case UNIQUE ... IF NOT EXISTS, which checks the
// name only, so keeping it is what stops a reboot from putting the
// constraint back (and failing on the history it would then meet).
func m0049TestResultsHistory(tx *sql.Tx) error {
	if _, err := tx.Exec(`DROP INDEX IF EXISTS idx_test_results_run_case`); err != nil {
		return err
	}
	_, err := tx.Exec(`CREATE INDEX idx_test_results_run_case ON test_results(run_id, test_case_id)`)
	return err
}
