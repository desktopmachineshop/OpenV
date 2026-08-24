package postgres

import (
	"database/sql"
	"fmt"
)

// InitAgentSchema creates the agent, automation, event, team, and proposal tables.
// All statements are idempotent.
func InitAgentSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS agents (
		id UUID PRIMARY KEY,
		slug VARCHAR(128) NOT NULL,
		name VARCHAR(512) NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		provider VARCHAR(64) NOT NULL,
		model VARCHAR(128) NOT NULL DEFAULT '',
		effort VARCHAR(16) NOT NULL DEFAULT '',
		allowed_tools JSONB NOT NULL DEFAULT '[]',
		write_mode VARCHAR(32) NOT NULL DEFAULT 'proposal',
		repo_access BOOLEAN NOT NULL DEFAULT FALSE,
		max_turns INT NOT NULL DEFAULT 50,
		timeout_seconds INT NOT NULL DEFAULT 1800,
		config JSONB NOT NULL DEFAULT '{}',
		system_prompt TEXT NOT NULL DEFAULT '',
		file_path VARCHAR(1024) NOT NULL DEFAULT '',
		content_hash VARCHAR(128) NOT NULL DEFAULT '',
		synced_at TIMESTAMP,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	-- Slug uniqueness is per-org: idx_agents_org_slug, owned by InitOrgSchema.
	CREATE INDEX IF NOT EXISTS idx_agents_slug_lookup ON agents(slug);

	CREATE TABLE IF NOT EXISTS agent_runs (
		id UUID PRIMARY KEY,
		agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
		project_id UUID,
		automation_id UUID,
		trigger_event_id UUID,
		team_id UUID,
		team_node_id UUID,
		parent_run_id UUID,
		work_item_id UUID,
		interview_session_id UUID,
		guided_session_id UUID,
		status VARCHAR(32) NOT NULL DEFAULT 'queued',
		cancel_requested BOOLEAN NOT NULL DEFAULT FALSE,
		priority INT NOT NULL DEFAULT 0,
		prompt TEXT NOT NULL,
		run_token_hash VARCHAR(128) NOT NULL DEFAULT '',
		worker_id VARCHAR(128) NOT NULL DEFAULT '',
		heartbeat_at TIMESTAMP,
		started_at TIMESTAMP,
		finished_at TIMESTAMP,
		exit_code INT,
		final_text TEXT NOT NULL DEFAULT '',
		error TEXT NOT NULL DEFAULT '',
		tokens_in BIGINT NOT NULL DEFAULT 0,
		tokens_out BIGINT NOT NULL DEFAULT 0,
		cost_usd NUMERIC(10,4),
		artifacts_touched JSONB NOT NULL DEFAULT '[]',
		launched_by UUID,
		created_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_agent_runs_status ON agent_runs(status);
	CREATE INDEX IF NOT EXISTS idx_agent_runs_agent_id ON agent_runs(agent_id);
	CREATE INDEX IF NOT EXISTS idx_agent_runs_project_id ON agent_runs(project_id);
	CREATE INDEX IF NOT EXISTS idx_agent_runs_parent ON agent_runs(parent_run_id);
	CREATE INDEX IF NOT EXISTS idx_agent_runs_created_at ON agent_runs(created_at);
	CREATE INDEX IF NOT EXISTS idx_agent_runs_token_hash ON agent_runs(run_token_hash);

	CREATE TABLE IF NOT EXISTS agent_run_logs (
		id BIGSERIAL PRIMARY KEY,
		run_id UUID NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
		seq INT NOT NULL,
		kind VARCHAR(32) NOT NULL,
		payload JSONB NOT NULL DEFAULT '{}',
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		UNIQUE (run_id, seq)
	);

	CREATE INDEX IF NOT EXISTS idx_agent_run_logs_run ON agent_run_logs(run_id, seq);

	CREATE TABLE IF NOT EXISTS automations (
		id UUID PRIMARY KEY,
		name VARCHAR(512) NOT NULL,
		agent_id UUID REFERENCES agents(id) ON DELETE CASCADE,
		team_id UUID,
		project_id UUID,
		kind VARCHAR(32) NOT NULL,
		enabled BOOLEAN NOT NULL DEFAULT TRUE,
		prompt_template TEXT NOT NULL DEFAULT '',
		cron_expr VARCHAR(128) NOT NULL DEFAULT '',
		catch_up BOOLEAN NOT NULL DEFAULT FALSE,
		next_run_at TIMESTAMP,
		last_run_at TIMESTAMP,
		event_type VARCHAR(64) NOT NULL DEFAULT '',
		event_filter JSONB NOT NULL DEFAULT '{}',
		cooldown_seconds INT NOT NULL DEFAULT 60,
		max_runs_per_hour INT NOT NULL DEFAULT 10,
		created_by UUID,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_automations_next_run ON automations(next_run_at) WHERE enabled AND kind = 'scheduled';
	CREATE INDEX IF NOT EXISTS idx_automations_event ON automations(event_type) WHERE enabled AND kind = 'triggered';

	CREATE TABLE IF NOT EXISTS domain_events (
		id UUID PRIMARY KEY,
		event_type VARCHAR(64) NOT NULL,
		project_id UUID,
		entity_id UUID,
		actor VARCHAR(160) NOT NULL DEFAULT 'system',
		payload JSONB NOT NULL DEFAULT '{}',
		created_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_domain_events_type ON domain_events(event_type, created_at);
	CREATE INDEX IF NOT EXISTS idx_domain_events_project ON domain_events(project_id, created_at);

	CREATE TABLE IF NOT EXISTS repo_connections (
		id UUID PRIMARY KEY,
		project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
		name VARCHAR(512) NOT NULL,
		remote_url VARCHAR(1024) NOT NULL DEFAULT '',
		default_branch VARCHAR(128) NOT NULL DEFAULT 'main',
		credential_strategy VARCHAR(32) NOT NULL DEFAULT 'host',
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_repo_connections_project ON repo_connections(project_id);

	-- Where a repo connection is checked out on a given member's machine:
	-- agents run locally, so the location differs for every user.
	CREATE TABLE IF NOT EXISTS user_repo_paths (
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		repo_connection_id UUID NOT NULL REFERENCES repo_connections(id) ON DELETE CASCADE,
		local_path VARCHAR(1024) NOT NULL,
		updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
		PRIMARY KEY (user_id, repo_connection_id)
	);

	CREATE TABLE IF NOT EXISTS provider_settings (
		id UUID PRIMARY KEY,
		provider VARCHAR(64) NOT NULL,
		auth_mode VARCHAR(32) NOT NULL DEFAULT 'subscription-cli',
		api_key_env VARCHAR(128) NOT NULL DEFAULT '',
		default_model VARCHAR(128) NOT NULL DEFAULT '',
		enabled BOOLEAN NOT NULL DEFAULT TRUE,
		last_detected JSONB NOT NULL DEFAULT '{}',
		updated_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	-- Provider uniqueness is per-org: idx_provider_settings_org_provider,
	-- owned by InitOrgSchema.
	CREATE INDEX IF NOT EXISTS idx_provider_settings_lookup ON provider_settings(provider);

	CREATE TABLE IF NOT EXISTS agent_proposals (
		id UUID PRIMARY KEY,
		run_id UUID NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
		project_id UUID NOT NULL,
		op VARCHAR(32) NOT NULL,
		target_id UUID,
		payload JSONB NOT NULL DEFAULT '{}',
		status VARCHAR(32) NOT NULL DEFAULT 'pending',
		review_note TEXT NOT NULL DEFAULT '',
		applied_entity_id UUID,
		reviewed_by UUID,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		reviewed_at TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_agent_proposals_run ON agent_proposals(run_id);
	CREATE INDEX IF NOT EXISTS idx_agent_proposals_status ON agent_proposals(status, project_id);

	CREATE TABLE IF NOT EXISTS provider_logins (
		id UUID PRIMARY KEY,
		provider VARCHAR(64) NOT NULL,
		target VARCHAR(32) NOT NULL DEFAULT 'workspace',
		status VARCHAR(32) NOT NULL DEFAULT 'pending',
		auth_url TEXT NOT NULL DEFAULT '',
		code TEXT NOT NULL DEFAULT '',
		detail TEXT NOT NULL DEFAULT '',
		requested_by UUID,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_provider_logins_status ON provider_logins(status, created_at);
	CREATE INDEX IF NOT EXISTS idx_provider_logins_provider ON provider_logins(provider, updated_at);

	CREATE TABLE IF NOT EXISTS agent_teams (
		id UUID PRIMARY KEY,
		name VARCHAR(512) NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		project_id UUID,
		entry_node_id UUID,
		is_default BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS agent_team_nodes (
		id UUID PRIMARY KEY,
		team_id UUID NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
		agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
		label VARCHAR(255) NOT NULL,
		department VARCHAR(255) NOT NULL DEFAULT '',
		position JSONB NOT NULL DEFAULT '{}',
		created_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_agent_team_nodes_team ON agent_team_nodes(team_id);

	CREATE TABLE IF NOT EXISTS agent_team_edges (
		id UUID PRIMARY KEY,
		team_id UUID NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
		from_node_id UUID NOT NULL REFERENCES agent_team_nodes(id) ON DELETE CASCADE,
		to_node_id UUID NOT NULL REFERENCES agent_team_nodes(id) ON DELETE CASCADE,
		edge_type VARCHAR(32) NOT NULL,
		config JSONB NOT NULL DEFAULT '{}',
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		UNIQUE (team_id, from_node_id, to_node_id, edge_type)
	);

	CREATE INDEX IF NOT EXISTS idx_agent_team_edges_team ON agent_team_edges(team_id);
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create agent schema: %w", err)
	}

	// Department grouping on team nodes (added after initial release).
	alterSQL := `
	DO $$
	BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name='agent_team_nodes' AND column_name='department'
		) THEN
			ALTER TABLE agent_team_nodes ADD COLUMN department VARCHAR(255) NOT NULL DEFAULT '';
		END IF;
	END $$;
	`
	if _, err := db.Exec(alterSQL); err != nil {
		return fmt.Errorf("failed to add team node department column: %w", err)
	}

	// Login targeting (added with the per-user settings panel): existing rows
	// keep the historical workspace behavior.
	targetSQL := `
	DO $$
	BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name='provider_logins' AND column_name='target'
		) THEN
			ALTER TABLE provider_logins ADD COLUMN target VARCHAR(32) NOT NULL DEFAULT 'workspace';
		END IF;
	END $$;
	`
	if _, err := db.Exec(targetSQL); err != nil {
		return fmt.Errorf("failed to add provider login target column: %w", err)
	}

	// Repo connections used to carry a shared local_path that members without
	// their own path fell back to. Since every run happens on a member's own
	// machine, hand the old shared value to each member as their own path and
	// drop the column.
	dropSharedPathSQL := `
	DO $$
	BEGIN
		IF EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name='repo_connections' AND column_name='local_path'
		) THEN
			INSERT INTO user_repo_paths (user_id, repo_connection_id, local_path, updated_at)
			SELECT m.user_id, c.id, c.local_path, NOW()
			FROM repo_connections c
			JOIN project_members m ON m.project_id = c.project_id
			WHERE c.local_path <> ''
			ON CONFLICT (user_id, repo_connection_id) DO NOTHING;

			ALTER TABLE repo_connections DROP COLUMN local_path;
		END IF;
	END $$;
	`
	if _, err := db.Exec(dropSharedPathSQL); err != nil {
		return fmt.Errorf("failed to drop shared repo connection local_path: %w", err)
	}

	// Guided-copilot turn runs link back to their guided session (added with
	// the in-wizard AI chat).
	guidedSQL := `
	DO $$
	BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name='agent_runs' AND column_name='guided_session_id'
		) THEN
			ALTER TABLE agent_runs ADD COLUMN guided_session_id UUID;
		END IF;
	END $$;
	`
	if _, err := db.Exec(guidedSQL); err != nil {
		return fmt.Errorf("failed to add agent run guided_session_id column: %w", err)
	}

	// Per-agent reasoning effort (added with the agent-settings effort field).
	effortSQL := `
	DO $$
	BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name='agents' AND column_name='effort'
		) THEN
			ALTER TABLE agents ADD COLUMN effort VARCHAR(16) NOT NULL DEFAULT '';
		END IF;
	END $$;
	`
	if _, err := db.Exec(effortSQL); err != nil {
		return fmt.Errorf("failed to add agent effort column: %w", err)
	}

	return nil
}
