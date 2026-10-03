/* The types api/accounts.ts sends and receives. */

import type { Item } from './items';

// ---- Accounts --------------------------------------------------------------

/** What an account may do: exported only as a type, by the list below. */
interface Role {
  admin: boolean;
}

export interface Account {
  id: string;
  items: Item[];
  role: Role;
}

export type { Role };
