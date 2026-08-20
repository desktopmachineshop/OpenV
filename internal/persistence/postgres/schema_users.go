package postgres

import (
	"database/sql"
	"fmt"
)

// InitUserSchema creates the users, sessions, and project membership tables,
// and adds created_by attribution columns to existing tables.
// All statements are idempotent so the schema can be re-initialized safely.
func InitUserSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id UUID PRIMARY KEY,
		email VARCHAR(320) NOT NULL,
		name VARCHAR(255) NOT NULL DEFAULT '',
		avatar_url VARCHAR(1024) NOT NULL DEFAULT '',
		auth_provider VARCHAR(32) NOT NULL DEFAULT 'password',
		password_hash VARCHAR(255),
		is_admin BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(LOWER(email));

	CREATE TABLE IF NOT EXISTS sessions (
		id UUID PRIMARY KEY,
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token_hash VARCHAR(128) NOT NULL,
		expires_at TIMESTAMP NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		last_seen_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_token_hash ON sessions(token_hash);
	CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
	CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);

	CREATE TABLE IF NOT EXISTS project_members (
		project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		role VARCHAR(32) NOT NULL DEFAULT 'viewer',
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		PRIMARY KEY (project_id, user_id)
	);

	CREATE INDEX IF NOT EXISTS idx_project_members_user_id ON project_members(user_id);
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create user schema: %w", err)
	}

	// Attribution columns on existing tables. Nullable so pre-auth rows stay valid.
	attributionSQL := `
	DO $$
	BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name='artifacts' AND column_name='created_by'
		) THEN
			ALTER TABLE artifacts ADD COLUMN created_by UUID;
		END IF;

		IF NOT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name='chatter' AND column_name='created_by'
		) THEN
			ALTER TABLE chatter ADD COLUMN created_by UUID;
			ALTER TABLE chatter ADD COLUMN author_name VARCHAR(255) NOT NULL DEFAULT '';
		END IF;

		IF NOT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name='baselines' AND column_name='created_by'
		) THEN
			ALTER TABLE baselines ADD COLUMN created_by UUID;
		END IF;
	END $$;
	`

	if _, err := db.Exec(attributionSQL); err != nil {
		return fmt.Errorf("failed to add attribution columns: %w", err)
	}

	return nil
}
