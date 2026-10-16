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

interface Props {
  // Whether a runner of the member's reports Claude Code signed in: a sign-in
  // for the session, which a cloud runner loses when its lease ends.
  loggedIn: boolean;
  onChanged: () => void;
}

// ClaudeSignInCard is the one place a member signs Claude Code in, two ways:
// for this session (the runner's own sign-in, gone when a cloud runner's
// lease ends) or with a saved token that keeps every runner of theirs signed
// in until it expires. The saved token is minted by their cloud runner
// (`claude setup-token` relayed to the browser) or pasted from their own
// computer. The line at the top says which of the two is in force, and until
// when. The token itself is never shown again: only its last four characters.
export const ClaudeSignInCard: React.FC<Props> = ({ loggedIn, onChanged }) => {
  const [status, setStatus] = useState<ProviderTokenStatus | null>(null);
  const [draft, setDraft] = useState('');
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

  const tokens = status?.available ?? false;
  const saved = Boolean(status?.connected && !status.expired);
  const left = daysLeft(status?.expires_at);

  return (
    <div data-testid="claude-sign-in" style={{ marginBottom: 12 }}>
      <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text)' }}>Claude Code</div>
      <p data-testid="claude-sign-in-state" style={{ fontSize: 13, margin: '4px 0 8px' }}>
        {saved ? (
          <span style={{ color: 'var(--success)' }}>
            ● Signed in with a saved token (…{status?.last4}), expires {formatDate(status?.expires_at)}
            {left <= WARN_DAYS && (
              <span style={{ color: 'var(--warning)' }}> — {left} days left; sign in again soon</span>
            )}
          </span>
        ) : loggedIn ? (
          <span style={{ color: 'var(--success)' }}>
            ● Signed in for this session only: a cloud runner&apos;s sign-in ends with its lease
          </span>
        ) : (
          <span style={{ color: 'var(--text-muted)' }}>Not signed in</span>
        )}
        {status?.connected && status.expired && (
          <span style={{ color: 'var(--danger)' }}>
            {' '}
            · your saved token expired on {formatDate(status.expires_at)}
          </span>
        )}
      </p>

      <ProviderConnectCard
        provider={PROVIDER}
        target="user"
        loggedIn={false}
        title="This session only"
        startLabel="Sign in"
        onComplete={onChanged}
      />
      {tokens && runnerMints && (
        <ProviderConnectCard
          provider={PROVIDER}
          target="user"
          purpose="token"
          loggedIn={false}
          title="Stay signed in for a year (saved token)"
          startLabel={saved ? 'Renew' : 'Sign in'}
          onComplete={load}
        />
      )}
      {tokens && (
        <details style={{ fontSize: 13, marginTop: 6 }}>
          <summary style={{ cursor: 'pointer', color: 'var(--text-muted)' }}>
            Already have a token from <code>claude setup-token</code>? Paste it
          </summary>
          <form onSubmit={save} style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginTop: 6 }}>
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
          </form>
        </details>
      )}
      {status?.connected && (
        <button className="button-secondary" type="button" onClick={remove} disabled={busy} style={{ marginTop: 6 }}>
          Remove saved token
        </button>
      )}
      {status && !tokens && (
        <p style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 0 }}>
          This server keeps no saved tokens (no <code>OPENV_SECRETS_KEY</code>), so Claude Code signs in
          for the session only.
        </p>
      )}
      {error && (
        <p role="alert" style={{ fontSize: 13, color: 'var(--danger)', marginBottom: 0 }}>
          {error}
        </p>
      )}
    </div>
  );
};
