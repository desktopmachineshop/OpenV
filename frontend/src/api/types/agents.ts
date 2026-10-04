// The types api/agents.ts sends and receives.
// Agents and their runs, automations, proposals, repository
// connections, AI provider settings and sign-ins, and crews.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

export interface AgentDef {
  id: string;
  slug: string;
  name: string;
  description: string;
  provider: string;
  model: string;
  effort: string;
  allowed_tools: string[];
  /** Pinned against seed adoption: releases never update this agent. */
  locked?: boolean;
  write_mode: 'proposal' | 'direct';
  repo_access: boolean;
  max_turns: number;
  timeout_seconds: number;
  system_prompt: string;
  file_path: string;
}

export interface AgentRun {
  id: string;
  org_id?: string;
  agent_id: string;
  agent_name?: string;
  agent_provider?: string;
  project_id?: string | null;
  automation_id?: string | null;
  team_id?: string | null;
  team_node_id?: string | null;
  parent_run_id?: string | null;
  work_item_id?: string | null;
  // Provenance: set when this run was re-enqueued from a terminal run via
  // the retry endpoint (no run-tree semantics, unlike parent_run_id).
  retried_from_run_id?: string | null;
  status: string;
  prompt: string;
  final_text: string;
  // The answer a still-running agent has written so far (whole text, not a
  // delta). Cleared when the run finishes and final_text takes over.
  partial_text?: string;
  error: string;
  // Structured failure taxonomy (issue #184): a class for a terminal failure
  // (provider_unavailable | auth | workspace | timeout | agent_error |
  // worker_error), empty for a succeeded or cancelled run. attempt_count /
  // max_attempts track the bounded auto-retry chain.
  error_class?: string;
  attempt_count?: number;
  max_attempts?: number;
  tokens_in: number;
  tokens_out: number;
  cost_usd?: number | null;
  // Reproducibility snapshot (issue #216): the agent identity captured at
  // launch — the definition's content hash, model, and reasoning effort — so a
  // finished run stays self-describing even after the agent is later edited.
  // Blank on runs launched before the feature existed.
  agent_content_hash?: string;
  agent_model?: string;
  agent_effort?: string;
  started_at?: string | null;
  finished_at?: string | null;
  created_at: string;
  worker_id?: string | null;
  // Grace window: queued runs prefer the launcher's personal runner until
  // hosted_after, when hosted/workspace runners may claim them.
  preferred_user_id?: string | null;
  hosted_after?: string | null;
}

export interface RunLogEntry {
  run_id: string;
  seq: number;
  kind: string;
  payload: Record<string, any>;
  created_at: string;
}

export interface Automation {
  id: string;
  name: string;
  agent_id?: string | null;
  team_id?: string | null;
  project_id?: string | null;
  kind: 'manual' | 'scheduled' | 'triggered';
  enabled: boolean;
  prompt_template: string;
  cron_expr: string;
  catch_up: boolean;
  next_run_at?: string | null;
  last_run_at?: string | null;
  event_type: string;
  event_filter: Record<string, any>;
  cooldown_seconds: number;
  max_runs_per_hour: number;
}

export interface Proposal {
  id: string;
  run_id: string;
  project_id: string;
  op: string;
  target_id?: string | null;
  payload: Record<string, any>;
  // Temporary reference token an agent attached to a create_artifact proposal
  // so a sibling create_link proposal in the same run can point at the
  // not-yet-created artifact via from_id/to_id (issue #235). Empty/absent for
  // proposals that mint no reference.
  ref?: string;
  status: string;
  review_note: string;
  created_at: string;
}

export interface RepoConnection {
  id: string;
  project_id: string;
  name: string;
  remote_url: string;
  default_branch: string;
  credential_strategy: string;
  // Where this repo lives on the calling user's machine — their runs check
  // out from here.
  my_local_path?: string;
}

// One selectable model for a provider. `id` is what the CLI/SDK receives.
export interface ProviderModel {
  id: string;
  label: string;
}

export interface ProviderSetting {
  id: string;
  provider: string;
  auth_mode: 'subscription-cli' | 'api-key';
  api_key_env: string;
  default_model: string;
  enabled: boolean;
  last_detected: Record<string, any>;
  // Server-derived: the built-in catalog plus anything a worker detected.
  // Read-only — it is ignored on upsert.
  available_models: ProviderModel[];
}

export interface Crew {
  id: string;
  name: string;
  description: string;
  project_id?: string | null;
  entry_node_id?: string | null;
  is_default: boolean;
}

// Note: JSON field names (team_id etc.) are unchanged on the wire — only the
// UI/API naming moved from "team" to "crew".
export interface CrewNode {
  id: string;
  team_id: string;
  agent_id: string | null;
  user_id?: string | null;
  node_type: 'agent' | 'human';
  label: string;
  department: string;
  position: Record<string, any>;
  user_name?: string;
  user_avatar_url?: string;
}

export interface CrewEdge {
  id: string;
  team_id: string;
  from_node_id: string;
  to_node_id: string;
  edge_type: 'delegates-to' | 'hands-off-to' | 'reviews';
  config: Record<string, any>;
}

export interface CrewGraph {
  team: Crew;
  nodes: CrewNode[];
  edges: CrewEdge[];
}

// Portable, org-independent crew document. Nodes reference agents by slug so
// the importing workspace resolves them to its own agents. See
// internal/domain/crewtemplates.
export interface PortableCrewNode {
  key: string;
  agent_slug: string;
  label: string;
  department?: string;
  position?: Record<string, any>;
}

export interface PortableCrewEdge {
  from: string;
  to: string;
  edge_type: string;
  config?: Record<string, any>;
}

export interface PortableCrew {
  kind: string;
  version: string;
  name: string;
  description?: string;
  entry_node_key?: string;
  nodes: PortableCrewNode[];
  edges: PortableCrewEdge[];
  exported_at?: string;
}

// Built-in crew preset served by GET /api/v1/crew-templates.
export interface CrewTemplate {
  key: string;
  name: string;
  description: string;
  is_builtin: boolean;
  crew: PortableCrew;
}

// Result of importing a crew: the created crew plus any non-fatal warnings
// (e.g. agent slugs missing in the target workspace).
export interface CrewImportResult {
  team: Crew;
  warnings?: string[];
}

export interface BulkProposalOutcome {
  id: string;
  ok: boolean;
  error?: string;
}

export interface ProviderLogin {
  id: string;
  provider: string;
  // 'workspace' runs on any shared worker; 'user' only on the requester's
  // personal runner (the credential lands on their own machine).
  target: 'workspace' | 'user';
  status: 'pending' | 'claimed' | 'url_ready' | 'awaiting_code' | 'completed' | 'failed' | 'cancelled';
  auth_url: string;
  detail: string;
  // What the worker is waiting for the member to paste back: a short code,
  // or the whole redirected address of a loopback flow. The worker knows
  // which from the flow it is driving; absent from workers older than the
  // field (and from steps that ask for no paste at all).
  paste_kind?: 'code' | 'url';
  created_at: string;
  updated_at: string;
}
