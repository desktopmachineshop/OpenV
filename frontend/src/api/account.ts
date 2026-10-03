// The signed-in account: sign-in and registration, invitations it
// accepts, passwords, the avatar and the default workspace.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

import { API_BASE_URL, client, uploadConfig } from './http';
import type {
  AuthConfig,
  DefaultWorkspace,
  InvitationPreview,
  RegisterResult,
  User,
} from './types/account';

// What a password form says about length while /auth/policy is in flight,
// and if it cannot be read at all. The server's own min_password_length
// replaces it as soon as the policy answers.
export const DEFAULT_MIN_PASSWORD_LENGTH = 8;

export const authAPI = {
  config: () => client.get<AuthConfig>('/api/v1/auth/config'),
  // inviteToken is the token from an invite link the form was opened with.
  // It is what grants the invited membership: the server treats holding the
  // link as proof the invited mailbox was read, and registering the address
  // without it joins nothing — the invitation then waits for its link, which
  // a signed-in account posts to POST /auth/invitations/accept. (Confirming
  // a verification link grants nothing: that address is one the account
  // asked the mail to be sent to, so it proves nothing about who was
  // invited.)
  //
  // `invitation` on the answer says what the token did — see
  // RegisterResult — and is absent when none was sent.
  register: (email: string, password: string, name: string, inviteToken?: string) =>
    client.post<RegisterResult>('/api/v1/auth/register', {
      email,
      password,
      name,
      ...(inviteToken ? { invite_token: inviteToken } : {}),
    }),
  login: (email: string, password: string) =>
    client.post<User>('/api/v1/auth/login', { email, password }),
  logout: () => client.post('/api/v1/auth/logout'),
  me: () => client.get<User>('/api/v1/auth/me'),
  // Sign-up email verification: confirm an emailed link (no session needed),
  // resend it, or send it to a corrected address (the address changes only
  // when that link is confirmed).
  verifyEmail: (token: string) => client.post<User>('/api/v1/auth/verify-email', { token }),
  resendVerification: () =>
    client.post<{ sent_to: string }>('/api/v1/auth/verify-email/resend', {}),
  changeVerificationEmail: (email: string) =>
    client.post<{ sent_to: string }>('/api/v1/auth/verify-email/change', { email }),
  // Password reset (REQ-158): ask for an emailed link (202 whether or not
  // the address has an account; 409 reset_email_unavailable when the server
  // cannot send mail), then spend the link with a new password (204; 400
  // reset_invalid or weak_password).
  requestPasswordReset: (email: string) =>
    client.post<{ sent_to: string }>('/api/v1/auth/password-reset', { email }),
  confirmPasswordReset: (token: string, newPassword: string) =>
    client.post('/api/v1/auth/password-reset/confirm', { token, new_password: newPassword }),
  // Registration policy on its own, for a caller that needs nothing else.
  // min_password_length is the server's own rule, so a password form states
  // the length that will actually be enforced rather than a copy of it.
  policy: () =>
    client.get<{ registration: 'open' | 'closed'; min_password_length?: number }>(
      '/api/v1/auth/policy'
    ),
  // Invite links: preview one (open — the holder has no session yet), or
  // accept it as the signed-in account. Accepting converts only when the
  // session's own address IS the invited one; any other address is refused
  // with 403 invitation_email_mismatch, and that body deliberately does not
  // name the invited address. `role` is what the account holds afterwards,
  // which is the role it already had when `already_member` is true — an
  // invitation never rewrites a membership.
  // The token goes in the body, never in the URL: an invite link is a
  // credential, and a path lands in access logs, proxy logs, browser history
  // and Referer headers.
  invitation: (token: string) =>
    client.post<InvitationPreview>('/api/v1/auth/invitations/preview', { token }),
  acceptInvitation: (token: string) =>
    client.post<{ org_id: string; org_name: string; role: string; already_member: boolean }>(
      '/api/v1/auth/invitations/accept',
      { token }
    ),
  // A reviewer share link (REQ-149), taken up by the signed-in account: it
  // becomes a reviewer of the project, or keeps the stronger role it holds.
  acceptShareLink: (token: string) =>
    client.post<{ project_id: string; project_name: string; role: string }>(
      '/api/v1/auth/share/accept',
      { token }
    ),
  googleLoginUrl: () => `${API_BASE_URL}/api/v1/auth/google`,
  oidcLoginUrl: () => `${API_BASE_URL}/api/v1/auth/oidc/login`,
  listUsers: () => client.get<User[]>('/api/v1/users'),
};

// The account's own password (REQ-99). A successful change ends every other
// session of the account; this browser's stays signed in.
export const passwordAPI = {
  change: (current_password: string, new_password: string) =>
    client.put('/api/v1/me/password', { current_password, new_password }),
};

// The account's own profile picture. PNG, JPEG, GIF or WebP up to 2 MB;
// both calls answer the updated user, whose avatar_url then points at the
// picture (or is empty again after a remove).
export const avatarAPI = {
  upload: (file: File) => {
    const formData = new FormData();
    formData.append('file', file);
    return client.post<User>('/api/v1/me/avatar', formData, uploadConfig());
  },
  remove: () => client.delete<User>('/api/v1/me/avatar'),
};

export const defaultWorkspaceAPI = {
  get: () => client.get<DefaultWorkspace>('/api/v1/me/default-workspace'),
  set: (orgId: string) =>
    client.put<DefaultWorkspace>('/api/v1/me/default-workspace', { org_id: orgId }),
};
