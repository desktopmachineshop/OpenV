import React, { useEffect, useState } from 'react';
import { Link, Navigate, useNavigate, useSearchParams } from 'react-router-dom';
import { DEFAULT_MIN_PASSWORD_LENGTH, authAPI } from '../api/client';
import { apiErrorCode, apiErrorMessage } from '../api/errors';

// The landing for a password reset link (REQ-158): ?token= from the email
// or from a platform admin. It asks for the new password twice, spends the
// link, and sends the person to sign in on a page with no token in its
// address bar. Setting the password ended every session of the account,
// including any this browser held, so signing in again is the only way on.

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
  maxWidth: 380,
  padding: 32,
  background: 'var(--surface)',
  borderRadius: 8,
  boxSizing: 'border-box',
  margin: 0,
};

export const ResetPassword: React.FC = () => {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const token = searchParams.get('token') || '';
  const [password, setPassword] = useState('');
  const [again, setAgain] = useState('');
  const [error, setError] = useState('');
  const [invalid, setInvalid] = useState(false);
  const [busy, setBusy] = useState(false);
  const [minPasswordLength, setMinPasswordLength] = useState(DEFAULT_MIN_PASSWORD_LENGTH);

  useEffect(() => {
    authAPI
      .policy()
      .then((res) => {
        if (res.data.min_password_length) setMinPasswordLength(res.data.min_password_length);
      })
      .catch(() => {});
  }, []);

  if (!token) return <Navigate to="/login?mode=forgot" replace />;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    if (password.length < minPasswordLength) {
      setError(`The password must be at least ${minPasswordLength} characters.`);
      return;
    }
    if (password !== again) {
      setError('The two passwords do not match.');
      return;
    }
    setBusy(true);
    try {
      await authAPI.confirmPasswordReset(token, password);
      navigate('/login?reset=done', { replace: true });
    } catch (err: any) {
      if (apiErrorCode(err) === 'reset_invalid') {
        setInvalid(true);
      } else {
        setError(apiErrorMessage(err, 'The password could not be set'));
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="app-shell" style={shell}>
      <div className="card" style={card}>
        <h1 style={{ margin: '0 0 8px', fontSize: 22, color: 'var(--text)' }}>
          {invalid ? 'This link did not work' : 'Choose a new password'}
        </h1>
        {invalid ? (
          <>
            <p style={{ color: 'var(--text-body)', fontSize: 14, lineHeight: 1.5 }}>
              This reset link is invalid or has expired. Links work once; an emailed one lasts an hour and
              one from an administrator lasts a day. Request a new one, or ask your OpenV administrator.
            </p>
            <Link
              to="/login?mode=forgot"
              className="button"
              style={{ display: 'block', textAlign: 'center', textDecoration: 'none' }}
            >
              Request a new link
            </Link>
          </>
        ) : (
          <form onSubmit={submit}>
            <p style={{ color: 'var(--text-body)', fontSize: 14, lineHeight: 1.5, marginTop: 0 }}>
              Setting it signs the account out everywhere; sign in again with the new password.
            </p>
            <div className="form-group" style={{ marginBottom: 12 }}>
              <input
                type="password"
                placeholder={`New password (min ${minPasswordLength} characters)`}
                autoComplete="new-password"
                value={password}
                required
                onChange={(e) => setPassword(e.target.value)}
                style={{ width: '100%', padding: 10, boxSizing: 'border-box' }}
              />
            </div>
            <div className="form-group" style={{ marginBottom: 16 }}>
              <input
                type="password"
                placeholder="New password again"
                autoComplete="new-password"
                value={again}
                required
                onChange={(e) => setAgain(e.target.value)}
                style={{ width: '100%', padding: 10, boxSizing: 'border-box' }}
              />
            </div>
            {error && <div style={{ color: 'var(--danger)', fontSize: 13, marginBottom: 12 }}>{error}</div>}
            <button className="button" type="submit" disabled={busy} style={{ width: '100%' }}>
              {busy ? 'Please wait…' : 'Set password'}
            </button>
          </form>
        )}
      </div>
    </div>
  );
};
