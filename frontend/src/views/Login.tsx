import React, { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { authAPI } from '../api/client';
import { useAppStore } from '../state/store';

// Login renders email/password sign-in plus optional Google SSO, and a
// register mode. The first registered user becomes the admin.
export const Login: React.FC = () => {
  const navigate = useNavigate();
  const setCurrentUser = useAppStore((s) => s.setCurrentUser);
  const [mode, setMode] = useState<'login' | 'register'>('login');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [name, setName] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [googleEnabled, setGoogleEnabled] = useState(false);

  useEffect(() => {
    authAPI
      .config()
      .then((res) => setGoogleEnabled(res.data.google_enabled))
      .catch(() => setGoogleEnabled(false));
    // Already signed in? Go straight to projects.
    authAPI
      .me()
      .then((res) => {
        setCurrentUser(res.data);
        navigate('/projects');
      })
      .catch(() => {});
  }, [navigate, setCurrentUser]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setBusy(true);
    try {
      const res =
        mode === 'login'
          ? await authAPI.login(email, password)
          : await authAPI.register(email, password, name);
      setCurrentUser(res.data);
      navigate('/projects');
    } catch (err: any) {
      setError(err.response?.data || err.message || 'Sign-in failed');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: 'var(--sidebar-bg)',
      }}
    >
      <div className="card" style={{ width: 380, padding: 32, background: 'var(--surface)', borderRadius: 8 }}>
        <h1 style={{ margin: 0, fontSize: 26, color: 'var(--text)' }}>OpenV</h1>
        <p style={{ color: 'var(--text-muted)', marginTop: 4, marginBottom: 24, fontSize: 14 }}>
          {mode === 'login' ? 'Sign in to your workspace' : 'Create your account'}
        </p>
        <form onSubmit={submit}>
          {mode === 'register' && (
            <div className="form-group" style={{ marginBottom: 12 }}>
              <input
                type="text"
                placeholder="Your name"
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
              value={email}
              required
              onChange={(e) => setEmail(e.target.value)}
              style={{ width: '100%', padding: 10, boxSizing: 'border-box' }}
            />
          </div>
          <div className="form-group" style={{ marginBottom: 16 }}>
            <input
              type="password"
              placeholder={mode === 'register' ? 'Password (min 8 characters)' : 'Password'}
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
            {busy ? 'Please wait…' : mode === 'login' ? 'Sign in' : 'Create account'}
          </button>
        </form>
        {googleEnabled ? (
          <a
            href={authAPI.googleLoginUrl()}
            className="button-secondary button"
            style={{ display: 'block', textAlign: 'center', marginTop: 12, textDecoration: 'none' }}
          >
            Sign in with Google
          </a>
        ) : (
          mode === 'login' && (
            <div style={{ marginTop: 12, fontSize: 11, textAlign: 'center', color: 'var(--neutral-mid)' }}>
              Google sign-in is available once the server is configured with a Google OAuth
              client (see docs/agents.md).
            </div>
          )
        )}
        <div style={{ margin: '20px 0 0', borderTop: '1px solid var(--neutral-soft)', paddingTop: 16 }}>
          {mode === 'login' ? (
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
