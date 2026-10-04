// Agents and their runs, automations, proposals, repository
// connections, AI provider settings and sign-ins, and crews.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

import { API_BASE_URL, client, downloadBlob } from './http';
import type {
  AgentDef,
  AgentRun,
  Automation,
  BulkProposalOutcome,
  Crew,
  CrewEdge,
  CrewGraph,
  CrewImportResult,
  CrewNode,
  CrewTemplate,
  PortableCrew,
  Proposal,
  ProviderLogin,
  ProviderSetting,
  RepoConnection,
  RunLogEntry,
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
  launchRun: (slug: string, payload: { project_id?: string; prompt: string; work_item_id?: string }) =>
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
};

export const providerLoginsAPI = {
  start: (provider: string, target: 'workspace' | 'user' = 'workspace') =>
    client.post<ProviderLogin>('/api/v1/provider-logins', { provider, target }),
  get: (id: string) => client.get<ProviderLogin>(`/api/v1/provider-logins/${id}`),
  submitCode: (id: string, code: string) =>
    client.post<ProviderLogin>(`/api/v1/provider-logins/${id}/code`, { code }),
  cancel: (id: string) => client.post<ProviderLogin>(`/api/v1/provider-logins/${id}/cancel`),
};

export const crewsAPI = {
  list: (projectId?: string) =>
    client.get<Crew[]>('/api/v1/crews', { params: projectId ? { project_id: projectId } : {} }),
  get: (id: string) => client.get<CrewGraph>(`/api/v1/crews/${id}`),
  create: (payload: { name: string; description?: string; project_id?: string | null }) =>
    client.post<Crew>('/api/v1/crews', payload),
  update: (id: string, payload: { name?: string; description?: string; entry_node_id?: string }) =>
    client.put<Crew>(`/api/v1/crews/${id}`, payload),
  remove: (id: string) => client.delete(`/api/v1/crews/${id}`),
  clone: (id: string, name: string, projectId?: string | null) =>
    client.post<Crew>(`/api/v1/crews/${id}/clone`, { name, project_id: projectId }),
  addNode: (
    crewId: string,
    payload: {
      node_type: 'agent' | 'human';
      agent_id?: string;
      user_id?: string;
      label: string;
      department?: string;
      position?: Record<string, any>;
    }
  ) => client.post<CrewNode>(`/api/v1/crews/${crewId}/nodes`, payload),
  updateNode: (
    nodeId: string,
    payload: {
      label?: string;
      agent_id?: string;
      user_id?: string;
      department?: string;
      position?: Record<string, any>;
    }
  ) => client.put<CrewNode>(`/api/v1/crew-nodes/${nodeId}`, payload),
  removeNode: (nodeId: string) => client.delete(`/api/v1/crew-nodes/${nodeId}`),
  addEdge: (crewId: string, payload: { from_node_id: string; to_node_id: string; edge_type: string; config?: Record<string, any> }) =>
    client.post<CrewEdge>(`/api/v1/crews/${crewId}/edges`, payload),
  updateEdge: (edgeId: string, config: Record<string, any>) =>
    client.put<CrewEdge>(`/api/v1/crew-edges/${edgeId}`, { config }),
  removeEdge: (edgeId: string) => client.delete(`/api/v1/crew-edges/${edgeId}`),
  launchRun: (crewId: string, payload: { project_id?: string; prompt: string }) =>
    client.post<AgentRun>(`/api/v1/crews/${crewId}/runs`, payload),
  // Downloads the crew as a portable JSON document (agents referenced by slug).
  exportDownload: (id: string, name?: string) =>
    downloadBlob(`/api/v1/crews/${id}/export`, `${name || 'crew'}.crew.json`),
  // Creates a crew in the active workspace from a portable document. Pin it to
  // a project by passing projectId.
  import: (doc: PortableCrew, projectId?: string | null) =>
    client.post<CrewImportResult>(
      `/api/v1/crews/import${projectId ? `?project_id=${encodeURIComponent(projectId)}` : ''}`,
      doc
    ),
};

// Built-in crew presets (GET /api/v1/crew-templates). Import a chosen preset's
// `crew` through crewsAPI.import.
export const crewTemplatesAPI = {
  list: () => client.get<CrewTemplate[]>('/api/v1/crew-templates'),
};
