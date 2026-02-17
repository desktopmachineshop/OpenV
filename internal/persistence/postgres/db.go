package postgres

import (
	"database/sql"
	"fmt"
	"time"
)

// Connect establishes a PostgreSQL connection
func Connect(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}

	// Configure connection pool
	// Higher MaxOpenConns to handle concurrent artifact creation during import
	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(10 * time.Second) // 10 seconds

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
		id UUID NOT NULL,
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
		updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
		PRIMARY KEY (id, version)
	);

	CREATE INDEX IF NOT EXISTS idx_artifacts_project_id ON artifacts(project_id);
	CREATE INDEX IF NOT EXISTS idx_artifacts_parent_id ON artifacts(parent_id);
	CREATE INDEX IF NOT EXISTS idx_artifacts_type ON artifacts(type);
	CREATE INDEX IF NOT EXISTS idx_artifacts_valid_to ON artifacts(valid_to);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_artifacts_id_active ON artifacts(id) WHERE valid_to IS NULL;

	CREATE TABLE IF NOT EXISTS links (
		id UUID PRIMARY KEY,
		from_id UUID NOT NULL,
		to_id UUID NOT NULL,
		type VARCHAR(255) NOT NULL,
		attributes JSONB DEFAULT '{}',
		version INT NOT NULL DEFAULT 1,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW()
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
		created_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_attachments_artifact_id ON attachments(artifact_id);

	CREATE TABLE IF NOT EXISTS baselines (
		id UUID PRIMARY KEY,
		project_id UUID NOT NULL,
		name VARCHAR(512) NOT NULL,
		snapshot JSONB NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT NOW()
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

	CREATE TABLE IF NOT EXISTS chatter (
		id UUID PRIMARY KEY,
		artifact_id UUID NOT NULL,
		message TEXT NOT NULL,
		is_auto_entry BOOLEAN NOT NULL DEFAULT FALSE,
		entry_type VARCHAR(64) NOT NULL DEFAULT 'comment',
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_chatter_artifact_id ON chatter(artifact_id);
	CREATE INDEX IF NOT EXISTS idx_chatter_created_at ON chatter(created_at);
	`

	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Add soft referential integrity checks via application logic
	// We don't use traditional foreign keys for artifact references because of temporal versioning
	// where we have composite keys (id, version) but need to reference by ID only
	constraintSQL := `
	DO $$ 
	BEGIN
		-- Only add the baselines->projects foreign key since projects don't use versioning
		IF NOT EXISTS (
			SELECT 1 FROM pg_constraint 
			WHERE conname = 'baselines_project_id_fkey'
		) THEN
			ALTER TABLE baselines 
			ADD CONSTRAINT baselines_project_id_fkey 
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE;
		END IF;
	END $$;
	`

	_, err = db.Exec(constraintSQL)
	if err != nil {
		return fmt.Errorf("failed to add constraints: %w", err)
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

	// Add versioning columns to links table for temporal tracking
	const addLinkVersioningSQL = `
	DO $$
	BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name='links' AND column_name='valid_from'
		) THEN
			ALTER TABLE links ADD COLUMN valid_from TIMESTAMP NOT NULL DEFAULT NOW();
			ALTER TABLE links ADD COLUMN valid_to TIMESTAMP;
		END IF;
	END $$;
	`

	_, err = db.Exec(addLinkVersioningSQL)
	if err != nil {
		return fmt.Errorf("failed to add link versioning columns: %w", err)
	}

	// Create indexes for link versioning
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_links_valid_to ON links(valid_to);`)
	if err != nil {
		return fmt.Errorf("failed to add links valid_to index: %w", err)
	}

	_, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_links_active ON links(id) WHERE valid_to IS NULL;`)
	if err != nil {
		return fmt.Errorf("failed to add links active index: %w", err)
	}

	// Create link_artifacts mapping table if it doesn't exist
	const createLinkArtifactsSQL = `
	CREATE TABLE IF NOT EXISTS link_artifacts (
		link_id UUID NOT NULL,
		artifact_id UUID NOT NULL,
		artifact_version INT NOT NULL,
		active BOOLEAN DEFAULT true,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		PRIMARY KEY (link_id, artifact_id, artifact_version)
	);
	`

	_, err = db.Exec(createLinkArtifactsSQL)
	if err != nil {
		return fmt.Errorf("failed to create link_artifacts table: %w", err)
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_link_artifacts_artifact ON link_artifacts(artifact_id, artifact_version);`)
	if err != nil {
		return fmt.Errorf("failed to add link_artifacts index: %w", err)
	}

	return nil
}
