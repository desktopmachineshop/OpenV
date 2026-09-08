import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Link, Navigate, useNavigate, useSearchParams } from 'react-router-dom';
import { authAPI } from '../api/client';
import { apiErrorMessage, retryAfterSeconds } from '../api/errors';
import { useAppStore } from '../state/store';

// Two jobs on one page. With ?token= in the URL it is the landing for the
// emailed link: it confirms the token and sends the person on. Without a
// token it is the wall a signed-in but unverified account sees instead of
// the app: the address the link went to, a resend, a way to correct a typo,
// and sign-out. The API refuses everything else until the link is clicked,
// so nothing here needs any other call to succeed.

const RESEND_COOLDOWN_SECONDS = 60;

const shell: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  background: 'var(--sidebar-bg)',
  padding: 16,
  boxSizing: 'border-box',
  overflowY: 'auto',
};

const card: React.CSSProperties = {
  width: '100%',
  maxWidth: 420,
  padding: 32,
  background: 'var(--surface)',
  borderRadius: 8,
  boxSizing: 'border-box',
  margin: 0,
};

const TokenLanding: React.FC<{ token: string }> = ({ token }) => {
  const navigate = useNavigate();
  const { currentUser, setCurrentUser } = useAppStore();
  const [state, setState] = useState<'verifying' | 'done' | 'failed'>('verifying');
  const [error, setError] = useState('');
  const started = useRef(false);

  useEffect(() => {
    if (started.current) return;
    started.current = true;
    authAPI
      .verifyEmail(token)
      .then((res) => {
        // The confirmed account is the signed-in one only when the link was
        // opened in the browser that holds the session.
        if (currentUser && currentUser.id === res.data.id) setCurrentUser(res.data);
        setState('done');
        // The spent token leaves the address bar and the history; the wall
        // then sends a signed-in account to the app and a visitor to sign in.
        navigate('/verify-email', { replace: true, state: { consumed: true } });
      })
      .catch((err) => {
        // A dead token stays in the URL: it is useless now, and the page has
        // to keep showing why the link did not work.
        setError(apiErrorMessage(err, 'The link could not be verified'));
        setState('failed');
      });
  }, [token, currentUser, setCurrentUser, navigate]);

  return (
    <div className="app-shell" style={shell}>
      <div className="card" style={card}>
        <h1 style={{ margin: '0 0 8px', fontSize: 22, color: 'var(--text)' }}>
          {state === 'verifying' ? 'Verifying your email…' : state === 'done' ? 'Email verified' : 'This link did not work'}
        </h1>
        {state === 'failed' && (
          <>
            <p style={{ color: 'var(--danger)', fontSize: 14 }}>{error}</p>
            <p style={{ color: 'var(--text-body)', fontSize: 14 }}>
              Links work once and expire after 24 hours. Sign in to request a new one.
            </p>
            <Link to="/login" className="button" style={{ display: 'block', textAlign: 'center', textDecoration: 'none' }}>
              Sign in
            </Link>
          </>
        )}
      </div>
    </div>
  );
};

const Wall: React.FC<{ consumed: boolean }> = ({ consumed }) => {
  const navigate = useNavigate();
  const { currentUser, setCurrentUser, emailVerificationRequired } = useAppStore();
  const [sentTo, setSentTo] = useState('');
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [cooldown, setCooldown] = useState(0);
  const [changing, setChanging] = useState(false);
  const [newEmail, setNewEmail] = useState('');
  const [checking, setChecking] = useState(false);

  // Count the resend cooldown down.
  useEffect(() => {
    if (cooldown <= 0) return;
    const id = window.setTimeout(() => setCooldown((c) => c - 1), 1000);
    return () => window.clearTimeout(id);
  }, [cooldown]);

  const refresh = useCallback(async () => {
    setChecking(true);
    try {
      const res = await authAPI.me();
      setCurrentUser(res.data);
      if (!res.data.email_verified) setNotice('Not verified yet. Click the link in the email, then try again.');
    } catch {
      setCurrentUser(null);
    } finally {
      setChecking(false);
    }
  }, [setCurrentUser]);

  // A link clicked in another tab or on the phone verifies this session too;
  // poll so the tab moves on without a manual check.
  useEffect(() => {
    if (!currentUser || currentUser.email_verified) return;
    const id = window.setInterval(() => {
      authAPI
        .me()
        .then((res) => {
          if (res.data.email_verified) setCurrentUser(res.data);
        })
        .catch(() => {});
    }, 15000);
    return () => window.clearInterval(id);
  }, [currentUser, setCurrentUser]);

  if (!currentUser) {
    return consumed ? (
      <div className="app-shell" style={shell}>
        <div className="card" style={card}>
          <h1 style={{ margin: '0 0 8px', fontSize: 22, color: 'var(--text)' }}>Email verified</h1>
          <p style={{ color: 'var(--text-body)', fontSize: 14 }}>Sign in to start using OpenV.</p>
          <Link to="/login" className="button" style={{ display: 'block', textAlign: 'center', textDecoration: 'none' }}>
            Sign in
          </Link>
        </div>
      </div>
    ) : (
      <Navigate to="/login" replace />
    );
  }
  if (currentUser.email_verified || !emailVerificationRequired) {
    return <Navigate to="/projects" replace />;
  }

  const send = async (email?: string) => {
    setError('');
    setNotice('');
    setBusy(true);
    try {
      const res = email
        ? await authAPI.changeVerificationEmail(email)
        : await authAPI.resendVerification();
      setSentTo(res.data.sent_to);
      setNotice(`Sent to ${res.data.sent_to}.`);
      setCooldown(RESEND_COOLDOWN_SECONDS);
      setChanging(false);
      setNewEmail('');
    } catch (err) {
      const wait = retryAfterSeconds(err);
      setError(
        wait
          ? `Too many emails requested. Try again in ${wait} seconds.`
          : apiErrorMessage(err, 'The email could not be sent')
      );
    } finally {
      setBusy(false);
    }
  };

  const signOut = async () => {
    try {
      await authAPI.logout();
    } finally {
      setCurrentUser(null);
      navigate('/login');
    }
  };

  const address = sentTo || currentUser.email;

  return (
    <div className="app-shell" style={shell}>
      <div className="card" style={card}>
        <h1 style={{ margin: '0 0 8px', fontSize: 22, color: 'var(--text)' }}>Check your inbox</h1>
        <p style={{ color: 'var(--text-body)', fontSize: 14, lineHeight: 1.5 }}>
          We sent a verification link to <strong style={{ overflowWrap: 'anywhere' }}>{address}</strong>. Click it to start
          using OpenV. The link is valid for 24 hours.
        </p>
        {notice && <p style={{ color: 'var(--success)', fontSize: 13 }}>{notice}</p>}
        {error && <p style={{ color: 'var(--danger)', fontSize: 13 }}>{error}</p>}

        <div style={{ display: 'flex', flexDirection: 'column', gap: 10, marginTop: 16 }}>
          <button className="button" type="button" disabled={busy || cooldown > 0} onClick={() => send()}>
            {cooldown > 0 ? `Resend email (${cooldown}s)` : busy ? 'Sending…' : 'Resend email'}
          </button>
          <button className="button-secondary button" type="button" disabled={checking} onClick={refresh}>
            {checking ? 'Checking…' : "I've clicked the link"}
          </button>
          {!changing ? (
            <button className="button-secondary button" type="button" onClick={() => setChanging(true)}>
              Use a different address
            </button>
          ) : (
            <form
              onSubmit={(e) => {
                e.preventDefault();
                if (newEmail.trim()) send(newEmail.trim());
              }}
              style={{ display: 'flex', flexDirection: 'column', gap: 8 }}
            >
              <input
                type="email"
                placeholder="New email address"
                autoComplete="email"
                inputMode="email"
                value={newEmail}
                required
                onChange={(e) => setNewEmail(e.target.value)}
                style={{ width: '100%', padding: 10, boxSizing: 'border-box' }}
              />
              <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                Your account keeps its current address until the new one is confirmed.
              </div>
              <div style={{ display: 'flex', gap: 8 }}>
                <button className="button" type="submit" disabled={busy || cooldown > 0} style={{ flex: 1 }}>
                  Send link there
                </button>
                <button className="button-secondary button" type="button" onClick={() => setChanging(false)} style={{ flex: 1 }}>
                  Cancel
                </button>
              </div>
            </form>
          )}
        </div>

        <div style={{ margin: '20px 0 0', borderTop: '1px solid var(--neutral-soft)', paddingTop: 16, textAlign: 'center' }}>
          <button
            type="button"
            onClick={signOut}
            style={{ background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 13, minHeight: 36 }}
          >
            Sign out
          </button>
        </div>
      </div>
    </div>
  );
};

export const VerifyEmail: React.FC = () => {
  const [searchParams] = useSearchParams();
  const token = searchParams.get('token') || '';
  // After the landing consumed a token it navigates here with a marker, so a
  // visitor with no session sees "verified, sign in" rather than the login
  // bounce.
  const consumed = typeof window !== 'undefined' && !!(window.history.state?.usr?.consumed);
  if (token) return <TokenLanding token={token} />;
  return <Wall consumed={consumed} />;
};
