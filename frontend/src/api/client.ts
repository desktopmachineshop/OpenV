import axios, { AxiosInstance } from 'axios';
import { filenameFromContentDisposition } from './contentDisposition';
import type { SharedProductPayload, toSharePayload } from '../utils/randomProduct';

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

export type ArtifactStatus = 'draft' | 'in_review' | 'approved' | 'superseded';

export interface Artifact {
  id: string;
  project_id: string;
  parent_id?: string | null;
  type: string;
  // Stable address, minted once per project and never reissued — this is what
  // a reader cites ("REQ-12"). Constant across versions and reordering.
  ref?: string;
  // Derived section number for headings ("1.2"). Position-dependent, so it
  // changes when the document is reordered; served only when the caller asks
  // for doc_numbers=1. Never a citation — that is what ref is for.
  doc_number?: string;
  title: string;
  body: string;
  sort_order?: number;
  // Review state: draft | in_review | approved | superseded.
  // First-class column; attributes.status is only a deprecated mirror.
  status?: ArtifactStatus;
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
  // True when the content of a linked artifact changed after the link was
  // made; cleared by an explicit confirm or by re-approving the artifact.
  // Optional: absent on legacy links_snapshot copies.
  suspect?: boolean;
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

// Server-side page size for artifact listings (the backend defaults to and
// caps at 1000 per request; the total rides on the X-Total-Count header).
const ARTIFACT_PAGE_LIMIT = 1000;

export const artifactAPI = {
  create: (payload: Partial<Artifact>) =>
    client.post<Artifact>('/api/v1/artifacts', payload),
  get: (id: string) =>
    client.get<Artifact>(`/api/v1/artifacts/${id}`),
  // One page of a project's artifacts in stable tree order. The response body
  // is a plain array; res.headers['x-total-count'] carries the total so
  // callers can page until exhaustion.
  listPage: (
    projectId: string,
    opts?: { type?: string; limit?: number; offset?: number; docNumbers?: boolean }
  ) =>
    client.get<Artifact[]>('/api/v1/artifacts', {
      params: {
        project_id: projectId,
        type: opts?.type,
        limit: opts?.limit,
        offset: opts?.offset,
        // Opt-in: computing section numbers reads the whole project, which a
        // plain paginated fetch has no reason to pay for.
        doc_numbers: opts?.docNumbers ? '1' : undefined,
      },
    }),
  // All of a project's artifacts, fetched through the paged API. The module
  // view renders artifacts as a parent_id tree, so it structurally needs the
  // full set to build hierarchy; most projects fit in one page. UI follow-up
  // (issue #136): render the tree incrementally (lazy-load subtrees) so huge
  // projects don't need this loop at all.
  list: async (projectId: string, type?: string): Promise<{ data: Artifact[] }> => {
    const all: Artifact[] = [];
    for (;;) {
      const res = await client.get<Artifact[]>('/api/v1/artifacts', {
        params: {
          project_id: projectId,
          type,
          limit: ARTIFACT_PAGE_LIMIT,
          offset: all.length,
          // This is the whole-project read the tree view renders, so it is
          // the one call that wants section numbers.
          doc_numbers: '1',
        },
      });
      const page = res.data || [];
      all.push(...page);
      const total = parseInt(res.headers?.['x-total-count'] ?? '', 10);
      // Stop on a short page, or once we have everything the server counted.
      // A server that doesn't send the header (older API) implies short-page
      // termination only.
      if (page.length < ARTIFACT_PAGE_LIMIT || (Number.isFinite(total) && all.length >= total)) {
        return { data: all };
      }
    }
  },
  update: (id: string, payload: Partial<Artifact>) =>
    client.put<Artifact>(`/api/v1/artifacts/${id}`, payload),
  // One review state-machine transition; the server enforces legality
  // (400 unknown status, 409 illegal transition, 403 insufficient role).
  changeStatus: (id: string, status: ArtifactStatus) =>
    client.put<Artifact>(`/api/v1/artifacts/${id}/status`, { status }),
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
  // Live links straight from the link table (no version snapshot involved).
  // Use this for the artifact currently on screen: link writes version-bump
  // the counterpart artifact server-side, so a client-held version number can
  // be stale and a version-scoped fetch would miss fresh links (issue #169).
  listForArtifact: (artifactId: string) =>
    client.get<Link[]>(`/api/v1/artifacts/${artifactId}/links`),
  update: (id: string, payload: Partial<Link>) =>
    client.put<Link>(`/api/v1/links/${id}`, payload),
  // Clears the suspect flag: the caller vouches the link still holds after
  // the linked artifact's content changed. Editor role required.
  confirm: (id: string) =>
    client.put<Link>(`/api/v1/links/${id}/confirm`),
  delete: (id: string) =>
    client.delete(`/api/v1/links/${id}`),
};

// A suspect link enriched with its endpoints' titles/types, as returned by
// the review-queue endpoint (issue #183). Distinct from Link: it is a
// read-only projection joined against both artifacts server-side.
export interface SuspectLink {
  id: string;
  type: string;
  from_id: string;
  from_title: string;
  from_type: string;
  to_id: string;
  to_title: string;
  to_type: string;
  updated_at: string;
}

// The review-queue payload: the two things awaiting a reviewer's attention.
export interface ReviewQueue {
  suspect_links: SuspectLink[];
  in_review_artifacts: Artifact[];
}

export const reviewAPI = {
  // The reviewer's daily driver: suspect links + in-review artifacts for one
  // project, in a single round trip. Viewer role suffices to read it.
  get: (projectId: string) =>
    client.get<ReviewQueue>(`/api/v1/projects/${projectId}/review-queue`),
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
  export: async (id: string, format: 'json' | 'csv' = 'json') => {
    const response = await client.get(`/api/v1/projects/${id}/export?format=${format}`, {
      responseType: 'blob',
    });

    // Extract filename from Content-Disposition header or use default
    const filename =
      filenameFromContentDisposition(response.headers['content-disposition']) ||
      `project_export_${new Date().toISOString().slice(0, 10)}.${format}`;

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
  report: async (id: string, baselineId?: string, format: 'pdf' | 'docx' = 'pdf') => {
    const query = new URLSearchParams();
    if (baselineId && baselineId !== 'live') {
      query.set('baseline_id', baselineId);
    }
    if (format && format !== 'pdf') {
      query.set('format', format);
    }
    const params = query.toString() ? `?${query.toString()}` : '';
    const response = await client.get(`/api/v1/projects/${id}/report${params}`, {
      responseType: 'blob',
    });

    const filename =
      filenameFromContentDisposition(response.headers['content-disposition']) ||
      `project_report_${new Date().toISOString().slice(0, 10)}.${format}`;

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

/** One side of a baseline comparison: a baseline, or the live project (id "live"). */
export interface BaselineDiffRef {
  id: string;
  name: string;
}

/** An artifact present on only one side of a baseline comparison. */
export interface BaselineDiffArtifact {
  id: string;
  type: string;
  title: string;
}

/** An artifact present on both sides with at least one tracked field changed. */
export interface BaselineDiffModified {
  id: string;
  type: string;
  old_title: string;
  new_title: string;
  title_changed: boolean;
  body_changed: boolean;
  type_changed: boolean;
  status_changed: boolean;
  parent_changed: boolean;
}

/** A link present on only one side of a baseline comparison. */
export interface BaselineDiffLink {
  from_id: string;
  to_id: string;
  type: string;
  from_title: string;
  to_title: string;
}

/** Changes in the direction base → target (added = present only in target). */
export interface BaselineDiff {
  base: BaselineDiffRef;
  target: BaselineDiffRef;
  added: BaselineDiffArtifact[];
  removed: BaselineDiffArtifact[];
  modified: BaselineDiffModified[];
  links_added: BaselineDiffLink[];
  links_removed: BaselineDiffLink[];
}

export const baselineAPI = {
  create: (projectId: string, name: string) =>
    client.post<Baseline>(`/api/v1/projects/${projectId}/baselines`, { name }),
  list: (projectId: string) =>
    client.get<Baseline[]>(`/api/v1/projects/${projectId}/baselines`),
  get: (baselineId: string) =>
    client.get<ProjectExport>(`/api/v1/baselines/${baselineId}`),
  diff: (baselineId: string, against: string) =>
    client.get<BaselineDiff>(`/api/v1/baselines/${baselineId}/diff`, { params: { against } }),
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
  // created_by is the authoring user's id (omitted for agent/system entries);
  // author_name is the display label resolved at write time (user name/email
  // or agent name), blank on pre-authorship rows.
  created_by?: string;
  author_name?: string;
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
  // Per-user email-notification opt-out (issue #187). Only has an effect when
  // the server has SMTP configured.
  email_notifications?: boolean;
  created_at: string;
}

export interface NotificationPrefs {
  email_notifications: boolean;
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

export type AttributeDataType = 'text' | 'number' | 'date' | 'enum' | 'boolean';

// A configurable typed attribute definition (issue #219). Exactly one of
// org_id (org-wide) or project_id (project-scoped) is set. Values live in the
// artifact's attributes map under `key`.
export interface AttributeDefinition {
  id: string;
  org_id: string | null;
  project_id: string | null;
  key: string;
  label: string;
  data_type: AttributeDataType;
  enum_values: string[];
  // Artifact type key this applies to, or '' for all types.
  applies_to_type: string;
  required: boolean;
  sort_order: number;
  created_at: string;
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
  // Set when an agent run produced this result, so reviewers can tell
  // agent-executed evidence from human-executed.
  executed_by_agent_run_id?: string | null;
}

// How a test case can be carried out. Only 'automated' cases may be executed
// by an agent; 'manual' needs a person and 'physical' needs hardware or a rig.
// Stored on the test-case artifact's `execution_method` attribute; an unset
// value means 'automated'.
export type ExecutionMethod = 'automated' | 'manual' | 'physical';

export const EXECUTION_METHODS: { value: ExecutionMethod; label: string; hint: string }[] = [
  { value: 'automated', label: 'Automated', hint: 'An agent or CI job can run this end to end.' },
  { value: 'manual', label: 'Manual (human)', hint: 'Needs a person: inspection, judgement, usability.' },
  { value: 'physical', label: 'Physical test', hint: 'Needs hardware, a rig, or lab measurement.' },
];

// Reads a test case artifact's execution method, defaulting to automated.
export const executionMethodOf = (artifact?: { attributes?: Record<string, any> } | null): ExecutionMethod => {
  const raw = artifact?.attributes?.execution_method;
  if (typeof raw !== 'string') return 'automated';
  const v = raw.trim().toLowerCase();
  return v === 'manual' || v === 'physical' ? v : 'automated';
};

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

// --- Requirement quality linting (issue #217) ---

export interface QualityFinding {
  rule: string;
  severity: 'error' | 'warning' | 'info';
  message: string;
  start: number;
  end: number;
  match?: string;
}

export interface QualityScore {
  artifact_id: string;
  title: string;
  type: string;
  score: number; // 0-100, higher is better
  band: 'good' | 'fair' | 'poor';
  findings: QualityFinding[];
}

export interface QualityReport {
  project_id: string;
  entries: QualityScore[];
  summary: Record<string, number>; // band -> count
}

export type ImpactDirection = 'downstream' | 'upstream' | 'both';

export interface ImpactNode {
  artifact_id: string;
  title: string;
  type: string;
  distance: number;
  via: string;
  path: string[];
}

export interface ImpactGroup {
  type: string;
  count: number;
  nodes: ImpactNode[];
}

export interface ImpactReport {
  project_id: string;
  artifact_id: string;
  direction: ImpactDirection;
  downstream: ImpactGroup[];
  upstream: ImpactGroup[];
  total: number;
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

export interface GuidedChatMessage {
  id: string;
  session_id: string;
  role: 'assistant' | 'user' | 'system';
  content: string;
  created_at: string;
}

export interface Interview {
  id: string;
  project_id: string;
  persona_artifact_id?: string | null;
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
  effort: string;
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
  // Provenance: set when this run was re-enqueued from a terminal run via
  // the retry endpoint (no run-tree semantics, unlike parent_run_id).
  retried_from_run_id?: string | null;
  status: string;
  prompt: string;
  final_text: string;
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

export const authAPI = {
  config: () =>
    client.get<{ google_enabled: boolean; oidc_enabled: boolean; oidc_provider_name: string }>(
      '/api/v1/auth/config'
    ),
  register: (email: string, password: string, name: string) =>
    client.post<User>('/api/v1/auth/register', { email, password, name }),
  login: (email: string, password: string) =>
    client.post<User>('/api/v1/auth/login', { email, password }),
  logout: () => client.post('/api/v1/auth/logout'),
  me: () => client.get<User>('/api/v1/auth/me'),
  googleLoginUrl: () => `${API_BASE_URL}/api/v1/auth/google`,
  oidcLoginUrl: () => `${API_BASE_URL}/api/v1/auth/oidc/login`,
  listUsers: () => client.get<User[]>('/api/v1/users'),
};

// Per-user notification preferences (issue #187): the email opt-out toggle.
export const notificationPrefsAPI = {
  get: () => client.get<NotificationPrefs>('/api/v1/me/notification-prefs'),
  update: (email_notifications: boolean) =>
    client.put<NotificationPrefs>('/api/v1/me/notification-prefs', { email_notifications }),
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
  // Monthly spend budget (issue #186). null/undefined = no budget set.
  monthly_budget_usd?: number | null;
  // Last budget-alert dedupe state (YYYY-MM and the highest % threshold
  // already alerted that month); present only once an alert has fired.
  budget_alert_month?: string;
  budget_alert_threshold?: number;
  // Present on soft-deleted workspaces (listDeleted); restorable for 30 days
  // from this time, then permanently purged.
  deleted_at?: string;
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

// Workspace usage rollup: the same runs aggregated by agent and by day.
export interface OrgUsageTotals {
  runs: number;
  tokens_in: number;
  tokens_out: number;
  cost_usd: number;
}

export interface OrgAgentUsage extends OrgUsageTotals {
  agent_slug: string;
  agent_name: string;
}

export interface OrgDailyUsage extends OrgUsageTotals {
  day: string; // YYYY-MM-DD (UTC)
}

export interface OrgUsageSummary {
  days: number;
  totals: OrgUsageTotals;
  by_agent: OrgAgentUsage[];
  by_day: OrgDailyUsage[];
  // Current calendar month spend (UTC), what the budget bar measures against.
  month_to_date_cost_usd: number;
}

export const orgsAPI = {
  list: () => client.get<{ orgs: Org[]; active_org: string }>('/api/v1/orgs'),
  usage: (orgId: string, days?: number) =>
    client.get<OrgUsageSummary>(`/api/v1/orgs/${orgId}/usage`, {
      params: days ? { days } : {},
    }),
  create: (name: string) => client.post<Org>('/api/v1/orgs', { name }),
  get: (id: string) => client.get<Org>(`/api/v1/orgs/${id}`),
  update: (id: string, payload: Partial<Org>) => client.put<Org>(`/api/v1/orgs/${id}`, payload),
  // Soft delete: the workspace is hidden and locked, restorable for 30 days,
  // then hard-deleted by the server's purge job.
  remove: (id: string) =>
    client.delete<{ deleted_at: string; purge_after: string }>(`/api/v1/orgs/${id}`),
  restore: (id: string) => client.post<Org>(`/api/v1/orgs/${id}/restore`),
  listDeleted: () => client.get<{ orgs: Org[] }>('/api/v1/orgs', { params: { deleted: 'true' } }),
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

/**
 * The community pool of joke demo products for the new-project wizard.
 *
 * This is the one API surface in OpenV whose reads and writes cross
 * workspaces on purpose: everyone sees the same pool, so it grows as people
 * share what their agents invent. The server sanitizes every entry to inert
 * plain text, rate-limits publishing per workspace, and returns no author
 * identity — `report` is the path for anything that should not be there.
 */
export const sharedProductsAPI = {
  list: () => client.get<SharedProductPayload[]>('/api/v1/shared-products'),
  publish: (product: ReturnType<typeof toSharePayload>) =>
    client.post<SharedProductPayload>('/api/v1/shared-products', product),
  report: (id: string) => client.post(`/api/v1/shared-products/${id}/report`),
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
  // Combined open-or-pair link: the connector starts with its existing
  // pairing for the workspace and only spends the code when unpaired.
  open_link?: string;
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
  // Include the workspace so a connector paired with several workspaces
  // starts against the one the browser is in.
  startLink: (orgId?: string) => `openv-connector://start${orgId ? `?org=${orgId}` : ''}`,
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

// Requirement quality rule sets (workspace house style, overridable per
// project). `effective` is what the linter and agents use; `workspace` and
// `project` are the overrides that produced it, either of them null when that
// level sets nothing. `catalog` carries the vocabulary the editor renders.
export type QualityConvention = 'shall' | 'rfc2119';
export type QualitySeverity = 'error' | 'warning' | 'info' | 'off';

export interface QualityRuleSet {
  convention?: QualityConvention | '';
  severities?: Record<string, QualitySeverity>;
}

export interface QualityRulesCatalog {
  conventions: QualityConvention[];
  rules: string[];
  severities: QualitySeverity[];
  defaults: Required<QualityRuleSet>;
  labels: Record<string, string>;
}

export interface QualityRules {
  effective: Required<QualityRuleSet>;
  workspace: QualityRuleSet | null;
  project: QualityRuleSet | null;
  summary: string;
  catalog: QualityRulesCatalog;
}

// An empty rule set clears the level's override, so it inherits again.
export const qualityRulesAPI = {
  forProject: (projectId: string) =>
    client.get<QualityRules>(`/api/v1/projects/${projectId}/quality-rules`),
  setForProject: (projectId: string, payload: QualityRuleSet) =>
    client.put<QualityRules>(`/api/v1/projects/${projectId}/quality-rules`, payload),
  forWorkspace: (orgId: string) =>
    client.get<QualityRules>(`/api/v1/orgs/${orgId}/quality-rules`),
  setForWorkspace: (orgId: string, payload: QualityRuleSet) =>
    client.put<QualityRules>(`/api/v1/orgs/${orgId}/quality-rules`, payload),
};

export const metaAPI = {
  artifactTypes: () => client.get<ArtifactTypeDef[]>('/api/v1/meta/artifact-types'),
  linkTypes: () => client.get<LinkTypeRule[]>('/api/v1/meta/link-types'),
  // Effective (org-wide + project override) attribute definitions for a
  // project — used to render typed inputs in the artifact editor.
  attributeDefinitions: (projectId: string) =>
    client.get<AttributeDefinition[]>('/api/v1/meta/attribute-definitions', {
      params: { project_id: projectId },
    }),
};

// Attribute definition management (issue #219). Org-wide definitions require
// org admin; project-scoped require project editor (enforced server-side).
export const attributeDefinitionAPI = {
  listByProject: (projectId: string) =>
    client.get<AttributeDefinition[]>('/api/v1/attribute-definitions', {
      params: { project_id: projectId },
    }),
  listByOrg: (orgId: string) =>
    client.get<AttributeDefinition[]>('/api/v1/attribute-definitions', {
      params: { org_id: orgId },
    }),
  create: (payload: Partial<AttributeDefinition>) =>
    client.post<AttributeDefinition>('/api/v1/attribute-definitions', payload),
  update: (id: string, payload: Partial<AttributeDefinition>) =>
    client.put<AttributeDefinition>(`/api/v1/attribute-definitions/${id}`, payload),
  remove: (id: string) => client.delete(`/api/v1/attribute-definitions/${id}`),
};

export const productProfileAPI = {
  get: (projectId: string) =>
    client.get<ProductProfile>(`/api/v1/projects/${projectId}/profile`),
  update: (projectId: string, payload: Partial<ProductProfile>) =>
    client.put<ProductProfile>(`/api/v1/projects/${projectId}/profile`, payload),
};

const downloadBlob = async (url: string, fallbackName: string) => {
  const response = await client.get(url, { responseType: 'blob' });
  const filename =
    filenameFromContentDisposition(response.headers['content-disposition']) || fallbackName;
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
  // Launch an agent run that executes this test run's agent-executable cases.
  // Manual/physical cases are excluded server-side and returned as `skipped`.
  launchAgentRun: (runId: string, payload: { agent_slug: string; test_case_ids?: string[] }) =>
    client.post<{
      run: AgentRun;
      executing: number;
      skipped: { id: string; title: string; execution_method: ExecutionMethod }[];
    }>(`/api/v1/test-runs/${runId}/agent-run`, payload),
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
  // Change-impact analysis: the artifacts reachable from `artifactId` through
  // the traceability link graph, grouped by type. `direction` selects
  // downstream (things that depend on it), upstream (what it depends on), or
  // both (default).
  impact: (
    projectId: string,
    artifactId: string,
    direction?: ImpactDirection,
    baselineId?: string
  ) =>
    client.get<ImpactReport>(`/api/v1/projects/${projectId}/impact`, {
      params: {
        artifact: artifactId,
        ...(direction ? { direction } : {}),
        ...(baselineId ? { baseline_id: baselineId } : {}),
      },
    }),
};

export const qualityAPI = {
  // Per-requirement quality scores + findings for a project (viewer role).
  project: (projectId: string, baselineId?: string) =>
    client.get<QualityReport>(`/api/v1/projects/${projectId}/quality`, {
      params: baselineId ? { baseline_id: baselineId } : {},
    }),
  // Single-artifact lint (400 for non-requirement types).
  artifact: (artifactId: string) =>
    client.get<QualityScore>(`/api/v1/artifacts/${artifactId}/quality`),
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
  listMessages: (id: string) =>
    client.get<GuidedChatMessage[]>(`/api/v1/guided-sessions/${id}/messages`),
  sendMessage: (id: string, content: string, step: number, state: Record<string, any>) =>
    client.post<{ message: GuidedChatMessage; runner_online?: boolean }>(
      `/api/v1/guided-sessions/${id}/messages`,
      { content, step, state }
    ),
  kickoffChat: (id: string, step: number, state: Record<string, any>) =>
    client.post<{ status: 'launched' | 'pending' | 'skipped' | 'unavailable'; runner_online?: boolean }>(
      `/api/v1/guided-sessions/${id}/chat/kickoff`,
      { step, state }
    ),
  nudgeChat: (id: string, step: number, state: Record<string, any>, event: string) =>
    client.post<{ status: 'launched' | 'pending' | 'unavailable'; runner_online?: boolean }>(
      `/api/v1/guided-sessions/${id}/chat/nudge`,
      { step, state, event }
    ),
  chatStreamUrl: (id: string) => `${API_BASE_URL}/api/v1/guided-sessions/${id}/chat/stream`,
};

export const interviewsAPI = {
  create: (
    projectId: string,
    payload: { name: string; brief: string; agent_slug?: string; persona_artifact_id?: string | null }
  ) => client.post<Interview>(`/api/v1/projects/${projectId}/interviews`, payload),
  list: (projectId: string) =>
    client.get<Interview[]>(`/api/v1/projects/${projectId}/interviews`),
  close: (id: string) => client.post<Interview>(`/api/v1/interviews/${id}/close`),
  setPersona: (id: string, personaArtifactId: string | null) =>
    client.put<Interview>(`/api/v1/interviews/${id}/persona`, { persona_artifact_id: personaArtifactId }),
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
  listProjectSessions: (projectId: string, limit?: number) =>
    client.get<InterviewSession[]>(`/api/v1/projects/${projectId}/interview-sessions`, {
      params: limit ? { limit } : {},
    }),
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
  // Launch the seeded test-case-author agent to draft test cases (and verifies
  // links) for the given requirement artifacts, as proposals. The agent fetches
  // the requirement content itself via its OpenV tools — only the IDs travel.
  draftTestCases: (projectId: string, requirementIds: string[]) =>
    client.post<AgentRun>(`/api/v1/projects/${projectId}/draft-test-cases`, {
      requirement_ids: requirementIds,
    }),
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

export interface BulkProposalOutcome {
  id: string;
  ok: boolean;
  error?: string;
}

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

// Domain audit event (see internal/domain/events). Actors are "system",
// "user:<id>" or "agent:<run_id>".
export interface DomainEvent {
  id: string;
  org_id?: string;
  event_type: string;
  project_id?: string;
  entity_id?: string;
  actor: string;
  payload: Record<string, any>;
  created_at: string;
}

export const eventsAPI = {
  // The backend clamps limit to (0, 500], defaulting to 100. `before` is a
  // keyset cursor (an event ID from a previous page): the server returns only
  // events strictly older than it. When another (older) page may exist, the
  // response carries an X-Next-Cursor header with the cursor for it.
  list: (params: { project_id?: string; event_type?: string; limit?: number; before?: string }) =>
    client.get<DomainEvent[]>('/api/v1/events', { params }),
};

// One row of the global artifact search (GET /api/v1/search). Results are
// already scoped server-side to projects the caller can access.
export interface SearchHit {
  artifact_id: string;
  project_id: string;
  project_name: string;
  type: string;
  title: string;
  snippet: string;
  // Semantic-similarity score (0..1); present only for semantic/hybrid hits.
  score?: number;
}

// Ranking path for GET /api/v1/search. 'keyword' is the original trigram match;
// 'semantic' and 'hybrid' use artifact embeddings and fall back to keyword when
// embeddings are not configured (see mode_used in the response).
export type SearchMode = 'keyword' | 'semantic' | 'hybrid';

// Envelope returned by GET /api/v1/search. mode_used reports which path
// actually ran — it differs from the requested mode when a semantic/hybrid
// request degraded to keyword because embeddings are unavailable.
export interface SearchResponse {
  mode_used: SearchMode;
  hits: SearchHit[];
}

export const searchAPI = {
  global: (q: string, opts?: { limit?: number; mode?: SearchMode }) =>
    client.get<SearchResponse>('/api/v1/search', {
      params: {
        q,
        ...(opts?.limit ? { limit: opts.limit } : {}),
        ...(opts?.mode ? { mode: opts.mode } : {}),
      },
    }),
};

// One candidate-duplicate pairing from GET /api/v1/projects/{id}/duplicates.
export interface DuplicatePair {
  artifact_id: string;
  artifact_title: string;
  artifact_type: string;
  other_id: string;
  other_title: string;
  other_type: string;
  similarity: number;
}

export interface DuplicatesResponse {
  enabled: boolean;
  note?: string;
  pairs: DuplicatePair[];
}

export const duplicatesAPI = {
  forProject: (projectId: string) =>
    client.get<DuplicatesResponse>(`/api/v1/projects/${projectId}/duplicates`),
};

// In-app notification (issue #132). entity_ref carries {kind, ...ids} so the
// bell can navigate to the subject; see NotificationBell.
export interface AppNotification {
  id: string;
  org_id?: string;
  user_id: string;
  type: 'proposal_pending' | 'run_failed' | 'interview_completed' | 'mention' | string;
  title: string;
  body?: string;
  entity_ref: Record<string, any>;
  read: boolean;
  created_at: string;
}

export interface NotificationList {
  notifications: AppNotification[] | null;
  unread_count: number;
}

export const notificationsAPI = {
  list: (params?: { unread?: boolean; limit?: number }) =>
    client.get<NotificationList>('/api/v1/notifications', {
      params: {
        ...(params?.unread ? { unread: 'true' } : {}),
        ...(params?.limit ? { limit: params.limit } : {}),
      },
    }),
  markRead: (ids: string[]) =>
    client.post<{ updated: number; unread_count: number }>('/api/v1/notifications/read', { ids }),
  markAllRead: () =>
    client.post<{ updated: number; unread_count: number }>('/api/v1/notifications/read-all'),
  streamUrl: () => `${API_BASE_URL}/api/v1/notifications/stream`,
};

export default client;
