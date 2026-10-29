import React, { useEffect, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { InvitationPreview, authAPI } from '../api/client';
import { rememberReturnTo } from '../utils/returnTo';

// The sign-in page's single sign-on: the provider buttons beside the
// password form, and, on a deployment that signs everyone in through its
// provider only (GET /auth/config's oidc_only, OPENV_OIDC_ONLY), the page
// that replaces the form and sends the browser straight to the provider.

/** The name DMS SaaS deployments give their single sign-on: DMSid, one login for every product. */
const DMSID = 'DMSid';

const DmsidNote: React.FC<{ name: string }> = ({ name }) =>
  name === DMSID ? (
    <div style={{ marginTop: 6, fontSize: 12, textAlign: 'center', color: 'var(--neutral-mid)' }}>
      One login, lots of services: your DMSid signs you in to OpenV, OpenD and every other DMS
      SaaS product.
    </div>
  ) : null;

/** The provider buttons under the password form. */
export const SsoButtons: React.FC<{
  oidcEnabled: boolean;
  oidcName: string;
  googleEnabled: boolean;
  signingIn: boolean;
  returnTo: string;
}> = ({ oidcEnabled, oidcName, googleEnabled, signingIn, returnTo }) => (
  <>
    {oidcEnabled && (
      <a
        href={authAPI.oidcLoginUrl()}
        onClick={() => rememberReturnTo(returnTo)}
        className="button-secondary button"
        style={{ display: 'block', textAlign: 'center', marginTop: 12, textDecoration: 'none' }}
      >
        Sign in with {oidcName}
      </a>
    )}
    {oidcEnabled && <DmsidNote name={oidcName} />}
    {googleEnabled ? (
      <a
        href={authAPI.googleLoginUrl()}
        onClick={() => rememberReturnTo(returnTo)}
        className="button-secondary button"
        style={{ display: 'block', textAlign: 'center', marginTop: 12, textDecoration: 'none' }}
      >
        Sign in with Google
      </a>
    ) : (
      !oidcEnabled &&
      signingIn && (
        <div style={{ marginTop: 12, fontSize: 11, textAlign: 'center', color: 'var(--neutral-mid)' }}>
          Google sign-in is available once the server is configured with a Google OAuth client
          (see docs/agents.md).
        </div>
      )
    )}
  </>
);

/**
 * What a sign-on that came back to the sign-in page with ?error=<code> says
 * (the OIDC callback's codes: sso_cancelled, sso_unverified, sso_conflict,
 * sso_failed). An unknown code reads as the general one.
 */
export const ssoErrorMessage = (code: string, name: string): string => {
  switch (code) {
    case 'sso_cancelled':
      return `Signing in through ${name} was cancelled.`;
    case 'sso_unverified':
      return `${name} has not confirmed your email address. Confirm it there, then try again.`;
    case 'sso_conflict':
      return `An OpenV account already uses this address with another way of signing in, so ${name} cannot sign in to it. Ask your OpenV administrator for help.`;
    default:
      return `Signing in through ${name} did not finish.`;
  }
};

/** The sign-in page's notice for ?error=, beside the password form, with a way to try again. */
export const SsoError: React.FC<{ name: string }> = ({ name }) => {
  const [searchParams] = useSearchParams();
  const code = searchParams.get('error');
  if (!code) return null;
  return (
    <div role="alert" style={{ color: 'var(--danger)', fontSize: 13, marginBottom: 12 }}>
      {ssoErrorMessage(code, name)}{' '}
      <a href={authAPI.oidcLoginUrl()}>Try again</a>
    </div>
  );
};

/** While the sign-in methods load: the page's background and nothing else, so no form flashes. */
export const SignInLoading: React.FC = () => (
  <div className="app-shell" aria-busy="true" style={{ background: 'var(--sidebar-bg)' }} />
);

/** Under the reset request where single sign-on is the only way in: what the password is for. */
export const ResetForLinkNote: React.FC<{ name: string }> = ({ name }) => (
  <p style={{ fontSize: 13, color: 'var(--text-muted)', lineHeight: 1.5, marginTop: 0 }}>
    The password connects {name} to your OpenV account once; after that you sign in with {name}.
  </p>
);

// The browser is sent to the provider on arrival at most once a minute. A
// sign-on that comes back without a session (a refused cookie, a provider
// that answers at once) would otherwise go round and round; the second
// arrival, and a Back from the provider's page, shows a button instead.
const SENT_KEY = 'openv_sso_sent_at';
const RESEND_AFTER_MS = 60_000;

const sentRecently = (): boolean => {
  try {
    const at = Number(sessionStorage.getItem(SENT_KEY));
    return at > 0 && Date.now() - at < RESEND_AFTER_MS;
  } catch {
    return false;
  }
};

const noteSent = (): void => {
  try {
    sessionStorage.setItem(SENT_KEY, String(Date.now()));
  } catch {
    // Without storage the page still sends; the provider's own page stops a loop.
  }
};

const notice: React.CSSProperties = {
  background: 'var(--tint-blue)',
  border: '1px solid var(--accent)',
  borderRadius: 4,
  padding: '10px 12px',
  marginBottom: 16,
  fontSize: 13,
  color: 'var(--text)',
};

/**
 * The sign-in page where single sign-on is the only way in. Once the session
 * check has said nobody is signed in (`ready`) it sends the browser to the
 * provider, to its sign-up page for `signUp`, and comes back to `returnTo`.
 * It stays put, with a button, after a sign-out (?signed_out=1), after a
 * sign-on error (?error=, which it explains), after the password the link
 * step asks for was reset (?reset=done), while a provider is being linked
 * to a password account (?link=), on an invite link, which it names first,
 * and when it
 * sent the browser a moment ago (sentRecently).
 */
export const SsoOnlySignIn: React.FC<{
  name: string;
  signUp: boolean;
  returnTo: string;
  ready: boolean;
  invite: InvitationPreview | null;
  problem: string;
}> = ({ name, signUp, returnTo, ready, invite, problem }) => {
  const [searchParams] = useSearchParams();
  const signedOut = searchParams.get('signed_out') === '1';
  const ssoError = searchParams.get('error') || '';
  // Back from setting the OpenV password the link step asks for.
  const resetDone = searchParams.get('reset') === 'done';
  const held =
    signedOut ||
    !!ssoError ||
    resetDone ||
    !!problem ||
    !!searchParams.get('link') ||
    !!searchParams.get('invite');
  const [stalled, setStalled] = useState(false);
  const url = authAPI.oidcLoginUrl(signUp);
  const go = () => {
    rememberReturnTo(returnTo);
    noteSent();
    window.location.assign(url);
  };
  const auto = ready && !held;
  useEffect(() => {
    if (!auto) return;
    if (sentRecently()) {
      setStalled(true);
      return;
    }
    go();
    // go reads only props that are fixed once the page has loaded.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [auto]);
  const failed = !!ssoError || stalled;

  return (
    <div
      className="app-shell"
      style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: 'var(--sidebar-bg)',
        padding: 16,
        boxSizing: 'border-box',
        overflowY: 'auto',
      }}
    >
      <div
        className="card"
        style={{ width: '100%', maxWidth: 380, padding: 32, background: 'var(--surface)', borderRadius: 8, boxSizing: 'border-box', margin: 0 }}
      >
        <Link to="/" style={{ fontSize: 13, color: 'var(--text-muted)', textDecoration: 'none' }}>
          ← About OpenV
        </Link>
        <h1 style={{ margin: '8px 0 0', fontSize: 26, color: 'var(--text)' }}>OpenV</h1>
        <p style={{ color: 'var(--text-muted)', marginTop: 4, marginBottom: 24, fontSize: 14 }}>
          {signUp ? 'Create your account' : 'Sign in to your workspace'}
        </p>
        {invite && (
          <div style={notice}>
            You have been invited to <b>{invite.org_name}</b> as {invite.role}. Continue with {name}{' '}
            as {invite.email} to join.
          </div>
        )}
        {problem && (
          <div role="alert" style={{ color: 'var(--danger)', fontSize: 13, marginBottom: 12 }}>
            {problem}
          </div>
        )}
        {failed && (
          <div role="alert" style={{ color: 'var(--danger)', fontSize: 13, marginBottom: 12 }}>
            {ssoErrorMessage(ssoError, name)}
          </div>
        )}
        {resetDone && !failed && (
          <p role="status" style={{ fontSize: 13, color: 'var(--text-body)', marginTop: 0 }}>
            Your OpenV password is set. Continue with {name}, and enter it when you are asked to
            connect your account.
          </p>
        )}
        {signedOut && !failed && (
          <p role="status" style={{ fontSize: 13, color: 'var(--text-body)', marginTop: 0 }}>
            You are signed out of OpenV.
          </p>
        )}
        {!held && !failed ? (
          <p role="status" style={{ fontSize: 13, color: 'var(--text-body)', marginTop: 0 }}>
            Taking you to {name}…{' '}
            <a href={url} onClick={go}>
              Continue to {name}
            </a>
          </p>
        ) : (
          <button className="button" type="button" style={{ width: '100%' }} onClick={go}>
            {failed || problem ? 'Try again' : `Continue with ${name}`}
          </button>
        )}
        <DmsidNote name={name} />
      </div>
    </div>
  );
};
