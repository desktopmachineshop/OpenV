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
		type VARCHAR(255) NOT NULL,
		title VARCHAR(512) NOT NULL,
		body TEXT,
		attributes JSONB DEFAULT '{}',
		version INT NOT NULL DEFAULT 1,
		valid_from TIMESTAMP NOT NULL DEFAULT NOW(),
		valid_to TIMESTAMP,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_artifacts_project_id ON artifacts(project_id);
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
	`

	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	return nil
}
