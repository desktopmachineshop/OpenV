// The types api/items.ts sends and receives.

/** How far an upload has got, as a percentage. */
export type Progress = (percent: number) => void;

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

/** What a list of items asks for: not exported, yet itemAPI's module uses it. */
interface ItemQuery {
  limit: number;
}

export type { ItemQuery };
