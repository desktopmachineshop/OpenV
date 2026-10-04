package postgres

import (
	"database/sql"
	"fmt"
)

// 0057: attachments.test_result_id goes (#379 bug 150, question 49). The
// 0001 baseline added it for evidence attached to a test result rather than
// an artifact, with no foreign key, but nothing has ever written or read
// it: evidence is held by evidence_bundles and cited from a result through
// evidence_citations (0030). The baseline no longer adds it, so a new
// database never has it, and here it is dropped where it is, with its
// index.
//
// Every row must hold NULL in it first. One that names a test result was
// written outside OpenV, and dropping the column would lose what it says,
// so the migration fails instead, naming how many rows hold one and what to
// do, and the server does not start until they are cleared.
func m0057DropAttachmentTestResult(tx *sql.Tx) error {
	var present bool
	if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'attachments' AND column_name = 'test_result_id')`).Scan(&present); err != nil {
		return err
	}
	if !present {
		return nil
	}
	var held int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM attachments WHERE test_result_id IS NOT NULL`).Scan(&held); err != nil {
		return err
	}
	if held > 0 {
		return fmt.Errorf("%d attachments rows hold a test_result_id, a column OpenV never wrote and now drops; "+
			"copy what they say elsewhere if you need it, set it to NULL "+
			"(UPDATE attachments SET test_result_id = NULL), and start the server again", held)
	}
	for _, stmt := range []string{
		`DROP INDEX IF EXISTS idx_attachments_test_result_id`,
		`ALTER TABLE attachments DROP COLUMN test_result_id`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
