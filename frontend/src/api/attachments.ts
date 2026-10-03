// Files attached to artifacts, their versions, uploads and downloads.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

import { API_BASE_URL, client, uploadConfig } from './http';
import type { Attachment, AttachmentVersion, UploadProgressHandler } from './types/attachments';

export const attachmentAPI = {
  upload: (artifactId: string, file: File, onProgress?: UploadProgressHandler) => {
    const formData = new FormData();
    formData.append('artifact_id', artifactId);
    formData.append('file', file);
    return client.post<Attachment>(
      '/api/v1/attachments/upload',
      formData,
      uploadConfig(onProgress)
    );
  },
  getMeta: (id: string) =>
    client.get<Attachment>(`/api/v1/attachments/${id}`),
  // The version is part of the URL, so a new version is a new URL: the
  // browser cannot serve the superseded drawing from cache, and an older
  // version stays addressable.
  getDownloadUrl: (id: string, version?: number) =>
    `${API_BASE_URL}/api/v1/attachments/${id}/download${version ? `?version=${version}` : ''}`,
  uploadVersion: (id: string, file: File, onProgress?: UploadProgressHandler) => {
    const formData = new FormData();
    formData.append('file', file);
    return client.post<Attachment>(
      `/api/v1/attachments/${id}/versions`,
      formData,
      uploadConfig(onProgress)
    );
  },
  listVersions: (id: string) =>
    client.get<AttachmentVersion[]>(`/api/v1/attachments/${id}/versions`),
  /**
   * Bring an older version back as a new one. Nothing is deleted: the
   * restore is itself a version, and the history keeps every step.
   */
  restoreVersion: (id: string, version: number) =>
    client.post<AttachmentVersion>(`/api/v1/attachments/${id}/versions/${version}/restore`),
  /** Give a figure a title; "" clears it. A change is a new figure version. */
  rename: (id: string, title: string) =>
    client.put<Attachment>(`/api/v1/attachments/${id}`, { title }),
  delete: (id: string) =>
    client.delete(`/api/v1/attachments/${id}`),
  listByArtifact: (artifactId: string) =>
    client.get<Attachment[]>(`/api/v1/artifacts/${artifactId}/attachments`),
  // Every figure in the project, for the citations that reach across
  // artifacts: the "##" menu offers them, and following one opens a figure
  // whose artifact the reader is not looking at.
  listByProject: (projectId: string) =>
    client.get<Attachment[]>(`/api/v1/projects/${projectId}/attachments`),
  /**
   * The file itself, as bytes.
   *
   * A PDF or a model is previewed from the app's own origin rather than by
   * pointing a frame at the API: the API refuses to be framed and serves
   * everything but a plain picture under a policy that permits nothing, which
   * is exactly what makes storing those formats safe. Fetching the bytes and
   * rendering them here needs none of that relaxed.
   */
  fetchFile: (id: string, version?: number) =>
    client.get<Blob>(`/api/v1/attachments/${id}/download${version ? `?version=${version}` : ''}`, {
      responseType: 'blob',
    }),
};
