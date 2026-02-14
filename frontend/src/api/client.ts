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
};

export default client;
