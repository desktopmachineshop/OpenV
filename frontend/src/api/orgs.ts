// Workspaces (orgs): members, teams, invitations, usage, limits and
// features, and who may reach a project (its members and team grants).
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

import { API_BASE_URL, client, uploadConfig } from './http';
import type {
  Org,
  OrgFeatures,
  OrgInvitation,
  OrgInvitationCreated,
  OrgMember,
  OrgTeam,
  OrgUsageSummary,
  ProjectMember,
  TeamGrant,
  WorkspaceLimits,
} from './types/orgs';

export const orgsAPI = {
  list: () => client.get<{ orgs: Org[]; active_org: string }>('/api/v1/orgs'),
  usage: (orgId: string, days?: number) =>
    client.get<OrgUsageSummary>(`/api/v1/orgs/${orgId}/usage`, {
      params: days ? { days } : {},
    }),
  /** Every limit this workspace is subject to, with usage where countable. */
  limits: (orgId: string) => client.get<WorkspaceLimits>(`/api/v1/orgs/${orgId}/limits`),
  create: (name: string) => client.post<Org>('/api/v1/orgs', { name }),
  get: (id: string) => client.get<Org>(`/api/v1/orgs/${id}`),
  update: (id: string, payload: Partial<Org>) => client.put<Org>(`/api/v1/orgs/${id}`, payload),
  // The caller's feature gates in the workspace, and the caller's own early
  // switch to the next stable release.
  features: (id: string) => client.get<OrgFeatures>(`/api/v1/orgs/${id}/features`),
  setStablePreview: (id: string, enabled: boolean) =>
    client.put<OrgFeatures>(`/api/v1/orgs/${id}/members/me/preview`, { enabled }),
  // Soft delete: the workspace is hidden and locked, restorable for 30 days,
  // then hard-deleted by the server's purge job.
  remove: (id: string) =>
    client.delete<{ deleted_at: string; purge_after: string }>(`/api/v1/orgs/${id}`),
  restore: (id: string) => client.post<Org>(`/api/v1/orgs/${id}/restore`),
  listDeleted: () => client.get<{ orgs: Org[] }>('/api/v1/orgs', { params: { deleted: 'true' } }),
  activate: (id: string) => client.post(`/api/v1/orgs/${id}/activate`),
  // Workspace logo (shown in the app and on download cover pages). PNG,
  // JPEG, GIF or WebP up to 2 MB; admins upload and remove, members view.
  uploadLogo: (id: string, file: File) => {
    const formData = new FormData();
    formData.append('file', file);
    return client.post<Org>(`/api/v1/orgs/${id}/logo`, formData, uploadConfig());
  },
  removeLogo: (id: string) => client.delete<Org>(`/api/v1/orgs/${id}/logo`),
  logoUrl: (id: string) => `${API_BASE_URL}/api/v1/orgs/${id}/logo`,
  // Pending invitations to the workspace (admin). An address with no account
  // is invited rather than refused, so this is where an admin watches for
  // people who have not arrived yet.
  invitations: {
    list: (orgId: string) => client.get<OrgInvitation[]>(`/api/v1/orgs/${orgId}/invitations`),
    // Same branch AND the same statuses as members.add: 201 with the
    // membership when the address already has an account, 409 when it is
    // already a member, 202 with the invitation and its one-time link when
    // it has no account.
    create: (orgId: string, email: string, role: string) =>
      client.post<OrgInvitationCreated | OrgMember>(`/api/v1/orgs/${orgId}/invitations`, {
        email,
        role,
      }),
    revoke: (orgId: string, invitationId: string) =>
      client.delete(`/api/v1/orgs/${orgId}/invitations/${invitationId}`),
  },
  members: {
    list: (orgId: string) => client.get<OrgMember[]>(`/api/v1/orgs/${orgId}/members`),
    // 201 with the membership when the address already has an account and
    // joined; 409 when it is already a member (change a role with setRole);
    // 202 with an OrgInvitationCreated body when it had no account and was
    // invited instead. invitations.create answers the same pair.
    add: (orgId: string, email: string, role: string) =>
      client.post<OrgInvitationCreated | OrgMember>(`/api/v1/orgs/${orgId}/members`, { email, role }),
    setRole: (orgId: string, userId: string, role: string) =>
      client.put(`/api/v1/orgs/${orgId}/members/${userId}`, { role }),
    remove: (orgId: string, userId: string) =>
      client.delete(`/api/v1/orgs/${orgId}/members/${userId}`),
  },
};

export const orgTeamsAPI = {
  list: (orgId: string) => client.get<OrgTeam[]>(`/api/v1/orgs/${orgId}/teams`),
  create: (orgId: string, payload: { name: string; description?: string }) =>
    client.post<OrgTeam>(`/api/v1/orgs/${orgId}/teams`, payload),
  update: (teamId: string, payload: { name?: string; description?: string }) =>
    client.put<OrgTeam>(`/api/v1/org-teams/${teamId}`, payload),
  remove: (teamId: string) => client.delete(`/api/v1/org-teams/${teamId}`),
  addMember: (teamId: string, userId: string) =>
    client.post(`/api/v1/org-teams/${teamId}/members/${userId}`),
  removeMember: (teamId: string, userId: string) =>
    client.delete(`/api/v1/org-teams/${teamId}/members/${userId}`),
};

export const projectTeamAccessAPI = {
  list: (projectId: string) =>
    client.get<TeamGrant[]>(`/api/v1/projects/${projectId}/team-access`),
  grant: (projectId: string, orgTeamId: string, role: string) =>
    client.put(`/api/v1/projects/${projectId}/team-access`, { org_team_id: orgTeamId, role }),
  revoke: (projectId: string, teamId: string) =>
    client.delete(`/api/v1/projects/${projectId}/team-access/${teamId}`),
};

export const membersAPI = {
  list: (projectId: string) =>
    client.get<ProjectMember[]>(`/api/v1/projects/${projectId}/members`),
  add: (projectId: string, email: string, role: string) =>
    client.post(`/api/v1/projects/${projectId}/members`, { email, role }),
  setRole: (projectId: string, userId: string, role: string) =>
    client.put(`/api/v1/projects/${projectId}/members/${userId}`, { role }),
  remove: (projectId: string, userId: string) =>
    client.delete(`/api/v1/projects/${projectId}/members/${userId}`),
};
