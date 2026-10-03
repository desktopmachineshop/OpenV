// The API client: everything src/api offers the app, re-exported from the
// modules that hold it (refactor plan F1). Code outside src/api imports
// from here only (K12), so a whole-module vi.mock('../api/client') still
// stubs every call. An endpoint goes in its area's module, api/<area>.ts,
// and its types in api/types/<area>.ts; the axios instance and its
// interceptors are in api/http.ts.

import { getAPIBaseURL, resolveAvatarUrl } from './baseURL';
import { client } from './http';

// getAPIBaseURL and resolveAvatarUrl live in baseURL.ts; re-exported so
// existing imports from the client keep working.
export { getAPIBaseURL, resolveAvatarUrl };

export default client;

export type * from './types/artifacts';
export type * from './types/projects';
export type * from './types/attachments';
export type * from './types/vv';
export type * from './types/guided';
export type * from './types/agents';
export type * from './types/account';
export type * from './types/orgs';
export type * from './types/billing';
export type * from './types/runners';
export type * from './types/community';
export type * from './types/notifications';
export type * from './types/platform';
export * from './artifacts';
export * from './projects';
export * from './attachments';
export * from './vv';
export * from './guided';
export * from './agents';
export * from './account';
export * from './orgs';
export * from './billing';
export * from './runners';
export * from './community';
export * from './notifications';
export * from './platform';
