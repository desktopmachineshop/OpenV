// The fixture's API client: tsdeclmove's test input, a small client.ts in
// the real one's shape. This header stays at the top of the barrel.

import axios, { AxiosInstance, AxiosRequestConfig } from 'axios';
import { fileName } from './files';
import type { Widget, makeWidget } from '../utils/widgets';
import * as paths from '../utils/paths';

// base lives in base.ts; re-exported so imports from the client keep working.
import { base } from './base';

export { base };

const API_URL = base();

const client: AxiosInstance = axios.create({
  baseURL: API_URL,
  timeout: 1000, // one second
});

// Tag every request.
client.interceptors.request.use((config) => {
  config.headers['X-Fixture'] = '1';
  return config;
});

/** How far an upload has got, as a percentage. */
export type Progress = (percent: number) => void;

const uploadConfig = (onProgress?: Progress): AxiosRequestConfig => ({
  timeout: 0,
  onUploadProgress: onProgress ? (event) => onProgress(event.loaded) : undefined,
});

client.interceptors.response.use(
  (response) => response,
  (error) => Promise.reject(error),
);

export type ItemStatus = 'draft' | 'done';
export interface Item {
  id: string;
  // Review state.
  status: ItemStatus;
}

// ---- Items -----------------------------------------------------------------

/** A page of items. */
export interface ItemPage {
  items: Item[];
  total: number; // the X-Total-Count header
}

// The server's page size.
const PAGE = 100;

/** What a list of items asks for: not exported, yet itemAPI's module uses it. */
interface ItemQuery {
  limit: number;
}

export const itemAPI = {
  list: (query: ItemQuery = { limit: PAGE }) => client.get<ItemPage>('/api/items', { params: query }),
  upload: (file: File, onProgress?: Progress) =>
    client.post<Item>(`/api/items/${fileName(file)}`, file, uploadConfig(onProgress)),
  link: (id: string) => `${API_URL}${paths.item(id)}`,
};

// Hand the browser a file to save.
const save = (data: BlobPart, name: string): string => {
  const url = URL.createObjectURL(new Blob([data]));
  return `${url}#${name}`;
};

export const METHODS = ['auto', 'manual'] as const;
export type Method = (typeof METHODS)[number];

export interface Widgets {
  widgets: Widget[];
  method: Method;
  example: ReturnType<typeof makeWidget>;
}

export const widgetAPI = {
  list: () => client.get<Widgets>('/api/widgets'),
  save: (widget: Widget) => save(JSON.stringify(widget), 'widget.json'),
  item: (id: string) => itemAPI.link(id),
};

export const exportAPI = {
  download: (id: string) => client.get(`/api/export/${id}`).then((r) => save(r.data, 'export')),
};

export default client;

// ---- Accounts --------------------------------------------------------------

/** What an account may do: exported only as a type, by the list below. */
interface Role {
  admin: boolean;
}

export type { Role };

export interface Account {
  id: string;
  items: Item[];
  role: Role;
}
export const accountAPI = { me: () => client.get<Account>('/api/me') }; // one line
