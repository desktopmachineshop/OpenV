// The fixture's API client: tsdeclmove's test input, a small client.ts in
// the real one's shape. This header stays at the top of the barrel.

// The fixture's barrel: every export of the client, from the modules that
// hold it now.

// base lives in base.ts; re-exported so imports from the client keep working.
import { base } from './base';
import { client } from './http';
import type { Role } from './types/accounts';

export { base };

export default client;

export type { Role };

export type { Item, ItemPage, ItemStatus, Progress } from './types/items';
export type * from './types/widgets';
export type { Account } from './types/accounts';
export * from './items';
export * from './widgets';
export * from './accounts';
