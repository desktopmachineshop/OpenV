// Items.

import { fileName } from './files';
import * as paths from '../utils/paths';
import { API_URL, client, uploadConfig } from './http';
import type { Item, ItemPage, ItemQuery, Progress } from './types/items';

// The server's page size.
const PAGE = 100;

export const itemAPI = {
  list: (query: ItemQuery = { limit: PAGE }) => client.get<ItemPage>('/api/items', { params: query }),
  upload: (file: File, onProgress?: Progress) =>
    client.post<Item>(`/api/items/${fileName(file)}`, file, uploadConfig(onProgress)),
  link: (id: string) => `${API_URL}${paths.item(id)}`,
};
