// Agents and their runs, automations, proposals, repository
// connections, and AI provider settings and sign-ins.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

import { API_BASE_URL, client } from './http';
import type {
  AgentDef,
  AgentPlacement,
  AgentRun,
  AgentStatsResponse,
  Automation,
  BulkProposalOutcome,
  Proposal,
  ProviderAPIKeys,
  ProviderLogin,
  ProviderSetting,
  ProviderTokenStatus,
  RepoConnection,
  RunLogEntry,
  StoredProviderAPIKey,
} from './types/agents';

export const agentsAPI = {
  list: () => client.get<AgentDef[]>('/api/v1/agents'),
  get: (slug: string) => client.get<AgentDef>(`/api/v1/agents/${slug}`),
  create: (definition: Partial<AgentDef>) => client.post<AgentDef>('/api/v1/agents', definition),
  update: (slug: string, definition: Partial<AgentDef>) =>
    client.put<AgentDef>(`/api/v1/agents/${slug}`, definition),
  remove: (slug: string) => client.delete(`/api/v1/agents/${slug}`),
  raw: (slug: string) => client.get<{ content: string }>(`/api/v1/agents/${slug}/raw`),
  saveRaw: (slug: string, content: string) =>
    client.put<AgentDef>(`/api/v1/agents/${slug}/raw`, { content }),
  sync: () => client.post<AgentDef[]>('/api/v1/agents/sync'),
  // Every agent's numbers over the last 7 days in a project, in one call.
  stats: (projectId: string) =>
    client.get<AgentStatsResponse>('/api/v1/agent-stats', { params: { project_id: projectId } }),
  // Places agents in the hierarchy; answers the workspace's agents.
  arrange: (agents: AgentPlacement[]) => client.put<AgentDef[]>('/api/v1/agent-arrangement', { agents }),
  /** `conversation` says a person is chatting with the agent rather than giving it a task, so the board makes no card of the run. */
  launchRun: (slug: string, payload: { project_id?: string; prompt: string; work_item_id?: string; conversation?: boolean }) =>
    client.post<AgentRun>(`/api/v1/agents/${slug}/runs`, payload),
  // Launch the seeded test-case-author agent to draft test cases (and verifies
  // links) for the given requirement artifacts, as proposals. The agent fetches
  // the requirement content itself via its OpenV tools — only the IDs travel.
  draftTestCases: (projectId: string, requirementIds: string[]) =>
    client.post<AgentRun>(`/api/v1/projects/${projectId}/draft-test-cases`, {
      requirement_ids: requirementIds,
    }),
};

export const agentRunsAPI = {
  // project: 'none' lists the runs with no project (the workspace Runs page).
  list: (params: {
    agent_id?: string;
    project_id?: string;
    project?: 'none';
    status?: string;
    parent_id?: string;
    limit?: number;
  }) => client.get<AgentRun[]>('/api/v1/agent-runs', { params }),
  get: (id: string) => client.get<AgentRun>(`/api/v1/agent-runs/${id}`),
  tree: (id: string) => client.get<AgentRun[]>(`/api/v1/agent-runs/${id}/tree`),
  logs: (id: string, afterSeq = 0) =>
    client.get<RunLogEntry[]>(`/api/v1/agent-runs/${id}/logs`, { params: { after_seq: afterSeq } }),
  streamUrl: (id: string, afterSeq = 0) =>
    `${API_BASE_URL}/api/v1/agent-runs/${id}/stream?after_seq=${afterSeq}`,
  cancel: (id: string) => client.post<AgentRun>(`/api/v1/agent-runs/${id}/cancel`),
  // Re-enqueue a terminal (failed/cancelled/timed_out) run as a NEW run with
  // the same agent/prompt/project, launched by the caller.
  retry: (id: string) => client.post<AgentRun>(`/api/v1/agent-runs/${id}/retry`),
};

export const automationsAPI = {
  list: (projectId?: string) =>
    client.get<Automation[]>('/api/v1/automations', { params: projectId ? { project_id: projectId } : {} }),
  get: (id: string) => client.get<Automation>(`/api/v1/automations/${id}`),
  create: (payload: Partial<Automation>) => client.post<Automation>('/api/v1/automations', payload),
  update: (id: string, payload: Partial<Automation>) =>
    client.put<Automation>(`/api/v1/automations/${id}`, payload),
  remove: (id: string) => client.delete(`/api/v1/automations/${id}`),
  runNow: (id: string) => client.post<AgentRun>(`/api/v1/automations/${id}/run-now`),
};

export const proposalsAPI = {
  list: (params: { project_id?: string; status?: string; run_id?: string }) =>
    client.get<Proposal[]>('/api/v1/proposals', { params }),
  approve: (id: string, note?: string) =>
    client.post<Proposal>(`/api/v1/proposals/${id}/approve`, { note: note || '' }),
  reject: (id: string, note?: string) =>
    client.post<Proposal>(`/api/v1/proposals/${id}/reject`, { note: note || '' }),
  bulkReview: (ids: string[], action: 'approve' | 'reject', note?: string) =>
    client.post<{ results: BulkProposalOutcome[] }>('/api/v1/proposals/bulk', {
      ids,
      action,
      note: note || '',
    }),
};

export const repoConnectionsAPI = {
  list: (projectId: string) =>
    client.get<RepoConnection[]>(`/api/v1/projects/${projectId}/repo-connections`),
  create: (projectId: string, payload: Partial<RepoConnection>) =>
    client.post<RepoConnection>(`/api/v1/projects/${projectId}/repo-connections`, payload),
  update: (id: string, payload: Partial<RepoConnection>) =>
    client.put<RepoConnection>(`/api/v1/repo-connections/${id}`, payload),
  remove: (id: string) => client.delete(`/api/v1/repo-connections/${id}`),
  // Set (or clear, with '') the caller's own local path for this connection.
  setMyPath: (id: string, localPath: string) =>
    client.put<RepoConnection>(`/api/v1/repo-connections/${id}/my-path`, { local_path: localPath }),
};

export const providerSettingsAPI = {
  list: () => client.get<ProviderSetting[]>('/api/v1/provider-settings'),
  upsert: (setting: Partial<ProviderSetting>) =>
    client.put<ProviderSetting>('/api/v1/provider-settings', setting),
  // The workspace's pasted API keys: never a value, only the last four
  // characters of each. Workspace admins only.
  listKeys: () => client.get<ProviderAPIKeys>('/api/v1/provider-api-keys'),
  setKey: (keyEnv: string, key: string) =>
    client.put<StoredProviderAPIKey>(`/api/v1/provider-api-keys/${keyEnv}`, { key }),
  removeKey: (keyEnv: string) => client.delete(`/api/v1/provider-api-keys/${keyEnv}`),
};

export const providerLoginsAPI = {
  start: (provider: string, target: 'workspace' | 'user' = 'workspace') =>
    client.post<ProviderLogin>('/api/v1/provider-logins', { provider, target }),
  // Asks the member's cloud runner to mint them a saved Claude token.
  startToken: () =>
    client.post<ProviderLogin>('/api/v1/provider-logins', {
      provider: 'claude-code',
      target: 'user',
      purpose: 'token',
    }),
  get: (id: string) => client.get<ProviderLogin>(`/api/v1/provider-logins/${id}`),
  submitCode: (id: string, code: string) =>
    client.post<ProviderLogin>(`/api/v1/provider-logins/${id}/code`, { code }),
  cancel: (id: string) => client.post<ProviderLogin>(`/api/v1/provider-logins/${id}/cancel`),
};

// A member's own stored subscription token (`claude setup-token`). The API
// answers its status only, never the token.
export const providerTokensAPI = {
  get: (provider: string) =>
    client.get<ProviderTokenStatus>(`/api/v1/me/provider-tokens/${provider}`),
  save: (provider: string, token: string) =>
    client.put<ProviderTokenStatus>(`/api/v1/me/provider-tokens/${provider}`, { token }),
  remove: (provider: string) => client.delete(`/api/v1/me/provider-tokens/${provider}`),
};
