// Plans and prices, and a workspace's billing: checkout, plan changes
// and the billing portal.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

import { client } from './http';
import type { BillingState, PublicPlans } from './types/billing';

export const billingAPI = {
  publicPlans: () => client.get<PublicPlans>('/api/v1/public/plans'),
  state: (orgId: string) => client.get<BillingState>(`/api/v1/orgs/${orgId}/billing`),
  /** Starts a hosted checkout; the answer is the URL to send the browser to. */
  checkout: (orgId: string, plan: string, interval: string, currency?: string) =>
    client.post<{ url: string }>(`/api/v1/orgs/${orgId}/billing/checkout`, { plan, interval, currency: currency || '' }),
  /** Moves the live subscription to another plan or interval in place. */
  change: (orgId: string, plan: string, interval: string) =>
    client.post<BillingState>(`/api/v1/orgs/${orgId}/billing/change`, { plan, interval }),
  /** The provider's self-service portal; the answer is a URL. */
  portal: (orgId: string) => client.post<{ url: string }>(`/api/v1/orgs/${orgId}/billing/portal`, {}),
  /** Re-reads the subscription now; with a session id, binds a just-completed checkout. */
  refresh: (orgId: string, sessionId?: string) =>
    client.post<BillingState>(`/api/v1/orgs/${orgId}/billing/refresh`, sessionId ? { session_id: sessionId } : {}),
};
