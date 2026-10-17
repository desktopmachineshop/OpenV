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
  /** The workspaces the member chose for the app. */
  org_ids: string[];
  approve: boolean;
}

/** A workspace a connected app reaches. */
export interface ConnectedAppWorkspace {
  org_id: string;
  org_name: string;
}

/** One of the member's connected apps. */
export interface ConnectedApp {
  id: string;
  user_id: string;
  /** The workspace the app was first connected to; see workspaces for what it reaches. */
  org_id: string;
  org_name: string;
  /** Every workspace the app reaches. */
  workspaces: ConnectedAppWorkspace[];
  client_id: string;
  client_name: string;
  scope: string;
  created_at: string;
  last_used_at?: string;
}
