import React, { useCallback, useEffect, useRef, useState } from 'react';
import { providerLoginsAPI, ProviderLogin } from '../../api/client';

interface Props {
  provider: string;
  loggedIn: boolean;
  onComplete: () => void;
  // Where the sign-in executes: 'workspace' (shared worker, default) or
  // 'user' (only the caller's personal runner — credential lands on their
  // own machine).
  target?: 'workspace' | 'user';
  // Optional heading override (default "Subscription sign-in").
  title?: string;
}

// ProviderConnectCard drives a CLI provider sign-in from the UI: it creates
// a login request, the host worker runs the vendor CLI's own login flow, and
// this component relays the auth URL (and paste-back code where the CLI
// needs one). Credentials are stored by the CLI on the host, never in OpenV.
export const ProviderConnectCard: React.FC<Props> = ({
  provider,
  loggedIn,
  onComplete,
  target = 'workspace',
  title = 'Subscription sign-in',
}) => {
  const [login, setLogin] = useState<ProviderLogin | null>(null);
  const [code, setCode] = useState('');
  const [codeSent, setCodeSent] = useState(false);
  const [error, setError] = useState('');
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const stopPolling = useCallback(() => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
  }, []);

  useEffect(() => stopPolling, [stopPolling]);

  const poll = useCallback(
    (id: string) => {
      stopPolling();
      pollRef.current = setInterval(async () => {
        try {
          const res = await providerLoginsAPI.get(id);
          setLogin(res.data);
          if (['completed', 'failed', 'cancelled'].includes(res.data.status)) {
            stopPolling();
            if (res.data.status === 'completed') onComplete();
          }
        } catch {
          // transient; keep polling
        }
      }, 2000);
    },
    [onComplete, stopPolling]
  );

  const start = async () => {
    setError('');
    setCode('');
    setCodeSent(false);
    try {
      const res = await providerLoginsAPI.start(provider, target);
      setLogin(res.data);
      poll(res.data.id);
    } catch (err: any) {
      setError(String(err.response?.data || err.message));
    }
  };

  const submitCode = async () => {
    if (!login || !code.trim()) return;
    try {
      await providerLoginsAPI.submitCode(login.id, code.trim());
      setCodeSent(true);
    } catch (err: any) {
      setError(String(err.response?.data || err.message));
    }
  };

  const cancel = async () => {
    if (!login) return;
    try {
      const res = await providerLoginsAPI.cancel(login.id);
      setLogin(res.data);
    } finally {
      stopPolling();
    }
  };

  const active = login && ['pending', 'claimed', 'url_ready', 'awaiting_code'].includes(login.status);

  const statusLine = () => {
    if (!login) return null;
    switch (login.status) {
      case 'pending':
        return target === 'user'
          ? 'Waiting for your personal runner to pick this up…'
          : 'Waiting for the worker (agentd) to pick this up…';
      case 'claimed':
        return 'Starting the CLI sign-in on the worker host…';
      case 'url_ready':
      case 'awaiting_code':
        return login.detail;
      case 'completed':
        return 'Signed in successfully.';
      case 'cancelled':
        return 'Sign-in cancelled.';
      case 'failed':
        return login.detail || 'Sign-in failed.';
      default:
        return login.detail;
    }
  };

  return (
    <div
      style={{
        marginTop: 10,
        padding: '10px 12px',
        background: 'var(--surface-alt)',
        border: '1px solid var(--neutral-soft)',
        borderRadius: 6,
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
        <span style={{ fontSize: 13, fontWeight: 600, color: 'var(--text)' }}>{title}</span>
        {loggedIn && !active && (
          <span style={{ fontSize: 12, color: 'var(--success)', fontWeight: 600 }}>● connected</span>
        )}
        <div style={{ flex: 1 }} />
        {!active ? (
          <button className="button" style={{ padding: '6px 14px', fontSize: 13 }} onClick={start}>
            {loggedIn ? 'Re-connect' : 'Connect'}
          </button>
        ) : (
          <button
            onClick={cancel}
            style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', fontSize: 12, width: 'auto' }}
          >
            Cancel
          </button>
        )}
      </div>

      {(login || error) && (
        <div style={{ marginTop: 8 }}>
          {error && <div style={{ color: 'var(--danger)', fontSize: 12 }}>{error}</div>}
          {login && (
            <div
              style={{
                fontSize: 12,
                color: login.status === 'failed' ? 'var(--danger)' : login.status === 'completed' ? 'var(--success)' : 'var(--text-muted)',
              }}
            >
              {statusLine()}
            </div>
          )}
          {login && active && login.auth_url && (
            <a
              href={login.auth_url}
              target="_blank"
              rel="noreferrer"
              className="button"
              style={{ display: 'inline-block', marginTop: 8, padding: '6px 14px', fontSize: 13, textDecoration: 'none' }}
            >
              Open sign-in page ↗
            </a>
          )}
          {login && login.status === 'awaiting_code' && (
            <div style={{ display: 'flex', gap: 8, marginTop: 8, alignItems: 'center' }}>
              <input
                value={code}
                onChange={(e) => setCode(e.target.value)}
                placeholder="Paste the authorization code here"
                style={{ flex: 1, padding: '6px 8px', fontSize: 13 }}
                disabled={codeSent}
              />
              <button
                className="button"
                style={{ padding: '6px 14px', fontSize: 13, width: 'auto' }}
                onClick={submitCode}
                disabled={codeSent || !code.trim()}
              >
                {codeSent ? 'Code sent…' : 'Submit code'}
              </button>
            </div>
          )}
          {login && login.status === 'failed' && (
            <button
              className="button-secondary button"
              style={{ marginTop: 8, padding: '6px 14px', fontSize: 13, width: 'auto' }}
              onClick={start}
            >
              Retry
            </button>
          )}
        </div>
      )}
      {!login && !loggedIn && (
        <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 6 }}>
          {target === 'user'
            ? 'Signs into the vendor CLI on your own machine, using your own subscription. Your personal runner (Agent Connector) must be running.'
            : 'Signs into the vendor CLI on the machine running agentd, using your own subscription. The worker must be running.'}
        </div>
      )}
    </div>
  );
};
