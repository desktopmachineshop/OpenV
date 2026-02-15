package postgres

import (
	"database/sql"
	"fmt"
)

// Connect establishes a PostgreSQL connection
func Connect(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}

	err = db.Ping()
	if err != nil {
		return nil, err
	}

	return db, nil
}

// InitSchema creates the database schema
func InitSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS artifacts (
		id UUID PRIMARY KEY,
		project_id UUID NOT NULL,
		parent_id UUID,
		type VARCHAR(255) NOT NULL,
		title VARCHAR(512) NOT NULL,
		body TEXT,
		sort_order INT NOT NULL DEFAULT 0,
		attributes JSONB DEFAULT '{}',
		version INT NOT NULL DEFAULT 1,
		valid_from TIMESTAMP NOT NULL DEFAULT NOW(),
		valid_to TIMESTAMP,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_artifacts_project_id ON artifacts(project_id);
	CREATE INDEX IF NOT EXISTS idx_artifacts_parent_id ON artifacts(parent_id);
	CREATE INDEX IF NOT EXISTS idx_artifacts_type ON artifacts(type);
	CREATE INDEX IF NOT EXISTS idx_artifacts_valid_to ON artifacts(valid_to);

	CREATE TABLE IF NOT EXISTS links (
		id UUID PRIMARY KEY,
		from_id UUID NOT NULL,
		to_id UUID NOT NULL,
		type VARCHAR(255) NOT NULL,
		attributes JSONB DEFAULT '{}',
		version INT NOT NULL DEFAULT 1,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
		FOREIGN KEY (from_id) REFERENCES artifacts(id) ON DELETE CASCADE,
		FOREIGN KEY (to_id) REFERENCES artifacts(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_links_from_id ON links(from_id);
	CREATE INDEX IF NOT EXISTS idx_links_to_id ON links(to_id);
	CREATE INDEX IF NOT EXISTS idx_links_type ON links(type);

	CREATE TABLE IF NOT EXISTS projects (
		id UUID PRIMARY KEY,
		name VARCHAR(512) NOT NULL,
		description TEXT,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS attachments (
		id UUID PRIMARY KEY,
		artifact_id UUID NOT NULL,
		filename VARCHAR(512) NOT NULL,
		mime_type VARCHAR(128) NOT NULL,
		file_path VARCHAR(1024) NOT NULL,
		file_size INT NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		FOREIGN KEY (artifact_id) REFERENCES artifacts(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_attachments_artifact_id ON attachments(artifact_id);

	CREATE TABLE IF NOT EXISTS baselines (
		id UUID PRIMARY KEY,
		project_id UUID NOT NULL,
		name VARCHAR(512) NOT NULL,
		snapshot JSONB NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_baselines_project_id ON baselines(project_id);

	CREATE TABLE IF NOT EXISTS templates (
		id UUID PRIMARY KEY,
		template_key VARCHAR(128),
		name VARCHAR(512) NOT NULL,
		description TEXT,
		snapshot JSONB NOT NULL,
		is_default BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_templates_key ON templates(template_key);
	`

	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Add foreign key constraint for parent_id after table exists (for self-referential relationship)
	constraintSQL := `
	DO $$ 
	BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM pg_constraint 
			WHERE conname = 'artifacts_parent_id_fkey'
		) THEN
			ALTER TABLE artifacts 
			ADD CONSTRAINT artifacts_parent_id_fkey 
			FOREIGN KEY (parent_id) REFERENCES artifacts(id) ON DELETE CASCADE;
		END IF;
	END $$;
	`

	_, err = db.Exec(constraintSQL)
	if err != nil {
		return fmt.Errorf("failed to add parent_id constraint: %w", err)
	}

	const addSortOrderSQL = `
	DO $$
	BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name='artifacts' AND column_name='sort_order'
		) THEN
			ALTER TABLE artifacts ADD COLUMN sort_order INT NOT NULL DEFAULT 0;
		END IF;
	END $$;
	`

	_, err = db.Exec(addSortOrderSQL)
	if err != nil {
		return fmt.Errorf("failed to add sort_order column: %w", err)
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_artifacts_sort_order ON artifacts(sort_order);`)
	if err != nil {
		return fmt.Errorf("failed to add sort_order index: %w", err)
	}

	return nil
}
