// Projects: their settings, downloads, baselines and baseline diffs,
// templates, artifact types and link rules, attribute definitions, the
// product profile, and the work items on a project's board.
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
  ProductProfile,
  Project,
  ProjectExport,
  Template,
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
