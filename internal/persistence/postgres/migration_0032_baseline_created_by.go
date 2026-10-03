package postgres

import "database/sql"

// 0032: who captured a baseline. A baseline is the audit anchor of a
// project — the thing a later argument about what was agreed is settled
// against — and until now it recorded only when it was taken, never by
// whom. Nullable rather than defaulted: the baselines that already exist
// were captured by somebody nobody recorded, and inventing an author for
// them would be worse than admitting there isn't one. ON DELETE SET NULL
// so removing an account does not take the project's history with it.
func m0032BaselineCreatedBy(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			ALTER TABLE baselines
				ADD COLUMN IF NOT EXISTS created_by UUID
		`); err != nil {
		return err
	}
	// Added separately and tolerantly: the constraint is worth having,
	// but a deployment whose users table has drifted must still get the
	// column rather than fail the whole migration over the key.
	_, err := tx.Exec(`
			DO $$
			BEGIN
				ALTER TABLE baselines
					ADD CONSTRAINT baselines_created_by_fkey
					FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL;
			EXCEPTION
				WHEN duplicate_object THEN NULL;
				WHEN undefined_table THEN NULL;
			END $$;
		`)
	return err
}
