# Data Model Overview

The schema is owned by `internal/persistence/postgres/`. At API startup
`cmd/server/main.go` calls `postgres.MigrateAndBackfill` (`migrations.go`),
which runs `postgres.Migrate` — bringing the database to the current version
through a numbered migration ledger (see "Schema migrations" below) — and
then the idempotent org backfill (`BackfillOrgs`). Migration 0001 — the frozen
"baseline" — wraps the legacy idempotent init chain and is re-run on every
boot: `db.go` (core `InitSchema`), `schema_users.go`, `schema_suite.go`,
`schema_agents.go`, and `schema_orgs.go` (multi-tenancy, which also
`ALTER`s earlier tables). `BackfillOrgs` (which finishes by calling
`PromoteOrgColumns`) stays outside the ledger as a boot-time idempotent
data migration: it backfills org ids, then promotes the `org_id` columns
to NOT NULL. This overview is generated from those files as of commit
`200cf4f`; the code is authoritative.

## Schema migrations (`migrations.go`)

### schema_migrations
The migration ledger: `version` (INT primary key), `name`, `applied_at`.
`postgres.Migrate` creates this table if missing, re-runs the idempotent
0001 baseline (recording it in the ledger once), then applies any
unapplied numbered migrations (0002+) in ascending order. Each numbered
migration runs exactly once: its DDL and its ledger row commit in the same
transaction (so a failed migration leaves neither behind), and concurrent
booting replicas are serialized by a `pg_advisory_xact_lock`. The baseline
is frozen — new schema changes are appended to the registry in
`migrations.go` as numbered migrations, never added to `InitSchema` or the
`schema_*.go` files.

## Core requirements data (`db.go`)

### projects
`id`, `org_id` (owning workspace), `name`, `description`,
`agent_auth` (`user-account` | `api-key` — how agent runs authenticate to
providers), timestamps.

### artifacts
Temporally versioned: **primary key `(id, version)`**; each update inserts a
new row and closes the old one (`valid_to`). Current version = `valid_to IS
NULL` (unique partial index). Columns: `project_id`, `parent_id` (hierarchy),
`type`, `title`, `body`, `sort_order`, `attributes` JSONB, `version`,
`valid_from`/`valid_to`, `created_by`, timestamps. Types and link types are
extensible catalogs served at `/api/v1/meta/*`.

### links
Traceability edges (`from_id` → `to_id`, `type`, `attributes` JSONB). Links
are temporally versioned like artifacts (`valid_from`/`valid_to` with a
unique active-row index).

### link_artifacts
Maps each link to the specific artifact **versions** it was created against
(`link_id`, `artifact_id`, `artifact_version`, `active`) so history and
baselines can reconstruct exact link states.

### attachments
Uploaded files: `artifact_id` (nullable) **or** `test_result_id` (test
evidence), `filename`, `mime_type`, `file_path` (under `UPLOADS_DIR`),
`file_size`.

### chatter
Per-artifact activity feed: `artifact_id`, `message`, `is_auto_entry`
(system-generated change summaries), `entry_type`, `created_by`,
`author_name`.

### baselines
Implemented (not "future"): `project_id`, `name`, full project `snapshot`
JSONB, `created_by`.

### templates
Project templates: `template_key` (unique; built-ins), `name`, `snapshot`
JSONB, `is_default`, `org_id` (NULL = global built-in).

## Users, sessions, membership (`schema_users.go`)

### users
`email` (unique, case-insensitive), `name`, `avatar_url`, `auth_provider`
(`password` | `google`), `password_hash`, `is_admin` (the first registered
user), timestamps.

### sessions
Cookie sessions: `user_id`, `token_hash` (unique), `expires_at`,
`last_seen_at`, plus `active_org_id` (the stored active workspace).

### project_members
`(project_id, user_id)` → `role` (`owner` | `editor` | `viewer`). A user's
effective project role is the max of this and any people-team grant.

## Multi-tenancy (`schema_orgs.go`)

### organizations
Workspaces: `name`, `slug` (unique), `org_type` (`company` | `personal` —
personal orgs are auto-created at signup), `plan` and `limits` JSONB
(`limits.runner_grace_seconds` tunes run routing;
`limits.runner_memory_mb` / `limits.runner_cpus` cap the hosted runner
container, falling back to the plan's defaults — `orgs.PlanDefaults` — when
unset; there is no API for editing `limits` yet, operators set keys directly
in the database), `created_by`.

### org_members
`(org_id, user_id)` → `role` (`admin` | `member`). Org admins act as owners
of every project in the org.

### org_teams / org_team_members
People-teams within a workspace (these are *human* teams; agent graphs are
"crews", below): team `name`/`description`, and its user membership.

### project_team_access
`(project_id, org_team_id)` → `role`. Grants a whole people-team a project
role; combined with direct membership by taking the highest role.

### worker_keys
Org-scoped runner credentials: `org_id`, `name`, `key_hash` (unique),
`user_id` (NULL = shared workspace key; set = a member's **personal runner
key**, one active per member), `revoked`, `last_used_at`. The legacy
`WORKER_API_KEY` env value is registered as a workspace key for the
bootstrap org at startup.

### connector_pairings
One-time Agent Connector pairing codes: `org_id`, `user_id`, `code_hash`
(unique), `expires_at` (10 min), `used`.

### hosted_workers
At most one platform-managed runner container per org (`org_id` unique):
`container_name`, `worker_key_id`, `status`
(`provisioning|running|stopped|error`), `detail`. Provider API keys are
injected into the container environment at provision time and never stored.

`schema_orgs.go` also adds `org_id` to `projects`, `agents`, `agent_teams`,
`automations`, `agent_runs`, `guided_sessions`, `domain_events`,
`provider_settings`, `provider_logins`, and `templates`, then promotes the
columns to NOT NULL once the backfill leaves no NULLs.

## Agent suite (`schema_agents.go`)

### agents
Registry mirroring the file-backed agent definitions
(`$AGENTS_DIR/<org-id>/<slug>.md` is the source of truth): `org_id` + `slug`
(unique per org), `name`, `description`, `provider`, `model`, `effort`
(`low|medium|high|xhigh|max`, empty = provider default), `allowed_tools`
JSONB, `write_mode` (`proposal` | `direct`), `repo_access`, `max_turns`,
`timeout_seconds`, `config` JSONB, `system_prompt`, `file_path`,
`content_hash`, `synced_at`.

### agent_runs
**This table is the job queue** (workers claim with
`FOR UPDATE SKIP LOCKED`). Scope/links: `agent_id`, `project_id`,
`automation_id`, `trigger_event_id`, `team_id`/`team_node_id` (crew),
`parent_run_id` (delegation tree), `work_item_id`, `interview_session_id`,
`guided_session_id`. Lifecycle: `status` (`queued` → `claimed` → `running` →
`succeeded`/`failed`/`cancelled`/`timed_out`, plus `awaiting_approval`),
`cancel_requested`, `priority` (child/interview turns jump the
queue), `run_token_hash` (the run's own API credential), `worker_id`,
`heartbeat_at` (stale runs are reaped), `started_at`/`finished_at`,
`exit_code`, `final_text`, `error`, `tokens_in`/`tokens_out`, `cost_usd`,
`artifacts_touched` JSONB, `launched_by`. Routing: `preferred_user_id` +
`hosted_after` give the launcher's personal runner first refusal before the
hosted/workspace pool takes over.

### agent_run_logs
Append-only run output: `(run_id, seq)` unique, `kind`, `payload` JSONB.
Feeds the SSE live stream.

### agent_proposals
Writes diverted from `write_mode: proposal` runs: `run_id`, `project_id`,
`op`, `target_id`, `payload` JSONB, `status` (pending/approved/rejected),
`review_note`, `applied_entity_id`, `reviewed_by`/`reviewed_at`.

### automations
Unattended launch rules: `agent_id` or `team_id`, optional `project_id`,
`kind` (`manual` | `scheduled` | `triggered`), `enabled`, `prompt_template`,
cron fields (`cron_expr`, `catch_up`, `next_run_at`, `last_run_at`), event
fields (`event_type`, `event_filter` JSONB), rate limits
(`cooldown_seconds`, `max_runs_per_hour`).

### domain_events
The event bus's persistent log: `event_type`, `project_id`, `entity_id`,
`actor` (`user:<id>` | `agent:<run>` | `worker:<org>` | `system`), `payload`
JSONB, `org_id`. Triggered automations match against this stream.

### repo_connections / user_repo_paths
A repo connection identifies a repository per project (`name`, `remote_url`,
`default_branch`, `credential_strategy`). Checkout locations are
**per-member**: `user_repo_paths` maps `(user_id, repo_connection_id)` →
`local_path` (a legacy shared `local_path` column was migrated into this
table and dropped).

### provider_settings / provider_logins
Per-org AI provider config: `provider`, `auth_mode`, `api_key_env`,
`default_model`, `enabled`, `last_detected` JSONB (worker detection reports,
including discovered models that are merged ahead of the built-in model
catalog). `provider_logins` relays CLI sign-in flows to runners: `provider`,
`target` (`workspace` | `user`), `status`, `auth_url`, `code`, `detail`,
`requested_by`.

### agent_teams / agent_team_nodes / agent_team_edges (crews)
Agent org charts (API name: **crews**; table names keep the historical
`agent_team*` prefix). `agent_teams`: `org_id`, `name`, optional
`project_id` (pinned), `entry_node_id`, `is_default`. Nodes: `node_type`
(`agent` | `human`) with a CHECK constraint tying `agent_id` xor `user_id`
to the type, `label`, `department`, `position` JSONB. Edges: `edge_type`
(`delegates-to` | `hands-off-to` | `reviews`) with per-graph validation
(delegation subgraph acyclic; no delegates-to humans).

## Product suite (`schema_suite.go`)

### product_profiles
One row per project: `vision`, `problem_statement`, `target_users`,
`constraints` and `success_metrics` JSONB, `settings` JSONB.

### guided_sessions / guided_session_messages
Guided wizard state: `project_id`, `org_id`, `status`, `current_step`,
`answers` JSONB, `draft_artifact_ids` JSONB, `agent_run_id`, `created_by`.
Messages are the copilot chat transcript (`session_id`, `role`, `content`);
copilot turns run as agent runs linked via `agent_runs.guided_session_id`.

### test_runs / test_results
Test execution: runs (`project_id`, `name`, optional `baseline_id`,
`status`, `started_at`/`completed_at`) and one result per
`(run_id, test_case_id)`: `test_case_version`, `status`, `notes`, `evidence`
JSONB, `executed_at`/`executed_by`, `executed_by_agent_run_id` (set when an
agent run recorded the result, so reviewers can tell agent evidence from
human evidence). Test cases carry an `execution_method` attribute
(`automated` — the default — | `manual` | `physical`); only automated cases
may have agent-recorded results (`internal/domain/vv`). Evidence files
attach via `attachments.test_result_id`.

### work_items / work_item_activity
Kanban cards: `project_id`, `title`, `description`, `board_column`,
`sort_order`, `assignee_type` (`user` | `agent`) + `assignee_id`,
`agent_run_id` (moving a card into an agent column enqueues a run),
`artifact_ids` JSONB, `due_date`. Activity is the card's feed (`kind`,
`actor`, `content`, `payload`).

### interviews / interview_invites / interview_sessions / interview_messages
Stakeholder elicitation: an interview (`project_id`, optional
`guided_session_id` and `persona_artifact_id`, `name`, `brief`, `agent_id`,
`status`, `expires_at`) has token-authenticated invites (`token_hash`
unique, `invitee_label`, `expires_at`, `revoked`), participant sessions
(`invite_id`, `participant_name`, `status`, `summary`), and per-session
chat messages. Sessions are listed per interview, and also project-wide
(most recent across all of a project's interviews, joined through
`interview_sessions.interview_id`) via
`GET /api/v1/projects/{id}/interview-sessions?limit=N`.

## Versioning strategy

- **Artifacts and links** use temporal versioning: every update inserts a
  new row (`version` incremented) and stamps the old row's `valid_to`.
  Current state is the `valid_to IS NULL` row; full history is retained for
  audit and baseline diffs, and old artifact versions can be restored.
- `link_artifacts` pins links to artifact versions so a baseline snapshot
  reproduces exactly what was linked to what, at which version.

## Extensibility

Artifacts and links carry an `attributes` JSONB column for custom
key-value data (priority, status, tags, `origin: interview`,
`status: draft` for agent-drafted content, …). Artifact and link *types* are
data, not schema — see `/api/v1/meta/artifact-types`,
`/api/v1/meta/link-types`, and `docs/link-type-rules.md`.
