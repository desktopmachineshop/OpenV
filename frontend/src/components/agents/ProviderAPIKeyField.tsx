import React, { useState } from 'react';
import { StoredProviderAPIKey, providerSettingsAPI } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import type { FeatureKey } from '../../generated/contract';

/** Gates pasting a provider API key into workspace settings. */
export const WORKSPACE_API_KEYS_FEATURE: FeatureKey = 'workspace-api-keys';

// The variable each provider's CLI reads its key from: the Go side's
// providers.DefaultAPIKeyEnv. A provider setting's api_key_env overrides it.
const DEFAULT_KEY_ENV: Record<string, string> = {
  'claude-code': 'ANTHROPIC_API_KEY',
  'anthropic-api': 'ANTHROPIC_API_KEY',
  'codex-cli': 'OPENAI_API_KEY',
  'openai-api': 'OPENAI_API_KEY',
  'gemini-cli': 'GEMINI_API_KEY',
  'antigravity-cli': 'GEMINI_API_KEY',
  'google-api': 'GEMINI_API_KEY',
};

export const providerKeyEnv = (provider: string, apiKeyEnv?: string): string =>
  (apiKeyEnv || '').trim() || DEFAULT_KEY_ENV[provider] || '';

interface ProviderAPIKeyFieldProps {
  keyEnv: string;
  stored?: StoredProviderAPIKey;
  // False when the server has no OPENV_SECRETS_KEY and cannot store keys.
  enabled: boolean;
  onSaved: (key: StoredProviderAPIKey) => void;
  onRemoved: (keyEnv: string) => void;
}

// A workspace admin pastes the provider's API key once; projects on API-key
// auth then use it on every runner, with nothing to set on the runner's
// machine. The key is write-only: only its last four characters come back.
export const ProviderAPIKeyField: React.FC<ProviderAPIKeyFieldProps> = ({
  keyEnv,
  stored,
  enabled,
  onSaved,
  onRemoved,
}) => {
  const [value, setValue] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  if (!keyEnv) return null;

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!value.trim()) return;
    setBusy(true);
    setError('');
    try {
      const res = await providerSettingsAPI.setKey(keyEnv, value.trim());
      setValue('');
      onSaved(res.data);
    } catch (err: any) {
      setError(`Failed to save the API key: ${apiErrorMessage(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    setBusy(true);
    setError('');
    try {
      await providerSettingsAPI.removeKey(keyEnv);
      onRemoved(keyEnv);
    } catch (err: any) {
      setError(`Failed to remove the API key: ${apiErrorMessage(err)}`);
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={save} style={{ marginTop: 12 }} data-testid={`provider-api-key-${keyEnv}`}>
      <label style={{ fontSize: 12 }} htmlFor={`provider-api-key-${keyEnv}`}>
        API key ({keyEnv})
      </label>
      {!enabled ? (
        <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
          This server cannot store API keys yet: its administrator has to set OPENV_SECRETS_KEY. Until
          then, set {keyEnv} on each runner's machine.
        </div>
      ) : (
        <>
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
            <input
              id={`provider-api-key-${keyEnv}`}
              type="password"
              autoComplete="off"
              spellCheck={false}
              value={value}
              onChange={(e) => setValue(e.target.value)}
              placeholder={stored ? `Saved key ending ${stored.last4}. Paste a new one to replace it` : 'Paste the key'}
              style={{ flex: 1, minWidth: 200, padding: '6px 8px', fontSize: 13 }}
            />
            <button className="button" type="submit" disabled={busy || !value.trim()} style={{ padding: '7px 16px' }}>
              {busy ? 'Saving…' : stored ? 'Replace key' : 'Save key'}
            </button>
            {stored && (
              <button className="button-secondary button" type="button" disabled={busy} onClick={remove}>
                Remove key
              </button>
            )}
          </div>
          <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 6 }}>
            {stored
              ? `A key ending ${stored.last4} is saved, encrypted. `
              : 'No key saved. '}
            Projects set to API-key auth (Project settings → Agents) use it on every runner, including
            members' own machines, so nothing has to be set there. It is never shown again.
          </div>
        </>
      )}
      {error && (
        <div role="alert" style={{ fontSize: 12, color: 'var(--danger)', marginTop: 6 }}>
          {error}
        </div>
      )}
    </form>
  );
};
