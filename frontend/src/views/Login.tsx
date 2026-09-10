import React, { useEffect, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { InvitationPreview, authAPI } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';

// Login renders email/password sign-in plus optional Google SSO, and a
// register mode. The first registered user becomes the admin.
//
// Two things can close or redirect that door. A deployment with
// registration: 'closed' has no sign-up form at all (REQ-95) — the only ways
// in are an invitation and SSO. And an invite link (?invite=<token>) opens
// the sign-up form with the invited address already filled in; if the person
// is already signed in, the link joins them to the workspace instead.
export const Login: React.FC = () => {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const setCurrentUser = useAppStore((s) => s.setCurrentUser);
  const setEmailVerificationRequired = useAppStore((s) => s.setEmailVerificationRequired);
  const [verificationRequired, setVerificationRequired] = useState(false);
  const inviteToken = searchParams.get('invite') || '';
  // The landing page links straight to registration with ?mode=register, and
  // an invite link means registration too — that is what the link is for.
  const [mode, setMode] = useState<'login' | 'register'>(
    searchParams.get('mode') === 'register' || inviteToken ? 'register' : 'login'
  );
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [name, setName] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [googleEnabled, setGoogleEnabled] = useState(false);
  const [oidcEnabled, setOidcEnabled] = useState(false);
  const [oidcName, setOidcName] = useState('SSO');
  const [registrationClosed, setRegistrationClosed] = useState(false);
  const [invite, setInvite] = useState<InvitationPreview | null>(null);
  const [inviteError, setInviteError] = useState('');

  useEffect(() => {
    authAPI
      .config()
      .then((res) => {
        setGoogleEnabled(res.data.google_enabled);
        setOidcEnabled(res.data.oidc_enabled);
        if (res.data.oidc_provider_name) setOidcName(res.data.oidc_provider_name);
        setVerificationRequired(!!res.data.email_verification_required);
        setEmailVerificationRequired(!!res.data.email_verification_required);
        setRegistrationClosed(res.data.registration === 'closed');
      })
      .catch(() => {
        setGoogleEnabled(false);
        setOidcEnabled(false);
      });
    // Already signed in? An invite link joins this account to the workspace
    // it names; otherwise go straight to projects.
    authAPI
      .me()
      .then(async (res) => {
        setCurrentUser(res.data);
        if (inviteToken) {
          // A link that no longer works is not worth blocking on: the person
          // is signed in either way.
          await authAPI.acceptInvitation(inviteToken).catch(() => {});
        }
        navigate('/projects');
      })
      .catch(() => {});
  }, [navigate, setCurrentUser, setEmailVerificationRequired, inviteToken]);

  // Resolve the invite link so the form can name the workspace and prefill
  // the address it was sent to.
  useEffect(() => {
    if (!inviteToken) return;
    let cancelled = false;
    authAPI
      .invitation(inviteToken)
      .then((res) => {
        if (cancelled) return;
        setInvite(res.data);
        setEmail(res.data.email);
        setMode('register');
      })
      .catch(() => {
        if (!cancelled) setInviteError('This invitation link is invalid or has expired.');
      });
    return () => {
      cancelled = true;
    };
  }, [inviteToken]);

  // An invitation is a door of its own: the sign-up form stays available on
  // a closed deployment when the link checks out.
  const signUpAvailable = !registrationClosed || !!invite;
  // Whatever ?mode= asked for, there is no sign-up form where there is no
  // sign-up: the view falls back to signing in.
  const activeMode: 'login' | 'register' = signUpAvailable ? mode : 'login';

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setBusy(true);
    try {
      const res =
        activeMode === 'login'
          ? await authAPI.login(email, password)
          : await authAPI.register(email, password, name);
      setCurrentUser(res.data);
      // On a server that sends verification links, an account that has not
      // clicked its link lands on the wall rather than the app.
      navigate(verificationRequired && !res.data.email_verified ? '/verify-email' : '/projects');
    } catch (err: any) {
      setError(
        apiErrorMessage(err, activeMode === 'login' ? 'Sign-in failed' : 'Registration failed')
      );
    } finally {
      setBusy(false);
    }
  };

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
          {activeMode === 'login' ? 'Sign in to your workspace' : 'Create your account'}
        </p>
        {invite && (
          <div
            style={{
              background: 'var(--tint-blue)',
              border: '1px solid var(--accent)',
              borderRadius: 4,
              padding: '10px 12px',
              marginBottom: 16,
              fontSize: 13,
              color: 'var(--text)',
            }}
          >
            You have been invited to <b>{invite.org_name}</b> as {invite.role}. Create your account
            with {invite.email} to join.
          </div>
        )}
        {inviteError && (
          <div style={{ color: 'var(--danger)', fontSize: 13, marginBottom: 12 }}>{inviteError}</div>
        )}
        <form onSubmit={submit}>
          {activeMode === 'register' && (
            <div className="form-group" style={{ marginBottom: 12 }}>
              <input
                type="text"
                placeholder="Your name"
                autoComplete="name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                style={{ width: '100%', padding: 10, boxSizing: 'border-box' }}
              />
            </div>
          )}
          <div className="form-group" style={{ marginBottom: 12 }}>
            <input
              type="email"
              placeholder="Email"
              autoComplete="email"
              inputMode="email"
              value={email}
              required
              onChange={(e) => setEmail(e.target.value)}
              style={{ width: '100%', padding: 10, boxSizing: 'border-box' }}
            />
          </div>
          <div className="form-group" style={{ marginBottom: 16 }}>
            <input
              type="password"
              placeholder={activeMode === 'register' ? 'Password (min 8 characters)' : 'Password'}
              autoComplete={activeMode === 'register' ? 'new-password' : 'current-password'}
              value={password}
              required
              onChange={(e) => setPassword(e.target.value)}
              style={{ width: '100%', padding: 10, boxSizing: 'border-box' }}
            />
          </div>
          {error && (
            <div style={{ color: 'var(--danger)', fontSize: 13, marginBottom: 12 }}>{String(error)}</div>
          )}
          <button className="button" type="submit" disabled={busy} style={{ width: '100%' }}>
            {busy ? 'Please wait…' : activeMode === 'login' ? 'Sign in' : 'Create account'}
          </button>
        </form>
        {oidcEnabled && (
          <a
            href={authAPI.oidcLoginUrl()}
            className="button-secondary button"
            style={{ display: 'block', textAlign: 'center', marginTop: 12, textDecoration: 'none' }}
          >
            Sign in with {oidcName}
          </a>
        )}
        {googleEnabled ? (
          <a
            href={authAPI.googleLoginUrl()}
            className="button-secondary button"
            style={{ display: 'block', textAlign: 'center', marginTop: 12, textDecoration: 'none' }}
          >
            Sign in with Google
          </a>
        ) : (
          !oidcEnabled &&
          activeMode === 'login' && (
            <div style={{ marginTop: 12, fontSize: 11, textAlign: 'center', color: 'var(--neutral-mid)' }}>
              Google sign-in is available once the server is configured with a Google OAuth
              client (see docs/agents.md).
            </div>
          )
        )}
        <div style={{ margin: '20px 0 0', borderTop: '1px solid var(--neutral-soft)', paddingTop: 16 }}>
          {!signUpAvailable ? (
            <div style={{ fontSize: 13, textAlign: 'center', color: 'var(--text-muted)' }}>
              Registration is closed; ask a workspace admin for an invitation.
            </div>
          ) : activeMode === 'login' ? (
            <button
              onClick={() => setMode('register')}
              className="button-secondary button"
              style={{ width: '100%' }}
              type="button"
            >
              Create a new account
            </button>
          ) : (
            <button
              onClick={() => setMode('login')}
              className="button-secondary button"
              style={{ width: '100%' }}
              type="button"
            >
              I already have an account — sign in
            </button>
          )}
        </div>
      </div>
    </div>
  );
};
