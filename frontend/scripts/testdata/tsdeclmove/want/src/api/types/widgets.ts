// The types api/widgets.ts sends and receives.

import type { Widget, makeWidget } from '../../utils/widgets';
import type { METHODS } from '../widgets';

export type Method = (typeof METHODS)[number];

export interface Widgets {
  widgets: Widget[];
  method: Method;
  example: ReturnType<typeof makeWidget>;
}
