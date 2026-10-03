package postgres

import "database/sql"

// 0040: the workspace a member's sign-in lands in (REQ-156). Nullable:
// a member who has chosen none lands in their personal workspace as
// before. The reference clears itself when that workspace is purged, so
// a stale choice can never point at nothing.
func m0040UsersDefaultOrg(tx *sql.Tx) error {
	_, err := tx.Exec(`
			DO $$
			BEGIN
				IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='users' AND column_name='default_org_id') THEN
					ALTER TABLE users ADD COLUMN default_org_id UUID;
				END IF;
				BEGIN
					ALTER TABLE users ADD CONSTRAINT users_default_org_id_fkey
						FOREIGN KEY (default_org_id) REFERENCES organizations(id) ON DELETE SET NULL;
				EXCEPTION WHEN duplicate_object THEN NULL;
				END;
			END $$;
		`)
	return err
}
