package postgres

import (
	"database/sql"
	"fmt"
)

// InitOrgSchema creates the multi-tenancy tables and adds org scoping
// columns across the platform. Must run AFTER the user/suite/agent schemas
// since it ALTERs their tables. All statements are idempotent; NOT NULL
// promotion of org_id columns happens in PromoteOrgColumns after the Go
// backfill has run.
func InitOrgSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS organizations (
		id UUID PRIMARY KEY,
		name VARCHAR(255) NOT NULL,
		slug VARCHAR(128) NOT NULL,
		org_type VARCHAR(16) NOT NULL DEFAULT 'company',
		plan VARCHAR(64) NOT NULL DEFAULT 'free',
		limits JSONB NOT NULL DEFAULT '{}',
		settings JSONB NOT NULL DEFAULT '{}',
		created_by UUID,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_organizations_slug ON organizations(slug);

	CREATE TABLE IF NOT EXISTS org_members (
		org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		role VARCHAR(16) NOT NULL DEFAULT 'member',
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		PRIMARY KEY (org_id, user_id)
	);

	CREATE INDEX IF NOT EXISTS idx_org_members_user ON org_members(user_id);

	CREATE TABLE IF NOT EXISTS org_teams (
		id UUID PRIMARY KEY,
		org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
		name VARCHAR(255) NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		created_by UUID,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_org_teams_org ON org_teams(org_id);

	CREATE TABLE IF NOT EXISTS org_team_members (
		org_team_id UUID NOT NULL REFERENCES org_teams(id) ON DELETE CASCADE,
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		PRIMARY KEY (org_team_id, user_id)
	);

	CREATE INDEX IF NOT EXISTS idx_org_team_members_user ON org_team_members(user_id);

	CREATE TABLE IF NOT EXISTS project_team_access (
		project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
		org_team_id UUID NOT NULL REFERENCES org_teams(id) ON DELETE CASCADE,
		role VARCHAR(32) NOT NULL DEFAULT 'viewer',
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		PRIMARY KEY (project_id, org_team_id)
	);

	CREATE TABLE IF NOT EXISTS worker_keys (
		id UUID PRIMARY KEY,
		org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
		name VARCHAR(255) NOT NULL,
		key_hash VARCHAR(128) NOT NULL,
		created_by UUID,
		revoked BOOLEAN NOT NULL DEFAULT FALSE,
		last_used_at TIMESTAMP,
		created_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_worker_keys_hash ON worker_keys(key_hash);
	CREATE INDEX IF NOT EXISTS idx_worker_keys_org ON worker_keys(org_id);

	-- Connector pairing codes: short-lived one-time codes the browser hands
	-- to the local Agent Connector, which exchanges them for the member's
	-- personal runner key.
	CREATE TABLE IF NOT EXISTS connector_pairings (
		id UUID PRIMARY KEY,
		org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		code_hash VARCHAR(128) NOT NULL,
		expires_at TIMESTAMP NOT NULL,
		used BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_connector_pairings_code ON connector_pairings(code_hash);

	-- Hosted runners (phase 3): at most one platform-managed runner container
	-- per org. Provider API keys are injected into the container environment
	-- at provision time and never stored here.
	CREATE TABLE IF NOT EXISTS hosted_workers (
		id UUID PRIMARY KEY,
		org_id UUID NOT NULL UNIQUE REFERENCES organizations(id) ON DELETE CASCADE,
		container_name VARCHAR(255) NOT NULL,
		worker_key_id UUID,
		status VARCHAR(32) NOT NULL DEFAULT 'provisioning', -- provisioning|running|stopped|error
		detail TEXT NOT NULL DEFAULT '',
		created_by UUID,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	-- Transient runners. A pool node is a pre-warmed always-on worker
	-- process with no workspace identity of its own; a runner session leases
	-- one to a single member for a bounded stretch of time, after which the
	-- node wipes the member's CLI credentials and returns to the pool.
	CREATE TABLE IF NOT EXISTS runner_pool_nodes (
		id UUID PRIMARY KEY,
		pool VARCHAR(64) NOT NULL DEFAULT 'default',
		name VARCHAR(255) NOT NULL,
		providers TEXT NOT NULL DEFAULT '',
		status VARCHAR(32) NOT NULL DEFAULT 'idle', -- idle|leased|draining|offline
		session_id UUID,
		last_seen_at TIMESTAMP NOT NULL DEFAULT NOW(),
		created_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_runner_pool_nodes_name ON runner_pool_nodes(pool, name);
	CREATE INDEX IF NOT EXISTS idx_runner_pool_nodes_free ON runner_pool_nodes(pool, status, last_seen_at);

	CREATE TABLE IF NOT EXISTS runner_sessions (
		id UUID PRIMARY KEY,
		org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		node_id UUID,
		worker_key_id UUID,
		status VARCHAR(32) NOT NULL DEFAULT 'starting', -- starting|active|ending|ended
		started_at TIMESTAMP NOT NULL DEFAULT NOW(),
		expires_at TIMESTAMP NOT NULL,
		last_activity_at TIMESTAMP NOT NULL DEFAULT NOW(),
		ended_at TIMESTAMP,
		end_reason VARCHAR(32) NOT NULL DEFAULT '',
		idle_minutes INT NOT NULL DEFAULT 15
	);

	CREATE INDEX IF NOT EXISTS idx_runner_sessions_live ON runner_sessions(org_id, user_id, status);
	CREATE INDEX IF NOT EXISTS idx_runner_sessions_sweep ON runner_sessions(status, expires_at);
	CREATE INDEX IF NOT EXISTS idx_runner_sessions_key ON runner_sessions(worker_key_id);
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create org schema: %w", err)
	}

	// org_id columns (nullable until PromoteOrgColumns) + session context +
	// crew human-node columns.
	alterSQL := `
	DO $$
	BEGIN
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='projects' AND column_name='org_id') THEN
			ALTER TABLE projects ADD COLUMN org_id UUID;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='sessions' AND column_name='active_org_id') THEN
			ALTER TABLE sessions ADD COLUMN active_org_id UUID;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='agents' AND column_name='org_id') THEN
			ALTER TABLE agents ADD COLUMN org_id UUID;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='agent_teams' AND column_name='org_id') THEN
			ALTER TABLE agent_teams ADD COLUMN org_id UUID;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='automations' AND column_name='org_id') THEN
			ALTER TABLE automations ADD COLUMN org_id UUID;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='agent_runs' AND column_name='org_id') THEN
			ALTER TABLE agent_runs ADD COLUMN org_id UUID;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='guided_sessions' AND column_name='org_id') THEN
			ALTER TABLE guided_sessions ADD COLUMN org_id UUID;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='domain_events' AND column_name='org_id') THEN
			ALTER TABLE domain_events ADD COLUMN org_id UUID;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='provider_settings' AND column_name='org_id') THEN
			ALTER TABLE provider_settings ADD COLUMN org_id UUID;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='provider_logins' AND column_name='org_id') THEN
			ALTER TABLE provider_logins ADD COLUMN org_id UUID;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='templates' AND column_name='org_id') THEN
			ALTER TABLE templates ADD COLUMN org_id UUID; -- NULL = global built-in
		END IF;

		-- Personal runner keys (phase 3): user_id NULL = workspace key.
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='worker_keys' AND column_name='user_id') THEN
			ALTER TABLE worker_keys ADD COLUMN user_id UUID REFERENCES users(id) ON DELETE CASCADE;
		END IF;

		-- Transient runner session keys: personal keys that live and die with
		-- one lease, and never displace a member's connector key.
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='worker_keys' AND column_name='session_id') THEN
			ALTER TABLE worker_keys ADD COLUMN session_id UUID;
		END IF;

		-- Claim routing (phase 3): first refusal for the launcher's personal
		-- runner, hosted takes over after hosted_after.
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='agent_runs' AND column_name='preferred_user_id') THEN
			ALTER TABLE agent_runs ADD COLUMN preferred_user_id UUID;
			ALTER TABLE agent_runs ADD COLUMN hosted_after TIMESTAMP;
		END IF;

		-- Crew human nodes.
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='agent_team_nodes' AND column_name='node_type') THEN
			ALTER TABLE agent_team_nodes ALTER COLUMN agent_id DROP NOT NULL;
			ALTER TABLE agent_team_nodes ADD COLUMN node_type VARCHAR(16) NOT NULL DEFAULT 'agent';
			ALTER TABLE agent_team_nodes ADD COLUMN user_id UUID REFERENCES users(id) ON DELETE CASCADE;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_crew_node_identity') THEN
			ALTER TABLE agent_team_nodes ADD CONSTRAINT chk_crew_node_identity CHECK (
				(node_type = 'agent' AND agent_id IS NOT NULL AND user_id IS NULL) OR
				(node_type = 'human' AND user_id IS NOT NULL AND agent_id IS NULL)
			);
		END IF;
	END $$;
	`

	if _, err := db.Exec(alterSQL); err != nil {
		return fmt.Errorf("failed to add org columns: %w", err)
	}

	// Per-org uniqueness replaces global uniqueness.
	uniqSQL := []string{
		`DROP INDEX IF EXISTS idx_agents_slug;`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_agents_org_slug ON agents(org_id, slug);`,
		`DROP INDEX IF EXISTS idx_provider_settings_provider;`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_provider_settings_org_provider ON provider_settings(org_id, provider);`,
		`CREATE INDEX IF NOT EXISTS idx_agent_runs_org ON agent_runs(org_id, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_domain_events_org ON domain_events(org_id, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_projects_org ON projects(org_id);`,
		`CREATE INDEX IF NOT EXISTS idx_provider_logins_org ON provider_logins(org_id, status, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_worker_keys_user ON worker_keys(org_id, user_id);`,
	}
	for _, stmt := range uniqSQL {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("failed to adjust org indexes: %w", err)
		}
	}

	return nil
}

// PromoteOrgColumns sets org_id columns NOT NULL once the backfill has left
// no NULLs behind. Safe to call repeatedly; each promotion is guarded.
func PromoteOrgColumns(db *sql.DB) error {
	targets := []string{
		"projects", "agents", "agent_teams", "automations", "agent_runs",
		"guided_sessions", "domain_events", "provider_settings", "provider_logins",
	}
	for _, table := range targets {
		stmt := fmt.Sprintf(`
		DO $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM %s WHERE org_id IS NULL)
			AND EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name='%s' AND column_name='org_id' AND is_nullable='YES'
			) THEN
				ALTER TABLE %s ALTER COLUMN org_id SET NOT NULL;
			END IF;
		END $$;
		`, table, table, table)
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("failed to promote org_id on %s: %w", table, err)
		}
	}
	return nil
}
