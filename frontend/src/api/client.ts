import axios, { AxiosInstance } from 'axios';

const API_BASE_URL = process.env.REACT_APP_API_URL || 'http://localhost:8080';

console.log('API_BASE_URL:', API_BASE_URL);

const client: AxiosInstance = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
  timeout: 10000, // 10 second timeout
});

// Add response interceptor for better error handling
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
  name: string;
  description: string;
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
};

export const linkAPI = {
  create: (payload: Partial<Link>) =>
    client.post<Link>('/api/v1/links', payload),
  get: (id: string) =>
    client.get<Link>(`/api/v1/links/${id}`),
  list: (projectId: string) =>
    client.get<Link[]>('/api/v1/links', { params: { project_id: projectId } }),
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

export default client;
