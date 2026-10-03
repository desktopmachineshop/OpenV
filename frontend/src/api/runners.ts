// Runners: worker keys, the member's own runner key, cloud runner
// sessions, worker status, hosted runners and connector pairing.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

import { client } from './http';
import type {
  ConnectorPairing,
  HostedRunnerRecord,
  HostedRunnerStatus,
  RunnerSessionPayload,
  WorkerKey,
  WorkerStatus,
} from './types/runners';

export const workerKeysAPI = {
  list: (orgId: string) => client.get<WorkerKey[]>(`/api/v1/orgs/${orgId}/worker-keys`),
  create: (orgId: string, name: string) =>
    client.post<{ key_record: WorkerKey; key: string }>(`/api/v1/orgs/${orgId}/worker-keys`, { name }),
  revoke: (orgId: string, keyId: string) =>
    client.delete(`/api/v1/orgs/${orgId}/worker-keys/${keyId}`),
};

// Personal runner key self-service — any member manages their own key.
export const myRunnerKeyAPI = {
  get: (orgId: string) =>
    client.get<{ key_record: WorkerKey | null; online: boolean }>(
      `/api/v1/orgs/${orgId}/my-runner-key`
    ),
  create: (orgId: string) =>
    client.post<{ key_record: WorkerKey; key: string }>(`/api/v1/orgs/${orgId}/my-runner-key`),
  revoke: (orgId: string) => client.delete(`/api/v1/orgs/${orgId}/my-runner-key`),
};

/**
 * Transient runners: lease a pre-warmed cloud runner for a while instead of
 * installing the connector. The lease ends on its own (hard expiry or idle),
 * and the runner is wiped when it does — so the next one starts with a fresh
 * agent sign-in.
 */
export const cloudRunnerAPI = {
  get: (orgId: string) =>
    client.get<RunnerSessionPayload>(`/api/v1/orgs/${orgId}/runner-session`),
  start: (orgId: string) =>
    client.post<RunnerSessionPayload>(`/api/v1/orgs/${orgId}/runner-session`),
  extend: (orgId: string) =>
    client.post<RunnerSessionPayload>(`/api/v1/orgs/${orgId}/runner-session/extend`),
  end: (orgId: string) =>
    client.delete<RunnerSessionPayload>(`/api/v1/orgs/${orgId}/runner-session`),
};

export const workerStatusAPI = {
  get: (orgId: string) => client.get<WorkerStatus>(`/api/v1/orgs/${orgId}/worker-status`),
};

export const hostedRunnerAPI = {
  get: (orgId: string) => client.get<HostedRunnerStatus>(`/api/v1/orgs/${orgId}/hosted-runner`),
  enable: (orgId: string, providerKeys: Record<string, string>) =>
    client.post<HostedRunnerRecord>(`/api/v1/orgs/${orgId}/hosted-runner`, {
      provider_keys: providerKeys,
    }),
  stop: (orgId: string) => client.post(`/api/v1/orgs/${orgId}/hosted-runner/stop`),
  start: (orgId: string) => client.post(`/api/v1/orgs/${orgId}/hosted-runner/start`),
  remove: (orgId: string, purge: boolean) =>
    client.delete(`/api/v1/orgs/${orgId}/hosted-runner`, { params: { purge } }),
};

export const connectorAPI = {
  createPairing: (orgId: string) =>
    client.post<ConnectorPairing>(`/api/v1/orgs/${orgId}/connector-pairing`),
  downloadURL: (os: string) =>
    `${client.defaults.baseURL || ''}/api/v1/public/connector/download?os=${os}`,
  // Preflight so the UI can show an inline message instead of navigating to a
  // 404 page when the dist bundles haven't been built on this deployment.
  downloadAvailable: async (os: string): Promise<boolean> => {
    try {
      await client.head(`/api/v1/public/connector/download?os=${os}`);
      return true;
    } catch {
      return false;
    }
  },
  // Include the workspace so a connector paired with several workspaces
  // starts against the one the browser is in.
  startLink: (orgId?: string) => `openv-connector://start${orgId ? `?org=${orgId}` : ''}`,
};
