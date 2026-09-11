import React, { useCallback, useEffect, useState } from 'react';
import { HostedRunnerStatus, hostedRunnerAPI } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { useViewport } from '../../hooks/useViewport';

interface HostedRunnerCardProps {
  orgId: string;
  isAdmin: boolean;
}

const chipStyle = (bg: string): React.CSSProperties => ({
  display: 'inline-block',
  padding: '2px 10px',
  borderRadius: 10,
  fontSize: 12,
  fontWeight: 600,
  color: '#fff',
  background: bg,
});

// The provider API keys the enable form collects, one row each.
const KEY_FIELDS: { id: string; label: string; placeholder: string }[] = [
  { id: 'anthropic', label: 'Anthropic API key', placeholder: 'sk-ant-…' },
  { id: 'openai', label: 'OpenAI API key', placeholder: 'sk-…' },
  { id: 'gemini', label: 'Gemini API key', placeholder: 'AIza…' },
];

const statusColor = (status: string): string => {
  if (status === 'running') return 'var(--success)';
  if (status === 'error') return 'var(--danger)';
  return 'var(--text-muted)';
};

// Admin-managed hosted runner container. Non-admins get read-only status.
export const HostedRunnerCard: React.FC<HostedRunnerCardProps> = ({ orgId, isAdmin }) => {
  // On a phone the status facts stack under their labels and every action is
  // a full-width, 44 px row (REQ-108).
  const { isPhone } = useViewport();
  const [status, setStatus] = useState<HostedRunnerStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  // Enable form: one value per provider key, in the order KEY_FIELDS lists.
  const [keyValues, setKeyValues] = useState<Record<string, string>>({
    anthropic: '',
    openai: '',
    gemini: '',
  });
  const setKey = (id: string, value: string) => setKeyValues((prev) => ({ ...prev, [id]: value }));
  const [formError, setFormError] = useState('');

  // Remove confirmation
  const [confirmingRemove, setConfirmingRemove] = useState(false);
  const [purgeVolume, setPurgeVolume] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await hostedRunnerAPI.get(orgId);
      setStatus(res.data);
      setError('');
    } catch (err: any) {
      setError(`Failed to load hosted runner status: ${apiErrorMessage(err)}`);
    } finally {
      setLoading(false);
    }
  }, [orgId]);

  useEffect(() => {
    load();
  }, [load]);

  const handleEnable = async (e: React.FormEvent) => {
    e.preventDefault();
    const providerKeys: Record<string, string> = {};
    for (const f of KEY_FIELDS) {
      const value = (keyValues[f.id] || '').trim();
      if (value) providerKeys[f.id] = value;
    }
    if (Object.keys(providerKeys).length === 0) {
      setFormError('Enter at least one provider API key.');
      return;
    }
    setFormError('');
    setBusy(true);
    try {
      await hostedRunnerAPI.enable(orgId, providerKeys);
      setKeyValues({ anthropic: '', openai: '', gemini: '' });
      await load();
    } catch (err: any) {
      setError(`Failed to enable the hosted runner: ${apiErrorMessage(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const handleStop = async () => {
    setBusy(true);
    try {
      await hostedRunnerAPI.stop(orgId);
      await load();
      setError('');
    } catch (err: any) {
      setError(`Failed to stop the hosted runner: ${apiErrorMessage(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const handleStart = async () => {
    setBusy(true);
    try {
      await hostedRunnerAPI.start(orgId);
      await load();
      setError('');
    } catch (err: any) {
      setError(`Failed to start the hosted runner: ${apiErrorMessage(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const handleRemove = async () => {
    setBusy(true);
    try {
      await hostedRunnerAPI.remove(orgId, purgeVolume);
      setConfirmingRemove(false);
      setPurgeVolume(false);
      await load();
      setError('');
    } catch (err: any) {
      setError(`Failed to remove the hosted runner: ${apiErrorMessage(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const record = status?.record || null;

  return (
    <div className="card">
      <h3 style={{ marginBottom: 6 }}>Hosted runner</h3>
      <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
        A managed runner container operated by this deployment. It executes runs when no personal
        or workspace runner claims them.
      </p>

      {error && (
        <div
          style={{
            background: 'var(--tint-red)',
            border: '1px solid var(--danger)',
            color: 'var(--danger-strong)',
            padding: '10px 14px',
            borderRadius: 4,
            marginBottom: 12,
            fontSize: 13,
          }}
        >
          {error}
        </div>
      )}

      {loading ? (
        <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading hosted runner status…</div>
      ) : !status ? null : !status.enabled ? (
        <div
          style={{
            background: 'var(--tint-blue)',
            border: '1px solid var(--accent)',
            color: 'var(--accent-text)',
            padding: '10px 14px',
            borderRadius: 4,
            fontSize: 13,
          }}
        >
          Hosted runners aren't enabled on this deployment — runs execute on personal and
          workspace runners.
        </div>
      ) : !record ? (
        isAdmin ? (
          <form onSubmit={handleEnable}>
            <div style={{ fontSize: 13, color: 'var(--text)', marginBottom: 10 }}>
              Enable a hosted runner for this workspace. Provide at least one provider API key for
              the runner to use.
            </div>
            <div
              // One field per row on a phone; three across on a wider screen.
              style={
                isPhone
                  ? { display: 'flex', flexDirection: 'column', gap: 12, marginBottom: 8 }
                  : { display: 'flex', gap: 10, flexWrap: 'wrap', marginBottom: 8 }
              }
            >
              {KEY_FIELDS.map((f) => (
                <div key={f.id} style={isPhone ? { minWidth: 0 } : { flex: 1, minWidth: 180 }}>
                  <label htmlFor={f.id} style={{ fontSize: isPhone ? 13 : 12 }}>
                    {f.label}
                  </label>
                  <input
                    id={f.id}
                    type="password"
                    value={keyValues[f.id]}
                    onChange={(e) => setKey(f.id, e.target.value)}
                    placeholder={f.placeholder}
                    // A key is not a password to remember, an email or a
                    // sentence: no manager fill, no autocapitalise, no
                    // autocorrect, and a plain keyboard.
                    autoComplete="off"
                    inputMode="text"
                    autoCapitalize="none"
                    autoCorrect="off"
                    spellCheck={false}
                    style={isPhone ? { width: '100%', minHeight: 44 } : undefined}
                  />
                </div>
              ))}
            </div>
            {formError && (
              <div style={{ color: 'var(--danger)', fontSize: isPhone ? 13 : 12, marginBottom: 8 }}>
                {formError}
              </div>
            )}
            <div style={{ fontSize: isPhone ? 13 : 12, color: 'var(--text-muted)', marginBottom: 12 }}>
              Keys are sent to the runner container once and never stored by OpenV.
            </div>
            <div className={isPhone ? 'action-sheet' : undefined}>
              <button
                type="submit"
                className="button"
                style={isPhone ? { minHeight: 44 } : { width: 'auto' }}
                disabled={busy}
              >
                {busy ? 'Enabling…' : 'Enable hosted runner'}
              </button>
            </div>
          </form>
        ) : (
          <div style={{ color: 'var(--neutral)', fontSize: 13 }}>
            No hosted runner is set up for this workspace. A workspace admin can enable one.
          </div>
        )
      ) : (
        <>
          <div
            // A phone stacks the facts under their labels and puts the
            // actions in a full-width column; a wider screen keeps the row.
            style={
              isPhone
                ? { display: 'flex', flexDirection: 'column', gap: 10 }
                : { display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }
            }
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
              <span style={chipStyle(statusColor(record.status))}>{record.status || 'unknown'}</span>
              <span style={{ fontSize: 13, color: 'var(--text)' }}>
                <span
                  style={{
                    color: status.online ? 'var(--success)' : 'var(--neutral)',
                    marginRight: 4,
                    fontSize: 12,
                  }}
                >
                  ●
                </span>
                {status.online ? 'online' : 'offline'}
              </span>
            </div>
            <div style={{ fontSize: isPhone ? 13 : 12, color: 'var(--text-muted)', overflowWrap: 'anywhere' }}>
              container:{' '}
              <code style={{ fontSize: isPhone ? 13 : 12 }}>{record.container_name}</code>
              {status.container_state ? ` (${status.container_state})` : ''}
            </div>
            {!isPhone && <div style={{ flex: 1 }} />}
            {isAdmin && (
              <div
                className={isPhone ? 'action-sheet' : undefined}
                style={isPhone ? undefined : { display: 'flex', gap: 8 }}
              >
                {status.container_state === 'running' ? (
                  <button
                    className="button-secondary button"
                    style={isPhone ? { minHeight: 44 } : { width: 'auto', padding: '6px 14px' }}
                    onClick={handleStop}
                    disabled={busy}
                  >
                    Stop
                  </button>
                ) : (
                  <button
                    className="button"
                    style={isPhone ? { minHeight: 44 } : { width: 'auto', padding: '6px 14px' }}
                    onClick={handleStart}
                    disabled={busy}
                  >
                    Start
                  </button>
                )}
                <button
                  onClick={() => setConfirmingRemove(true)}
                  disabled={busy}
                  style={{
                    background: 'none',
                    border: '1px solid var(--danger)',
                    color: 'var(--danger)',
                    cursor: 'pointer',
                    fontSize: isPhone ? 14 : 13,
                    width: isPhone ? '100%' : 'auto',
                    minHeight: isPhone ? 44 : undefined,
                    padding: '6px 14px',
                    borderRadius: 4,
                  }}
                >
                  Remove
                </button>
              </div>
            )}
          </div>
          {record.detail && (
            <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 8 }}>{record.detail}</div>
          )}
          {isAdmin && confirmingRemove && (
            <div
              style={{
                marginTop: 12,
                border: '1px solid var(--danger)',
                background: 'var(--tint-red)',
                borderRadius: 4,
                padding: '10px 14px',
                fontSize: 13,
                color: 'var(--danger-strong)',
              }}
            >
              <div style={{ marginBottom: 8 }}>
                Remove the hosted runner? Queued runs will fall back to personal and workspace
                runners.
              </div>
              <label
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 6,
                  fontSize: 13,
                  color: 'var(--danger-strong)',
                  marginBottom: 10,
                }}
              >
                <input
                  type="checkbox"
                  checked={purgeVolume}
                  onChange={(e) => setPurgeVolume(e.target.checked)}
                  style={{ width: 'auto' }}
                />
                also delete its credential volume
              </label>
              <div
                className={isPhone ? 'action-sheet' : undefined}
                style={isPhone ? undefined : { display: 'flex', gap: 8 }}
              >
                <button
                  onClick={handleRemove}
                  disabled={busy}
                  style={{
                    background: 'var(--danger)',
                    border: 'none',
                    color: '#fff',
                    cursor: 'pointer',
                    fontSize: isPhone ? 14 : 13,
                    fontWeight: 600,
                    width: isPhone ? '100%' : 'auto',
                    minHeight: isPhone ? 44 : undefined,
                    padding: '6px 14px',
                    borderRadius: 4,
                  }}
                >
                  {busy ? 'Removing…' : 'Remove runner'}
                </button>
                <button
                  className="button-secondary button"
                  style={isPhone ? { minHeight: 44 } : { width: 'auto', padding: '6px 14px' }}
                  onClick={() => {
                    setConfirmingRemove(false);
                    setPurgeVolume(false);
                  }}
                  disabled={busy}
                >
                  Cancel
                </button>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
};
