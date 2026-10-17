// Connected apps (REQ-44): the consent an AI client asks a member for, and
// the member's list of the apps they have connected, each revocable.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

import { client } from './http';
import type { ConnectedApp, OAuthConsentDecision, OAuthConsentRequest } from './types/connectedApps';

export const connectedAppsAPI = {
  // The app asking for consent: its name and where it sends the member back.
  consent: (clientId: string, redirectUri: string) =>
    client.get<OAuthConsentRequest>('/api/v1/oauth/consent', {
      params: { client_id: clientId, redirect_uri: redirectUri },
    }),
  // Approve or decline; the answer is where to send the browser, back to
  // the app with a code or with access_denied.
  decide: (decision: OAuthConsentDecision) => client.post<{ redirect_to: string }>('/api/v1/oauth/consent', decision),
  list: () => client.get<ConnectedApp[]>('/api/v1/me/connected-apps'),
  revoke: (id: string) => client.delete(`/api/v1/me/connected-apps/${id}`),
  // Change the workspaces an app reaches, without connecting it again.
  setWorkspaces: (id: string, orgIds: string[]) =>
    client.put<ConnectedApp>(`/api/v1/me/connected-apps/${id}/workspaces`, { org_ids: orgIds }),
};
