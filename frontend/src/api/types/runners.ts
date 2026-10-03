// The types api/runners.ts sends and receives.
// Runners: worker keys, the member's own runner key, cloud runner
// sessions, worker status, hosted runners and connector pairing.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

export interface WorkerKey {
  id: string;
  org_id: string;
  name: string;
  revoked: boolean;
  last_used_at: string | null;
  created_at: string;
  // Personal runner keys are bound to a single user; workspace keys have no user.
  user_id?: string | null;
  user_name?: string;
}

// Live worker/queue status for the Runners UX (any member may read).
export interface WorkerStatusWorker {
  id: string;
  name: string;
  personal: boolean;
  hosted: boolean;
  user_name: string;
  online: boolean;
  revoked: boolean;
  last_used_at: string | null;
}

export interface WorkerStatusQueue {
  queued: number;
  oldest_queued_seconds: number;
  queued_repo_access: number;
}

export interface WorkerStatus {
  workers: WorkerStatusWorker[];
  queue: WorkerStatusQueue;
}

// Hosted runner container managed by the deployment (admin-controlled).
export interface HostedRunnerRecord {
  id: string;
  org_id: string;
  container_name: string;
  status: string;
  detail: string;
  created_at: string;
}

export interface HostedRunnerStatus {
  record: HostedRunnerRecord | null;
  enabled: boolean;
  container_state: string;
  online: boolean;
}

/** A transient runner lease: the member's cloud runner, while it lasts. */
export interface RunnerSession {
  id: string;
  org_id: string;
  user_id: string;
  node_id: string;
  status: 'starting' | 'active' | 'ending' | 'ended';
  started_at: string;
  expires_at: string;
  last_activity_at: string;
  end_reason?: string;
  idle_minutes: number;
  node_name?: string;
}

/** How busy the runner pool is right now. */
/**
 * How busy the shared pool is, as a band rather than a count.
 *
 * A member can hold one runner at a time, so the exact number free is not
 * something they can act on, and the deployment's capacity is not published
 * to every account. `unavailable` means this deployment has no pool at all,
 * which is a different thing from `red` (nodes exist, all taken).
 */
export type RunnerPoolStatus = 'green' | 'amber' | 'red' | 'unavailable';

export interface RunnerPoolLoad {
  status: RunnerPoolStatus;
}

export interface RunnerSessionPayload {
  enabled: boolean;
  session: RunnerSession | null;
  deadline?: string;
  seconds_remaining?: number;
  pool_load?: RunnerPoolLoad;
}

export interface ConnectorPairing {
  code: string;
  expires_at: string;
  api_url: string;
  deep_link: string;
  start_link: string;
  // Combined open-or-pair link: the connector starts with its existing
  // pairing for the workspace and only spends the code when unpaired.
  open_link?: string;
}
