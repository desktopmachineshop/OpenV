// The platform itself: the release a workspace runs and its notes, and
// the site administrator's views of workspaces and users. A declaration
// the F1 spec does not name joins this module (or its types module).
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

import { client } from './http';
import type { Org } from './types/orgs';
import type { AdminUser, AdminWorkspace, ReleaseInfo } from './types/platform';

export const releaseAPI = {
  current: () => client.get<ReleaseInfo>('/api/v1/release'),
};

export const PLANS: { value: string; label: string }[] = [
  { value: 'single', label: 'Single User' },
  { value: 'business_lite', label: 'Business Lite' },
  { value: 'business', label: 'Business' },
  { value: 'enterprise', label: 'Enterprise' },
  { value: 'open_source', label: 'Open source' },
  { value: 'self_host', label: 'Self-hosted' },
];

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
