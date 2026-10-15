// Connected apps (REQ-44): the AI clients a member attaches to a workspace
// over the remote MCP endpoint, and the consent the app asks them for.

/** What the consent page shows about the app asking. */
export interface OAuthConsentRequest {
  client_id: string;
  client_name: string;
  /** The host the browser goes back to, for the member to recognise. */
  redirect_host: string;
}

/** The member's decision, as the consent page posts it. */
export interface OAuthConsentDecision {
  client_id: string;
  redirect_uri: string;
  code_challenge: string;
  code_challenge_method: string;
  state: string;
  /** 'openv:read', or 'openv:read openv:write'. */
  scope: string;
  org_id: string;
  approve: boolean;
}

/** One of the member's connected apps. */
export interface ConnectedApp {
  id: string;
  user_id: string;
  org_id: string;
  org_name: string;
  client_id: string;
  client_name: string;
  scope: string;
  created_at: string;
  last_used_at?: string;
}
