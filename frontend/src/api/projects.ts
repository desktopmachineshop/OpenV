// Projects: their settings, downloads, baselines and baseline diffs,
// templates, artifact types and link rules, attribute definitions, the
// product profile, the work items on a project's board, and to-dos.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

import { filenameFromContentDisposition } from './contentDisposition';
import { downloadExtension, downloadQuery } from '../utils/downloadSelection';
import { client, saveBlob } from './http';
import type { LinkedArtifact } from './types/artifacts';
import type {
  ArtifactTypeDef,
  AttributeDefinition,
  Baseline,
  BaselineDiff,
  DownloadFormat,
  DownloadOptions,
  DownloadSelection,
  LinkTypeRule,
  Party,
  PlanExportFormat,
  PlanMilestoneInput,
  PlanScheduleInput,
  PlanTask,
  ProductProfile,
  Project,
  ProjectExport,
  ProjectPlan,
  StoredPlanMilestone,
  Template,
  Todo,
  TodoInput,
  WorkItem,
  WorkItemActivity,
} from './types/projects';

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
  /** The projects filed under this one (REQ-144). */
  children: (id: string) => client.get<Project[]>(`/api/v1/projects/${id}/children`),
  /** The far end of every link crossing out of the project (REQ-145). */
  linkedArtifacts: (id: string) =>
    client.get<LinkedArtifact[]>(`/api/v1/projects/${id}/linked-artifacts`),
  /** The parties the project recognises as owners, the workspace first. */
  parties: (id: string) => client.get<{ parties: Party[] }>(`/api/v1/projects/${id}/parties`),
  setParties: (id: string, parties: Party[]) =>
    client.put<{ parties: Party[] }>(`/api/v1/projects/${id}/parties`, { parties }),
  /** Whether the editor fills a requirement's priority from its shall, should or may. */
  priorityDetection: (id: string) =>
    client.get<{ enabled: boolean }>(`/api/v1/projects/${id}/priority-detection`),
  setPriorityDetection: (id: string, enabled: boolean) =>
    client.put<{ enabled: boolean }>(`/api/v1/projects/${id}/priority-detection`, { enabled }),
  downloadOptions: (id: string, baselineId?: string) => {
    const params = baselineId && baselineId !== 'live' ? `?baseline_id=${encodeURIComponent(baselineId)}` : '';
    return client.get<DownloadOptions>(`/api/v1/projects/${id}/download/options${params}`);
  },
  // One call per format, all of them narrowed by the same selection. The
  // response may be the document itself or a zip of it with its attachments —
  // the filename in the Content-Disposition header says which, so the browser
  // saves whatever the server actually built.
  download: async (
    id: string,
    format: DownloadFormat,
    selection: DownloadSelection,
    baselineId?: string
  ) => {
    const query = downloadQuery(selection, baselineId);
    const response = await client.get(
      `/api/v1/projects/${id}/download/${format}${query ? `?${query}` : ''}`,
      { responseType: 'blob' }
    );

    const filename =
      filenameFromContentDisposition(response.headers['content-disposition']) ||
      `project_download_${new Date().toISOString().slice(0, 10)}.${downloadExtension(format)}`;

    saveBlob(response.data, filename);
    return response;
  },
  // A new project from an OpenV JSON export or a ReqIF file. The server can
  // tell the two apart by content, but a .reqif or .xml file is named as
  // ReqIF so that one that is not valid ReqIF is reported as such rather
  // than as bad JSON.
  import: async (file: File) => {
    const fileContent = await file.text();
    const reqif = /\.(reqif|xml)$/i.test(file.name);
    const response = await client.post<{ status: string; message: string; project_id: string }>(
      '/api/v1/projects/import',
      fileContent,
      {
        headers: {
          'Content-Type': reqif ? 'application/xml' : 'application/json',
        },
        ...(reqif ? { params: { format: 'reqif' } } : {}),
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

/** To-dos (issue #731): a project's list, My to-dos, and one to-do's edits. */
export const todosAPI = {
  listProject: (projectId: string) => client.get<Todo[]>(`/api/v1/projects/${projectId}/todos`),
  createProject: (projectId: string, payload: TodoInput) =>
    client.post<Todo>(`/api/v1/projects/${projectId}/todos`, payload),
  /** The caller's to-dos in the active workspace, across its projects. */
  listMine: () => client.get<Todo[]>('/api/v1/todos'),
  /** A personal to-do: no project, assigned to its writer. */
  createPersonal: (payload: Pick<TodoInput, 'text' | 'due_date'>) =>
    client.post<Todo>('/api/v1/todos', payload),
  get: (id: string) => client.get<Todo>(`/api/v1/todos/${id}`),
  update: (id: string, payload: Pick<TodoInput, 'text' | 'assignee_id' | 'due_date'>) =>
    client.put<Todo>(`/api/v1/todos/${id}`, payload),
  setDone: (id: string, done: boolean) => client.post<Todo>(`/api/v1/todos/${id}/done`, { done }),
  promote: (id: string) =>
    client.post<{ todo: Todo; work_item: WorkItem }>(`/api/v1/todos/${id}/promote`),
  remove: (id: string) => client.delete(`/api/v1/todos/${id}`),
};

const PLAN_EXTENSION: Record<PlanExportFormat, string> = { pdf: 'pdf', excel: 'xlsx', csv: 'csv', json: 'json' };

/** The project plan, the Gantt over the board: its read, edits and export. */
export const planAPI = {
  get: (projectId: string) => client.get<ProjectPlan>(`/api/v1/projects/${projectId}/plan`),
  /** A task's dates, parent and dependencies; answers its row of the plan. */
  schedule: (workItemId: string, payload: PlanScheduleInput) =>
    client.put<PlanTask>(`/api/v1/work-items/${workItemId}/schedule`, payload),
  /** A to-do raised on a task, which counts toward its progress. */
  addTodo: (workItemId: string, text: string) =>
    client.post<Todo>(`/api/v1/work-items/${workItemId}/todos`, { text }),
  createMilestone: (projectId: string, payload: PlanMilestoneInput) =>
    client.post<StoredPlanMilestone>(`/api/v1/projects/${projectId}/plan/milestones`, payload),
  updateMilestone: (projectId: string, milestoneId: string, payload: PlanMilestoneInput) =>
    client.put<StoredPlanMilestone>(`/api/v1/projects/${projectId}/plan/milestones/${milestoneId}`, payload),
  removeMilestone: (projectId: string, milestoneId: string) =>
    client.delete(`/api/v1/projects/${projectId}/plan/milestones/${milestoneId}`),
  /** The completion criteria of a release's milestone. */
  setReleaseCriteria: (projectId: string, releaseId: string, criteria: PlanMilestoneInput['criteria']) =>
    client.put<StoredPlanMilestone>(`/api/v1/projects/${projectId}/plan/release-milestones/${releaseId}`, { criteria }),
  /** Saves the plan as a file in the chosen format. */
  export: async (projectId: string, format: PlanExportFormat) => {
    const response = await client.get(`/api/v1/projects/${projectId}/plan/export`, {
      params: { format },
      responseType: 'blob',
    });
    const filename =
      filenameFromContentDisposition(response.headers['content-disposition']) ||
      `plan-${new Date().toISOString().slice(0, 10)}.${PLAN_EXTENSION[format]}`;
    saveBlob(response.data, filename);
    return response;
  },
};
