// The platform itself: the release a workspace runs and its notes, and
// the site administrator's views of workspaces and users. A declaration
// the F1 spec does not name joins this module (or its types module).
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

import { client } from './http';
import type { Org } from './types/orgs';
import type { AdminUser, AdminWorkspace, ReleaseInfo } from './types/platform';
import type { WorkspacePlan } from '../generated/contract';

export const releaseAPI = {
  current: () => client.get<ReleaseInfo>('/api/v1/release'),
};

// Every plan a workspace can be on (Go's orgs plans). `legacy` marks free
// and team, the names that shipped first for Single User and Business: rows
// in the wild still carry them, so a workspace on one is shown by name, but
// none is offered as a plan to move a workspace onto. Each value is a plan of
// the generated contract's WorkspacePlan (refactor plan X4b).
export const PLANS: { value: string; label: string; legacy?: boolean }[] = [
  { value: 'single', label: 'Single User' },
  { value: 'business_lite', label: 'Business Lite' },
  { value: 'business', label: 'Business' },
  { value: 'enterprise', label: 'Enterprise' },
  { value: 'open_source', label: 'Open source' },
  { value: 'self_host', label: 'Self-hosted' },
  { value: 'free', label: 'Free (legacy Single User)', legacy: true },
  { value: 'team', label: 'Team (legacy Business)', legacy: true },
] satisfies { value: WorkspacePlan; label: string; legacy?: boolean }[];

export const adminAPI = {
  workspaces: () => client.get<AdminWorkspace[]>('/api/v1/admin/workspaces'),
  users: () => client.get<AdminUser[]>('/api/v1/admin/users'),
  setPlan: (orgId: string, plan: string) => client.put<Org>(`/api/v1/orgs/${orgId}/plan`, { plan }),
  setAdmin: (userId: string, isAdmin: boolean) =>
    client.put<AdminUser>(`/api/v1/admin/users/${userId}/admin`, { is_admin: isAdmin }),
  // Mint a password reset link for an account and get it back once
  // (REQ-158). Nothing is emailed: the admin hands the link over.
  issuePasswordReset: (userId: string) =>
    client.post<{ link: string; expires_at: string }>(`/api/v1/admin/users/${userId}/password-reset`, {}),
};
