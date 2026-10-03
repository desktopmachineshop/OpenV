// What members share: shared products, share links and the open-source
// showcase.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

import type { SharedProductPayload, toSharePayload } from '../utils/randomProduct';
import { client } from './http';
import type {
  OpenSourceProject,
  ShareLink,
  ShareLinkRole,
  SharedProductSort,
  SharedProductVotes,
  SharedProject,
} from './types/community';

export const sharedProductsAPI = {
  list: (params?: { sort?: SharedProductSort; limit?: number }) =>
    client.get<SharedProductPayload[]>('/api/v1/shared-products', { params }),
  publish: (product: ReturnType<typeof toSharePayload>) =>
    client.post<SharedProductPayload>('/api/v1/shared-products', product),
  report: (id: string) => client.post(`/api/v1/shared-products/${id}/report`),
  // Voting is idempotent on both sides, hence PUT/DELETE rather than POST:
  // pressing an already-pressed arrow settles on the same count.
  vote: (id: string) => client.put<SharedProductVotes>(`/api/v1/shared-products/${id}/vote`),
  unvote: (id: string) => client.delete<SharedProductVotes>(`/api/v1/shared-products/${id}/vote`),
};

export const shareLinkAPI = {
  list: (projectId: string) => client.get<ShareLink[]>(`/api/v1/projects/${projectId}/share-links`),
  create: (projectId: string, role: ShareLinkRole, label: string, expiresAt?: string) =>
    client.post<ShareLink>(`/api/v1/projects/${projectId}/share-links`, {
      role,
      label,
      expires_at: expiresAt || null,
    }),
  revoke: (id: string) => client.delete(`/api/v1/share-links/${id}`),
  // Open a link without a session. The token is in the path here, and only
  // here: the link is the URL somebody was handed, and the API redacts it
  // from its own log.
  open: (token: string) => client.get<SharedProject>(`/api/v1/public/share/${token}`),
};

export const openSourceAPI = {
  list: () => client.get<OpenSourceProject[]>('/api/v1/public/open-source/projects'),
  get: (projectId: string) =>
    client.get<SharedProject>(`/api/v1/public/open-source/projects/${projectId}`),
};
