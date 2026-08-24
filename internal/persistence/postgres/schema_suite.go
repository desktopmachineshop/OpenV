package postgres

import (
	"database/sql"
	"fmt"
)

// InitSuiteSchema creates the product definition, guided requirements, V&V,
// kanban, and interview tables. All statements are idempotent.
func InitSuiteSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS product_profiles (
		project_id UUID PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
		vision TEXT NOT NULL DEFAULT '',
		problem_statement TEXT NOT NULL DEFAULT '',
		target_users TEXT NOT NULL DEFAULT '',
		constraints JSONB NOT NULL DEFAULT '[]',
		success_metrics JSONB NOT NULL DEFAULT '[]',
		settings JSONB NOT NULL DEFAULT '{}',
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS guided_sessions (
		id UUID PRIMARY KEY,
		project_id UUID,
		status VARCHAR(32) NOT NULL DEFAULT 'in-progress',
		current_step INT NOT NULL DEFAULT 0,
		answers JSONB NOT NULL DEFAULT '{}',
		draft_artifact_ids JSONB NOT NULL DEFAULT '[]',
		agent_run_id UUID,
		created_by UUID,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_guided_sessions_project_id ON guided_sessions(project_id);

	CREATE TABLE IF NOT EXISTS guided_session_messages (
		id UUID PRIMARY KEY,
		session_id UUID NOT NULL REFERENCES guided_sessions(id) ON DELETE CASCADE,
		role VARCHAR(32) NOT NULL,
		content TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_guided_session_messages_session ON guided_session_messages(session_id, created_at);

	CREATE TABLE IF NOT EXISTS test_runs (
		id UUID PRIMARY KEY,
		project_id UUID NOT NULL,
		name VARCHAR(512) NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		baseline_id UUID,
		status VARCHAR(32) NOT NULL DEFAULT 'in-progress',
		started_at TIMESTAMP NOT NULL DEFAULT NOW(),
		completed_at TIMESTAMP,
		created_by UUID,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_test_runs_project_id ON test_runs(project_id);

	CREATE TABLE IF NOT EXISTS test_results (
		id UUID PRIMARY KEY,
		run_id UUID NOT NULL REFERENCES test_runs(id) ON DELETE CASCADE,
		test_case_id UUID NOT NULL,
		test_case_version INT NOT NULL DEFAULT 1,
		status VARCHAR(32) NOT NULL DEFAULT 'not-run',
		notes TEXT NOT NULL DEFAULT '',
		evidence JSONB NOT NULL DEFAULT '[]',
		executed_at TIMESTAMP,
		executed_by UUID,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_test_results_run_case ON test_results(run_id, test_case_id);
	CREATE INDEX IF NOT EXISTS idx_test_results_case ON test_results(test_case_id);

	CREATE TABLE IF NOT EXISTS work_items (
		id UUID PRIMARY KEY,
		project_id UUID NOT NULL,
		title VARCHAR(512) NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		board_column VARCHAR(32) NOT NULL DEFAULT 'backlog',
		sort_order INT NOT NULL DEFAULT 0,
		assignee_type VARCHAR(32) NOT NULL DEFAULT 'user',
		assignee_id UUID,
		agent_run_id UUID,
		artifact_ids JSONB NOT NULL DEFAULT '[]',
		due_date TIMESTAMP,
		created_by UUID,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_work_items_project_id ON work_items(project_id);
	CREATE INDEX IF NOT EXISTS idx_work_items_column ON work_items(project_id, board_column, sort_order);

	CREATE TABLE IF NOT EXISTS work_item_activity (
		id UUID PRIMARY KEY,
		work_item_id UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
		kind VARCHAR(32) NOT NULL,
		actor VARCHAR(160) NOT NULL DEFAULT 'system',
		content TEXT NOT NULL DEFAULT '',
		payload JSONB NOT NULL DEFAULT '{}',
		created_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_work_item_activity_item ON work_item_activity(work_item_id, created_at);

	CREATE TABLE IF NOT EXISTS interviews (
		id UUID PRIMARY KEY,
		project_id UUID NOT NULL,
		guided_session_id UUID,
		persona_artifact_id UUID,
		name VARCHAR(512) NOT NULL,
		brief TEXT NOT NULL DEFAULT '',
		agent_id UUID,
		status VARCHAR(32) NOT NULL DEFAULT 'open',
		expires_at TIMESTAMP,
		created_by UUID,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_interviews_project_id ON interviews(project_id);

	CREATE TABLE IF NOT EXISTS interview_invites (
		id UUID PRIMARY KEY,
		interview_id UUID NOT NULL REFERENCES interviews(id) ON DELETE CASCADE,
		token_hash VARCHAR(128) NOT NULL,
		invitee_label VARCHAR(255) NOT NULL DEFAULT '',
		expires_at TIMESTAMP,
		revoked BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_interview_invites_token ON interview_invites(token_hash);
	CREATE INDEX IF NOT EXISTS idx_interview_invites_interview ON interview_invites(interview_id);

	CREATE TABLE IF NOT EXISTS interview_sessions (
		id UUID PRIMARY KEY,
		interview_id UUID NOT NULL REFERENCES interviews(id) ON DELETE CASCADE,
		invite_id UUID NOT NULL,
		participant_name VARCHAR(255) NOT NULL DEFAULT '',
		status VARCHAR(32) NOT NULL DEFAULT 'active',
		summary TEXT NOT NULL DEFAULT '',
		started_at TIMESTAMP NOT NULL DEFAULT NOW(),
		ended_at TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_interview_sessions_interview ON interview_sessions(interview_id);

	CREATE TABLE IF NOT EXISTS interview_messages (
		id UUID PRIMARY KEY,
		session_id UUID NOT NULL REFERENCES interview_sessions(id) ON DELETE CASCADE,
		role VARCHAR(32) NOT NULL,
		content TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_interview_messages_session ON interview_messages(session_id, created_at);
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create suite schema: %w", err)
	}

	// Evidence attachments may belong to a test result instead of an artifact.
	alterSQL := `
	DO $$
	BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name='attachments' AND column_name='test_result_id'
		) THEN
			ALTER TABLE attachments ADD COLUMN test_result_id UUID;
			ALTER TABLE attachments ALTER COLUMN artifact_id DROP NOT NULL;
		END IF;
	END $$;
	`

	if _, err := db.Exec(alterSQL); err != nil {
		return fmt.Errorf("failed to extend attachments for test evidence: %w", err)
	}

	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_attachments_test_result_id ON attachments(test_result_id);`); err != nil {
		return fmt.Errorf("failed to add attachments test_result_id index: %w", err)
	}

	// Interviews may be linked (many to one) to a persona artifact.
	personaSQL := `
	DO $$
	BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name='interviews' AND column_name='persona_artifact_id'
		) THEN
			ALTER TABLE interviews ADD COLUMN persona_artifact_id UUID;
		END IF;
	END $$;
	`

	if _, err := db.Exec(personaSQL); err != nil {
		return fmt.Errorf("failed to add interviews persona_artifact_id column: %w", err)
	}

	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_interviews_persona ON interviews(persona_artifact_id);`); err != nil {
		return fmt.Errorf("failed to add interviews persona index: %w", err)
	}

	return nil
}
