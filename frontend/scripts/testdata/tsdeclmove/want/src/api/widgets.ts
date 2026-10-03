// Widgets and exports.

import type { Widget } from '../utils/widgets';
import { client, save } from './http';
import type { Widgets } from './types/widgets';
import { itemAPI } from './items';

export const METHODS = ['auto', 'manual'] as const;

export const widgetAPI = {
  list: () => client.get<Widgets>('/api/widgets'),
  save: (widget: Widget) => save(JSON.stringify(widget), 'widget.json'),
  item: (id: string) => itemAPI.link(id),
};

export const exportAPI = {
  download: (id: string) => client.get(`/api/export/${id}`).then((r) => save(r.data, 'export')),
};
