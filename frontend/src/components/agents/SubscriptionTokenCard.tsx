import React, { useCallback, useEffect, useState } from 'react';
import { providerTokensAPI, ProviderTokenStatus } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import type { FeatureKey } from '../../generated/contract';
import { useFeature } from '../../hooks/useFeature';
import { ProviderConnectCard } from './ProviderConnectCard';

export const SUBSCRIPTION_TOKEN_FEATURE: FeatureKey = 'claude-subscription-token';
// The member's cloud runner minting the token, instead of a paste.
export const CLOUD_RUNNER_TOKEN_FEATURE: FeatureKey = 'cloud-runner-token';

const PROVIDER = 'claude-code';
const WARN_DAYS = 30;

const formatDate = (iso?: string) => (iso ? new Date(iso).toLocaleDateString() : '');

const daysLeft = (iso?: string) =>
  iso ? Math.floor((new Date(iso).getTime() - Date.now()) / 86_400_000) : 0;

// SubscriptionTokenCard keeps Claude Code signed in for the member's own runs:
// they run `claude setup-token` on their own computer and paste the token it
// prints. OpenV seals it and hands it only to a runner that serves this member
// alone (their own machine or a cloud runner leased to them), so those runners
// start chats already signed in, with no browser step per lease. The token is
// never shown again; the card shows its last four characters and when it
// lapses.
export const SubscriptionTokenCard: React.FC = () => {
  const [status, setStatus] = useState<ProviderTokenStatus | null>(null);
  const [draft, setDraft] = useState('');
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const runnerMints = useFeature(CLOUD_RUNNER_TOKEN_FEATURE);

  const load = useCallback(async () => {
    try {
      const res = await providerTokensAPI.get(PROVIDER);
      setStatus(res.data);
    } catch (err) {
      setError(apiErrorMessage(err, 'Could not load your saved token.'));
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      const res = await providerTokensAPI.save(PROVIDER, draft.trim());
      setStatus(res.data);
      setDraft('');
      setEditing(false);
    } catch (err) {
      setError(apiErrorMessage(err, 'Could not save the token.'));
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    setBusy(true);
    setError('');
    try {
      await providerTokensAPI.remove(PROVIDER);
      await load();
    } catch (err) {
      setError(apiErrorMessage(err, 'Could not remove the token.'));
    } finally {
      setBusy(false);
    }
  };

  if (status && !status.available) {
    return (
      <div className="card" data-testid="subscription-token-card">
        <h3 style={{ marginBottom: 6 }}>Claude subscription token</h3>
        <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 0 }}>
          This server is not set up to keep tokens (no <code>OPENV_SECRETS_KEY</code>). Sign Claude
          Code in under Agent sign-ins instead.
        </p>
      </div>
    );
  }

  const left = daysLeft(status?.expires_at);
  const showForm = editing || (status !== null && !status.connected);

  return (
    <div className="card" data-testid="subscription-token-card">
      <h3 style={{ marginBottom: 6 }}>Claude subscription token</h3>
      <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
        Stay signed in to Claude Code without signing in again on every cloud runner. On your own
        computer, run <code>claude setup-token</code>, sign in with your Claude subscription, and
        paste the token it prints here. It lasts a year. OpenV keeps it encrypted and gives it only
        to runners that work for you alone: your own machine and cloud runners leased to you, never
        a shared workspace runner.
      </p>

      {status?.connected && !editing && (
        <div style={{ fontSize: 13, marginBottom: 8 }}>
          {status.expired ? (
            <span style={{ color: 'var(--danger)' }}>
              Your token ending …{status.last4} expired on {formatDate(status.expires_at)}. Run{' '}
              <code>claude setup-token</code> again and save the new one.
            </span>
          ) : (
            <span>
              Saved token ending <b>…{status.last4}</b>, valid until{' '}
              {formatDate(status.expires_at)}
              {left <= WARN_DAYS && (
                <span style={{ color: 'var(--warning)' }}> — {left} days left; replace it soon</span>
              )}
              .
            </span>
          )}
          <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
            <button className="button" type="button" onClick={() => setEditing(true)} disabled={busy}>
              Replace token
            </button>
            <button className="button-secondary" type="button" onClick={remove} disabled={busy}>
              Remove
            </button>
          </div>
        </div>
      )}

      {showForm && (
        <form onSubmit={save} style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          <input
            type="password"
            aria-label="Claude subscription token"
            placeholder="sk-ant-oat01-…"
            autoComplete="off"
            spellCheck={false}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            style={{ flex: '1 1 240px', minWidth: 0 }}
          />
          <button className="button" type="submit" disabled={busy || !draft.trim()}>
            {busy ? 'Saving…' : 'Save token'}
          </button>
          {editing && (
            <button
              className="button-secondary"
              type="button"
              onClick={() => {
                setEditing(false);
                setDraft('');
              }}
            >
              Cancel
            </button>
          )}
        </form>
      )}

      {runnerMints && (showForm || status?.expired) && (
        <div data-testid="mint-token" style={{ marginTop: 12 }}>
          <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 4 }}>
            No terminal to hand? Your cloud runner can get the token for you: start one under Cloud
            runner, then sign in with your Claude subscription here. The token goes straight to
            OpenV and is saved for you.
          </p>
          <ProviderConnectCard
            provider="claude-code"
            target="user"
            purpose="token"
            loggedIn={false}
            title="Get a token from your cloud runner"
            startLabel="Get token"
            onComplete={() => {
              setEditing(false);
              load();
            }}
          />
        </div>
      )}

      {error && (
        <p role="alert" style={{ fontSize: 13, color: 'var(--danger)', marginBottom: 0 }}>
          {error}
        </p>
      )}
    </div>
  );
};
