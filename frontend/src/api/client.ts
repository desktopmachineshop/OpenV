import axios, { AxiosInstance } from 'axios';

// Determine API base URL
// Priority: env var > browser detection > default fallback
const getAPIBaseURL = (): string => {
  // Check for environment variable set at build time (Railway Variables)
  if (process.env.REACT_APP_API_URL) {
    console.log('Using REACT_APP_API_URL:', process.env.REACT_APP_API_URL);
    return process.env.REACT_APP_API_URL;
  }

  // Runtime detection for local development
  if (typeof window !== 'undefined' && window.location) {
    const protocol = window.location.protocol; // http: or https:
    const hostname = window.location.hostname; // localhost or IP
    
    // Local development: use port 8080
    const apiUrl = `${protocol}//${hostname}:8080`;
    console.log('Detected local API URL:', apiUrl);
    return apiUrl;
  }

  // Default fallback
  return 'http://localhost:8080';
};

const API_BASE_URL = getAPIBaseURL();

console.log('API_BASE_URL:', API_BASE_URL);

const client: AxiosInstance = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
  timeout: 60000, // 60 second timeout for complex operations like template import
  withCredentials: true, // session cookie auth
});

// Attach the active workspace (org) to every request. The backend validates
// membership and falls back server-side when the header is invalid.
client.interceptors.request.use((config) => {
  try {
    const activeOrg =
      sessionStorage.getItem('openv_active_org') ||
      localStorage.getItem('openv_active_org') ||
      '';
    if (activeOrg) {
      config.headers['X-Org-ID'] = activeOrg;
    }
  } catch {
    // storage unavailable (private mode etc.) — proceed without the header
  }
  return config;
});

// Add response interceptor for better error handling + auth redirects
client.interceptors.response.use(
  (response) => response,
  (error) => {
    console.error('API Error:', {
      url: error.config?.url,
      method: error.config?.method,
      status: error.response?.status,
      data: error.response?.data,
      message: error.message,
    });
    // Redirect to login on 401, except on public pages and auth calls.
    if (
      error.response?.status === 401 &&
      typeof window !== 'undefined' &&
      !window.location.pathname.startsWith('/login') &&
      !window.location.pathname.startsWith('/interview/') &&
      !String(error.config?.url || '').includes('/api/v1/auth/')
    ) {
      window.location.href = '/login';
    }
    return Promise.reject(error);
  }
);

export interface Artifact {
  id: string;
  project_id: string;
  parent_id?: string | null;
  type: string;
  title: string;
  body: string;
  sort_order?: number;
  attributes: Record<string, any>;
  version: number;
  valid_from: string;
  valid_to: string | null;
  created_at: string;
  updated_at: string;
}

export interface Link {
  id: string;
  from_id: string;
  to_id: string;
  type: string;
  attributes: Record<string, any>;
  version: number;
  created_at: string;
  updated_at: string;
}

export interface Project {
  id: string;
  org_id: string;
  name: string;
  description: string;
  // How agent runs in this project authenticate with their AI provider:
  // 'user-account' (each member's local CLI sign-in) or 'api-key'
  // (workspace API key; overrides local sign-ins but still runs via the
  // member's OpenV connector/runner).
  agent_auth: 'user-account' | 'api-key';
  created_at: string;
  updated_at: string;
}

export interface Baseline {
  id: string;
  project_id: string;
  name: string;
  created_at: string;
}

export interface Template {
  id: string;
  key?: string;
  name: string;
  description: string;
  source?: string; // "database" or "file"
  is_default: boolean;
  created_at: string;
}

export interface ProjectExport {
  exported_at: string;
  version: string;
  project_id: string;
  project_name: string;
  project_description: string;
  artifacts: Artifact[];
  links: Link[];
  attachments: Attachment[];
}

export interface Attachment {
  id: string;
  artifact_id: string;
  filename: string;
  mime_type: string;
  file_path: string;
  file_size: number;
  created_at: string;
}

export const artifactAPI = {
  create: (payload: Partial<Artifact>) => 
    client.post<Artifact>('/api/v1/artifacts', payload),
  get: (id: string) => 
    client.get<Artifact>(`/api/v1/artifacts/${id}`),
  list: (projectId: string, type?: string) => 
    client.get<Artifact[]>('/api/v1/artifacts', { params: { project_id: projectId, type } }),
  update: (id: string, payload: Partial<Artifact>) =>
    client.put<Artifact>(`/api/v1/artifacts/${id}`, payload),
  delete: (id: string) =>
    client.delete(`/api/v1/artifacts/${id}`),
  getVersions: (id: string) =>
    client.get<Artifact[]>(`/api/v1/artifacts/${id}/versions`),
  restoreVersion: (id: string, version: number) =>
    client.post<Artifact>(`/api/v1/artifacts/${id}/restore`, { version }),
};

export const linkAPI = {
  create: (payload: Partial<Link>) =>
    client.post<Link>('/api/v1/links', payload),
  get: (id: string) =>
    client.get<Link>(`/api/v1/links/${id}`),
  list: (projectId: string) =>
    client.get<Link[]>('/api/v1/links', { params: { project_id: projectId } }),
  listForArtifactVersion: (artifactId: string, version: number) =>
    client.get<Link[]>(`/api/v1/artifacts/${artifactId}/links`, { params: { version } }),
  update: (id: string, payload: Partial<Link>) =>
    client.put<Link>(`/api/v1/links/${id}`, payload),
  delete: (id: string) =>
    client.delete(`/api/v1/links/${id}`),
};

export const projectAPI = {
  create: (payload: Partial<Project>) =>
    client.post<Project>('/api/v1/projects', payload),
  get: (id: string) =>
    client.get<Project>(`/api/v1/projects/${id}`),
  list: () =>
    client.get<Project[]>('/api/v1/projects'),
  update: (id: string, payload: Partial<Project>) =>
    client.put<Project>(`/api/v1/projects/${id}`, payload),
  delete: (id: string) =>
    client.delete(`/api/v1/projects/${id}`),
  export: async (id: string, format: string = 'json') => {
    const response = await client.get(`/api/v1/projects/${id}/export?format=${format}`, {
      responseType: 'blob',
    });
    
    // Extract filename from Content-Disposition header or use default
    const contentDisposition = response.headers['content-disposition'];
    let filename = `project_export_${new Date().toISOString().slice(0, 10)}.json`;
    
    if (contentDisposition) {
      const filenameMatch = contentDisposition.match(/filename=(.+)/);
      if (filenameMatch) {
        filename = filenameMatch[1].replace(/['"]/g, '');
      }
    }
    
    // Create a download link and trigger it
    const url = window.URL.createObjectURL(new Blob([response.data]));
    const link = document.createElement('a');
    link.href = url;
    link.setAttribute('download', filename);
    document.body.appendChild(link);
    link.click();
    link.remove();
    window.URL.revokeObjectURL(url);
    
    return response;
  },
  report: async (id: string, baselineId?: string) => {
    const params = baselineId && baselineId !== 'live' ? `?baseline_id=${baselineId}` : '';
    const response = await client.get(`/api/v1/projects/${id}/report${params}`, {
      responseType: 'blob',
    });

    const contentDisposition = response.headers['content-disposition'];
    let filename = `project_report_${new Date().toISOString().slice(0, 10)}.pdf`;
    if (contentDisposition) {
      const filenameMatch = contentDisposition.match(/filename=(.+)/);
      if (filenameMatch) {
        filename = filenameMatch[1].replace(/['"]/g, '');
      }
    }

    const url = window.URL.createObjectURL(new Blob([response.data]));
    const link = document.createElement('a');
    link.href = url;
    link.setAttribute('download', filename);
    document.body.appendChild(link);
    link.click();
    link.remove();
    window.URL.revokeObjectURL(url);

    return response;
  },
  import: async (file: File) => {
    const fileContent = await file.text();
    const response = await client.post<{ status: string; message: string; project_id: string }>(
      '/api/v1/projects/import',
      fileContent,
      {
        headers: {
          'Content-Type': 'application/json',
        },
      }
    );
    return response;
  },
};

export const baselineAPI = {
  create: (projectId: string, name: string) =>
    client.post<Baseline>(`/api/v1/projects/${projectId}/baselines`, { name }),
  list: (projectId: string) =>
    client.get<Baseline[]>(`/api/v1/projects/${projectId}/baselines`),
  get: (baselineId: string) =>
    client.get<ProjectExport>(`/api/v1/baselines/${baselineId}`),
  delete: (baselineId: string) =>
    client.delete(`/api/v1/baselines/${baselineId}`),
};

export const templateAPI = {
  list: () =>
    client.get<Template[]>('/api/v1/templates'),
  create: (projectId: string, name: string, description: string) =>
    client.post<Template>('/api/v1/templates', { project_id: projectId, name, description }),
  createProject: (templateId: string, name: string, description: string) =>
    client.post<Project>(`/api/v1/templates/${templateId}/projects`, { name, description }),
};

export const attachmentAPI = {
  upload: (artifactId: string, file: File) => {
    const formData = new FormData();
    formData.append('artifact_id', artifactId);
    formData.append('file', file);
    return client.post<Attachment>('/api/v1/attachments/upload', formData, {
      headers: {
        'Content-Type': 'multipart/form-data',
      },
    });
  },
  getMeta: (id: string) =>
    client.get<Attachment>(`/api/v1/attachments/${id}`),
  getDownloadUrl: (id: string) =>
    `${API_BASE_URL}/api/v1/attachments/${id}/download`,
  delete: (id: string) =>
    client.delete(`/api/v1/attachments/${id}`),
  listByArtifact: (artifactId: string) =>
    client.get<Attachment[]>(`/api/v1/artifacts/${artifactId}/attachments`),
};

export interface ChatterEntry {
  id: string;
  artifact_id: string;
  message: string;
  is_auto_entry: boolean;
  entry_type: string;
  created_at: string;
  updated_at: string;
}

export const chatterAPI = {
  create: (payload: { artifact_id: string; message: string }) =>
    client.post<ChatterEntry>('/api/v1/chatter', payload),
  list: (artifactId: string) =>
    client.get<ChatterEntry[]>('/api/v1/chatter', { params: { artifact_id: artifactId } }),
};

// ---------------------------------------------------------------------------
// Multi-agent suite APIs
// ---------------------------------------------------------------------------

export interface User {
  id: string;
  email: string;
  name: string;
  avatar_url: string;
  auth_provider: string;
  is_admin: boolean;
  created_at: string;
}

export interface ProjectMember {
  project_id: string;
  user_id: string;
  role: 'owner' | 'editor' | 'viewer';
  user_name?: string;
  user_email?: string;
  avatar_url?: string;
}

export interface ArtifactTypeDef {
  value: string;
  label: string;
  description: string;
  color: string;
}

export interface LinkTypeRule {
  type: string;
  label: string;
  inverseLabel: string;
  allowedFromTypes: string[];
  allowedToTypes: string[];
  description: string;
}

export interface ProductProfile {
  project_id: string;
  vision: string;
  problem_statement: string;
  target_users: string;
  constraints: Record<string, any>[];
  success_metrics: Record<string, any>[];
  settings: Record<string, any>;
  updated_at: string;
}

export interface TestRun {
  id: string;
  project_id: string;
  name: string;
  description: string;
  baseline_id?: string | null;
  status: 'in-progress' | 'completed' | 'aborted';
  started_at: string;
  completed_at?: string | null;
}

export interface TestResult {
  id: string;
  run_id: string;
  test_case_id: string;
  test_case_version: number;
  status: 'pass' | 'fail' | 'blocked' | 'not-run';
  notes: string;
  evidence: string[];
  executed_at?: string | null;
}

export interface CoverageEntry {
  requirement_id: string;
  title: string;
  verification_method: string;
  verification_status: string;
  test_case_ids: string[];
  latest_results: Record<string, string>;
  rollup: string;
}

export interface CoverageReport {
  project_id: string;
  entries: CoverageEntry[];
  summary: Record<string, number>;
}

export interface MatrixRow {
  requirement_id: string;
  title: string;
  user_need_ids: string[];
  design_ids: string[];
  test_case_ids: string[];
  latest_results: Record<string, string>;
  hazard_ids: string[];
}

export interface WorkItem {
  id: string;
  project_id: string;
  title: string;
  description: string;
  column: string;
  sort_order: number;
  assignee_type: 'user' | 'agent' | 'team';
  assignee_id?: string | null;
  agent_run_id?: string | null;
  artifact_ids: string[];
  due_date?: string | null;
  created_at: string;
  updated_at: string;
}

export interface WorkItemActivity {
  id: string;
  work_item_id: string;
  kind: string;
  actor: string;
  content: string;
  payload: Record<string, any>;
  created_at: string;
}

export interface GuidedSession {
  id: string;
  project_id: string;
  status: string;
  current_step: number;
  answers: Record<string, any>;
  draft_artifact_ids: string[];
  agent_run_id?: string | null;
}

export interface Interview {
  id: string;
  project_id: string;
  name: string;
  brief: string;
  status: string;
  created_at: string;
}

export interface InterviewSession {
  id: string;
  interview_id: string;
  participant_name: string;
  status: string;
  summary: string;
  started_at: string;
  ended_at?: string | null;
}

export interface InterviewMessage {
  id: string;
  session_id: string;
  role: 'assistant' | 'participant' | 'system';
  content: string;
  created_at: string;
}

export interface AgentDef {
  id: string;
  slug: string;
  name: string;
  description: string;
  provider: string;
  model: string;
  allowed_tools: string[];
  write_mode: 'proposal' | 'direct';
  repo_access: boolean;
  max_turns: number;
  timeout_seconds: number;
  system_prompt: string;
  file_path: string;
}

export interface AgentRun {
  id: string;
  agent_id: string;
  agent_name?: string;
  agent_provider?: string;
  project_id?: string | null;
  automation_id?: string | null;
  team_id?: string | null;
  team_node_id?: string | null;
  parent_run_id?: string | null;
  work_item_id?: string | null;
  status: string;
  prompt: string;
  final_text: string;
  error: string;
  tokens_in: number;
  tokens_out: number;
  cost_usd?: number | null;
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
  status: string;
  review_note: string;
  created_at: string;
}

export interface RepoConnection {
  id: string;
  project_id: string;
  name: string;
  remote_url: string;
  local_path: string;
  default_branch: string;
  credential_strategy: string;
  // The calling user's per-user override of local_path (runs on their own
  // machine use this checkout location).
  my_local_path?: string;
}

export interface ProviderSetting {
  id: string;
  provider: string;
  auth_mode: 'subscription-cli' | 'api-key';
  api_key_env: string;
  default_model: string;
  enabled: boolean;
  last_detected: Record<string, any>;
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

export const authAPI = {
  config: () => client.get<{ google_enabled: boolean }>('/api/v1/auth/config'),
  register: (email: string, password: string, name: string) =>
    client.post<User>('/api/v1/auth/register', { email, password, name }),
  login: (email: string, password: string) =>
    client.post<User>('/api/v1/auth/login', { email, password }),
  logout: () => client.post('/api/v1/auth/logout'),
  me: () => client.get<User>('/api/v1/auth/me'),
  googleLoginUrl: () => `${API_BASE_URL}/api/v1/auth/google`,
  listUsers: () => client.get<User[]>('/api/v1/users'),
};

// ---------------------------------------------------------------------------
// Organizations / workspaces
// ---------------------------------------------------------------------------

export interface Org {
  id: string;
  name: string;
  slug: string;
  type: 'personal' | 'company';
  plan: string;
  role: 'admin' | 'member';
  created_at: string;
}

export interface OrgMember {
  org_id: string;
  user_id: string;
  role: 'admin' | 'member';
  user_name: string;
  user_email: string;
  avatar_url: string;
}

export interface OrgTeam {
  id: string;
  org_id: string;
  name: string;
  description: string;
  members: OrgMember[];
}

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

export interface TeamGrant {
  project_id: string;
  org_team_id: string;
  role: string;
  team_name: string;
}

export const orgsAPI = {
  list: () => client.get<{ orgs: Org[]; active_org: string }>('/api/v1/orgs'),
  create: (name: string) => client.post<Org>('/api/v1/orgs', { name }),
  get: (id: string) => client.get<Org>(`/api/v1/orgs/${id}`),
  update: (id: string, payload: Partial<Org>) => client.put<Org>(`/api/v1/orgs/${id}`, payload),
  activate: (id: string) => client.post(`/api/v1/orgs/${id}/activate`),
  members: {
    list: (orgId: string) => client.get<OrgMember[]>(`/api/v1/orgs/${orgId}/members`),
    add: (orgId: string, email: string, role: string) =>
      client.post(`/api/v1/orgs/${orgId}/members`, { email, role }),
    setRole: (orgId: string, userId: string, role: string) =>
      client.put(`/api/v1/orgs/${orgId}/members/${userId}`, { role }),
    remove: (orgId: string, userId: string) =>
      client.delete(`/api/v1/orgs/${orgId}/members/${userId}`),
  },
};

export const orgTeamsAPI = {
  list: (orgId: string) => client.get<OrgTeam[]>(`/api/v1/orgs/${orgId}/teams`),
  create: (orgId: string, payload: { name: string; description?: string }) =>
    client.post<OrgTeam>(`/api/v1/orgs/${orgId}/teams`, payload),
  update: (teamId: string, payload: { name?: string; description?: string }) =>
    client.put<OrgTeam>(`/api/v1/org-teams/${teamId}`, payload),
  remove: (teamId: string) => client.delete(`/api/v1/org-teams/${teamId}`),
  addMember: (teamId: string, userId: string) =>
    client.post(`/api/v1/org-teams/${teamId}/members/${userId}`),
  removeMember: (teamId: string, userId: string) =>
    client.delete(`/api/v1/org-teams/${teamId}/members/${userId}`),
};

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

export interface ConnectorPairing {
  code: string;
  expires_at: string;
  api_url: string;
  deep_link: string;
  start_link: string;
}

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
  startLink: 'openv-connector://start',
};

export const projectTeamAccessAPI = {
  list: (projectId: string) =>
    client.get<TeamGrant[]>(`/api/v1/projects/${projectId}/team-access`),
  grant: (projectId: string, orgTeamId: string, role: string) =>
    client.put(`/api/v1/projects/${projectId}/team-access`, { org_team_id: orgTeamId, role }),
  revoke: (projectId: string, teamId: string) =>
    client.delete(`/api/v1/projects/${projectId}/team-access/${teamId}`),
};

export const membersAPI = {
  list: (projectId: string) =>
    client.get<ProjectMember[]>(`/api/v1/projects/${projectId}/members`),
  add: (projectId: string, email: string, role: string) =>
    client.post(`/api/v1/projects/${projectId}/members`, { email, role }),
  setRole: (projectId: string, userId: string, role: string) =>
    client.put(`/api/v1/projects/${projectId}/members/${userId}`, { role }),
  remove: (projectId: string, userId: string) =>
    client.delete(`/api/v1/projects/${projectId}/members/${userId}`),
};

export const metaAPI = {
  artifactTypes: () => client.get<ArtifactTypeDef[]>('/api/v1/meta/artifact-types'),
  linkTypes: () => client.get<LinkTypeRule[]>('/api/v1/meta/link-types'),
};

export const productProfileAPI = {
  get: (projectId: string) =>
    client.get<ProductProfile>(`/api/v1/projects/${projectId}/profile`),
  update: (projectId: string, payload: Partial<ProductProfile>) =>
    client.put<ProductProfile>(`/api/v1/projects/${projectId}/profile`, payload),
};

const downloadBlob = async (url: string, fallbackName: string) => {
  const response = await client.get(url, { responseType: 'blob' });
  const contentDisposition = response.headers['content-disposition'];
  let filename = fallbackName;
  if (contentDisposition) {
    const match = contentDisposition.match(/filename=(.+)/);
    if (match) filename = match[1].replace(/['"]/g, '');
  }
  const objectUrl = window.URL.createObjectURL(new Blob([response.data]));
  const anchor = document.createElement('a');
  anchor.href = objectUrl;
  anchor.setAttribute('download', filename);
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  window.URL.revokeObjectURL(objectUrl);
  return response;
};

export const vvAPI = {
  createRun: (projectId: string, payload: { name: string; description?: string; baseline_id?: string }) =>
    client.post<TestRun>(`/api/v1/projects/${projectId}/test-runs`, payload),
  listRuns: (projectId: string) =>
    client.get<TestRun[]>(`/api/v1/projects/${projectId}/test-runs`),
  getRun: (id: string) => client.get<TestRun>(`/api/v1/test-runs/${id}`),
  updateRun: (id: string, status: string) =>
    client.put<TestRun>(`/api/v1/test-runs/${id}`, { status }),
  deleteRun: (id: string) => client.delete(`/api/v1/test-runs/${id}`),
  upsertResult: (runId: string, payload: { test_case_id: string; status: string; notes?: string; evidence?: string[] }) =>
    client.post<TestResult>(`/api/v1/test-runs/${runId}/results`, payload),
  listResults: (runId: string) =>
    client.get<TestResult[]>(`/api/v1/test-runs/${runId}/results`),
  coverage: (projectId: string, baselineId?: string) =>
    client.get<CoverageReport>(`/api/v1/projects/${projectId}/vv/coverage`, {
      params: baselineId ? { baseline_id: baselineId } : {},
    }),
  matrix: (projectId: string, baselineId?: string) =>
    client.get<{ rows: MatrixRow[] }>(`/api/v1/projects/${projectId}/vv/matrix`, {
      params: baselineId ? { baseline_id: baselineId } : {},
    }),
  gaps: (projectId: string, baselineId?: string) =>
    client.get<Record<string, string[]>>(`/api/v1/projects/${projectId}/vv/gaps`, {
      params: baselineId ? { baseline_id: baselineId } : {},
    }),
  report: (projectId: string, baselineId?: string) =>
    downloadBlob(
      `/api/v1/projects/${projectId}/vv/report${baselineId ? `?baseline_id=${baselineId}` : ''}`,
      `vv_report_${new Date().toISOString().slice(0, 10)}.pdf`
    ),
};

export const workItemsAPI = {
  create: (projectId: string, payload: Partial<WorkItem>) =>
    client.post<WorkItem>(`/api/v1/projects/${projectId}/work-items`, payload),
  list: (projectId: string) =>
    client.get<WorkItem[]>(`/api/v1/projects/${projectId}/work-items`),
  get: (id: string) =>
    client.get<{ item: WorkItem; activity: WorkItemActivity[] }>(`/api/v1/work-items/${id}`),
  update: (id: string, payload: Partial<WorkItem>) =>
    client.put<WorkItem>(`/api/v1/work-items/${id}`, payload),
  remove: (id: string) => client.delete(`/api/v1/work-items/${id}`),
  move: (id: string, column: string, sortOrder: number) =>
    client.post<WorkItem>(`/api/v1/work-items/${id}/move`, { column, sort_order: sortOrder }),
  comment: (id: string, content: string) =>
    client.post<WorkItemActivity>(`/api/v1/work-items/${id}/comments`, { content }),
};

export const guidedAPI = {
  start: (projectId: string) =>
    client.post<GuidedSession>('/api/v1/guided-sessions', { project_id: projectId }),
  list: (projectId: string) =>
    client.get<GuidedSession[]>('/api/v1/guided-sessions', { params: { project_id: projectId } }),
  get: (id: string) => client.get<GuidedSession>(`/api/v1/guided-sessions/${id}`),
  saveStep: (id: string, step: number, answers: Record<string, any>) =>
    client.put<GuidedSession>(`/api/v1/guided-sessions/${id}/step`, { step, answers }),
  materializeDrafts: (id: string, drafts: any[]) =>
    client.post<{ artifact_ids: string[] }>(`/api/v1/guided-sessions/${id}/drafts`, { drafts }),
  commit: (id: string) => client.post<GuidedSession>(`/api/v1/guided-sessions/${id}/commit`),
  abandon: (id: string) => client.post<GuidedSession>(`/api/v1/guided-sessions/${id}/abandon`),
};

export const interviewsAPI = {
  create: (projectId: string, payload: { name: string; brief: string; agent_slug?: string }) =>
    client.post<Interview>(`/api/v1/projects/${projectId}/interviews`, payload),
  list: (projectId: string) =>
    client.get<Interview[]>(`/api/v1/projects/${projectId}/interviews`),
  close: (id: string) => client.post<Interview>(`/api/v1/interviews/${id}/close`),
  createInvite: (interviewId: string, inviteeLabel?: string) =>
    client.post<{ invite: any; token: string; path: string }>(
      `/api/v1/interviews/${interviewId}/invites`,
      { invitee_label: inviteeLabel || '' }
    ),
  listInvites: (interviewId: string) =>
    client.get<any[]>(`/api/v1/interviews/${interviewId}/invites`),
  revokeInvite: (inviteId: string) =>
    client.post(`/api/v1/interview-invites/${inviteId}/revoke`),
  listSessions: (interviewId: string) =>
    client.get<InterviewSession[]>(`/api/v1/interviews/${interviewId}/sessions`),
  transcript: (sessionId: string) =>
    client.get<InterviewMessage[]>(`/api/v1/interview-sessions/${sessionId}/transcript`),
};

export const publicInterviewAPI = {
  intro: (token: string) =>
    client.get<{ interview_name: string; session: InterviewSession; transcript: InterviewMessage[] }>(
      `/api/v1/public/interviews/${token}`
    ),
  sendMessage: (token: string, content: string, participantName?: string) =>
    client.post<{ session: InterviewSession; message: InterviewMessage }>(
      `/api/v1/public/interviews/${token}/messages`,
      { content, participant_name: participantName || '' }
    ),
  streamUrl: (token: string) => `${API_BASE_URL}/api/v1/public/interviews/${token}/stream`,
  finish: (token: string) => client.post(`/api/v1/public/interviews/${token}/finish`),
};

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
};

export const agentRunsAPI = {
  list: (params: { agent_id?: string; project_id?: string; status?: string; parent_id?: string; limit?: number }) =>
    client.get<AgentRun[]>('/api/v1/agent-runs', { params }),
  get: (id: string) => client.get<AgentRun>(`/api/v1/agent-runs/${id}`),
  tree: (id: string) => client.get<AgentRun[]>(`/api/v1/agent-runs/${id}/tree`),
  logs: (id: string, afterSeq = 0) =>
    client.get<RunLogEntry[]>(`/api/v1/agent-runs/${id}/logs`, { params: { after_seq: afterSeq } }),
  streamUrl: (id: string, afterSeq = 0) =>
    `${API_BASE_URL}/api/v1/agent-runs/${id}/stream?after_seq=${afterSeq}`,
  cancel: (id: string) => client.post<AgentRun>(`/api/v1/agent-runs/${id}/cancel`),
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

export interface ProviderLogin {
  id: string;
  provider: string;
  // 'workspace' runs on any shared worker; 'user' only on the requester's
  // personal runner (the credential lands on their own machine).
  target: 'workspace' | 'user';
  status: 'pending' | 'claimed' | 'url_ready' | 'awaiting_code' | 'completed' | 'failed' | 'cancelled';
  auth_url: string;
  detail: string;
  created_at: string;
  updated_at: string;
}

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
};

export const eventsAPI = {
  list: (params: { project_id?: string; event_type?: string; limit?: number }) =>
    client.get<any[]>('/api/v1/events', { params }),
};

export default client;
