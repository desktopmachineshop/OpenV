// The types api/account.ts sends and receives.
// The signed-in account: sign-in and registration, invitations it
// accepts, passwords, the avatar and the default workspace.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

// ---------------------------------------------------------------------------
// Multi-agent suite APIs
// ---------------------------------------------------------------------------

export interface User {
  id: string;
  email: string;
  name: string;
  // A picture: the identity provider's URL, or — once one is uploaded — a
  // path on the API (resolve it with resolveAvatarUrl before rendering).
  avatar_url: string;
  // True when an uploaded picture is stored; avatarAPI.remove clears it.
  has_avatar?: boolean;
  auth_provider: string;
  is_admin: boolean;
  // Per-user email-notification opt-out (issue #187). Only has an effect when
  // the server has SMTP configured.
  email_notifications?: boolean;
  // Whether the account has proved control of its address. Only gates
  // anything when AuthConfig.email_verification_required is true.
  email_verified: boolean;
  email_verified_at?: string;
  created_at: string;
  // The workspace a sign-in lands in when the member has chosen one
  // (REQ-156); "" means the personal workspace.
  default_org_id?: string;
}

export interface AuthConfig {
  google_enabled: boolean;
  oidc_enabled: boolean;
  oidc_provider_name: string;
  email_verification_required: boolean;
  // Whether the sign-in page can email a password reset link (REQ-158).
  // Without a mailer, a platform admin mints the link instead.
  password_reset_email?: boolean;
  // Whether this deployment still has a public sign-up door (REQ-95). When
  // 'closed', new accounts arrive only by invitation or through SSO.
  registration?: 'open' | 'closed';
}

// What a sign-up that carried an invite token did with it. Absent when the
// form carried no token at all.
//
//   accepted       — the membership the link named was granted;
//   already_member — the account was in that workspace already and kept the
//                    role it had; the link is spent either way;
//   email_mismatch — the link is live but was issued to another address, so
//                    it granted nothing;
//   invalid        — unknown, revoked, spent or expired, including a link
//                    revoked in the moment between the sign-up being allowed
//                    and the membership being claimed.
export type InvitationOutcome = 'accepted' | 'already_member' | 'email_mismatch' | 'invalid';

// The created account, plus what its invite token did (see InvitationOutcome).
export interface RegisterResult extends User {
  invitation?: InvitationOutcome;
}

// What an invite link resolves to before the holder has any account: enough
// to prefill the sign-up form and name the workspace, nothing more.
export interface InvitationPreview {
  email: string;
  org_name: string;
  role: 'admin' | 'member';
  expires_at: string;
}

// The workspace a sign-in lands in (REQ-156): the member's own choice, a
// workspace they belong to, or "" for the personal workspace.
export interface DefaultWorkspace {
  org_id: string;
}
