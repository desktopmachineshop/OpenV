import React, { useCallback, useEffect, useState } from 'react';
import { WorkerKey, myRunnerKeyAPI } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { RunnerKeyModal } from './RunnerKeyModal';
import { RunnerConnectPrompt } from '../RunnerConnectPrompt';
import { ErrorBanner, useConfirm } from '../ui';

interface MyRunnerCardProps {
  orgId: string;
  onKeysChanged?: () => void;
}

// Every member can self-serve a personal runner key: run agents on their own
// machine with their own AI subscription. Runs they launch prefer their
// personal runner during the grace window.
export const MyRunnerCard: React.FC<MyRunnerCardProps> = ({ orgId, onKeysChanged }) => {
  const confirm = useConfirm();
  const [keyRecord, setKeyRecord] = useState<WorkerKey | null>(null);
  const [online, setOnline] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [plaintext, setPlaintext] = useState<string | null>(null);
  const [showConnect, setShowConnect] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await myRunnerKeyAPI.get(orgId);
      setKeyRecord(res.data.key_record);
      setOnline(res.data.online);
      setError('');
    } catch (err: any) {
      setError(`Failed to load your runner key: ${apiErrorMessage(err)}`);
    } finally {
      setLoading(false);
    }
  }, [orgId]);

  useEffect(() => {
    load();
  }, [load]);

  const createKey = async (rotating: boolean) => {
    if (rotating) {
      const ok = await confirm({
        title: 'Rotate runner key',
        message:
          'Rotate your runner key? The old key is invalidated immediately — any runner using it will stop authenticating.',
        confirmLabel: 'Rotate key',
        danger: true,
      });
      if (!ok) return;
    }
    setBusy(true);
    try {
      const res = await myRunnerKeyAPI.create(orgId);
      setPlaintext(res.data.key);
      await load();
      onKeysChanged?.();
      setError('');
    } catch (err: any) {
      setError(`Failed to create your runner key: ${apiErrorMessage(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const handleRevoke = async () => {
    const ok = await confirm({
      title: 'Revoke runner key',
      message:
        'Revoke your personal runner key? Your runner will stop authenticating and runs you launch will use workspace or hosted runners.',
      confirmLabel: 'Revoke',
      danger: true,
    });
    if (!ok) {
      return;
    }
    setBusy(true);
    try {
      await myRunnerKeyAPI.revoke(orgId);
      await load();
      onKeysChanged?.();
      setError('');
    } catch (err: any) {
      setError(`Failed to revoke your runner key: ${apiErrorMessage(err)}`);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="card">
      <h3 style={{ marginBottom: 6 }}>My personal runner</h3>

      <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 12 }} />

      {loading ? (
        <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading your runner key…</div>
      ) : !keyRecord ? (
        <>
          <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
            Run agents on your own machine with your own AI subscription. Runs you launch prefer
            your personal runner for the first minute. The easiest setup is the Agent Connector —
            it pairs, stores your key, and starts the runner on demand.
          </p>
          <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap' }}>
            <button
              className="button"
              style={{ width: 'auto' }}
              onClick={() => setShowConnect(true)}
            >
              Set up Agent Connector
            </button>
            <button
              className="button-secondary button"
              style={{ width: 'auto' }}
              onClick={() => createKey(false)}
              disabled={busy}
              title="Advanced: mint a key and run agentd manually"
            >
              {busy ? 'Creating…' : 'Create key manually'}
            </button>
          </div>
        </>
      ) : (
        <div style={{ display: 'flex', alignItems: 'center', gap: 14, flexWrap: 'wrap' }}>
          <span style={{ fontSize: 13, color: 'var(--text)', fontWeight: 600 }}>
            <span
              style={{ color: online ? 'var(--success)' : 'var(--neutral)', marginRight: 4, fontSize: 11 }}
            >
              ●
            </span>
            {online ? 'online' : 'offline'}
          </span>
          <span style={{ fontSize: 13, color: 'var(--text-muted)' }}>
            created {keyRecord.created_at ? new Date(keyRecord.created_at).toLocaleDateString() : '—'}
          </span>
          <div style={{ flex: 1 }} />
          {!online && (
            <button
              className="button"
              style={{ width: 'auto', padding: '6px 14px' }}
              onClick={() => setShowConnect(true)}
            >
              Open connector
            </button>
          )}
          <button
            className="button-secondary button"
            style={{ width: 'auto', padding: '6px 14px' }}
            onClick={() => createKey(true)}
            disabled={busy}
          >
            Rotate
          </button>
          <button
            onClick={handleRevoke}
            disabled={busy}
            style={{
              background: 'none',
              border: '1px solid var(--danger)',
              color: 'var(--danger)',
              cursor: 'pointer',
              fontSize: 13,
              width: 'auto',
              padding: '6px 14px',
              borderRadius: 4,
            }}
          >
            Revoke
          </button>
        </div>
      )}

      {plaintext && (
        <RunnerKeyModal
          title="Your personal runner key"
          plaintext={plaintext}
          onClose={() => setPlaintext(null)}
        />
      )}

      {showConnect && (
        <RunnerConnectPrompt
          orgId={orgId}
          onClose={() => {
            setShowConnect(false);
            load();
            onKeysChanged?.();
          }}
          reason="Set up or open your personal runner."
        />
      )}
    </div>
  );
};
