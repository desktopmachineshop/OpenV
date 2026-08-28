import React, { useCallback, useEffect, useState } from 'react';
import { Org, WorkerKey, workerKeysAPI } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { HostedRunnerCard } from './HostedRunnerCard';
import { MyRunnerCard } from './MyRunnerCard';
import { RunnerKeyModal } from './RunnerKeyModal';

const th: React.CSSProperties = {
  textAlign: 'left',
  fontSize: 12,
  color: 'var(--text-muted)',
  padding: '8px 10px',
  borderBottom: '1px solid var(--border-soft)',
};

const td: React.CSSProperties = {
  padding: '8px 10px',
  fontSize: 13,
  color: 'var(--text)',
  borderBottom: '1px solid var(--surface-inset)',
};

const chip = (bg: string): React.CSSProperties => ({
  display: 'inline-block',
  padding: '2px 10px',
  borderRadius: 10,
  fontSize: 11,
  fontWeight: 600,
  color: '#fff',
  background: bg,
});

interface WorkerKeysTabProps {
  org: Org;
  isAdmin: boolean;
}

// Runners tab: hosted runner (admin-managed), the member's personal runner,
// and the workspace worker-key table. Worker keys let a local agent daemon
// (agentd) authenticate against this workspace; the plaintext key is only
// ever shown once, right after creation.
export const WorkerKeysTab: React.FC<WorkerKeysTabProps> = ({ org, isAdmin }) => {
  const [keys, setKeys] = useState<WorkerKey[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [creating, setCreating] = useState(false);
  const [newKey, setNewKey] = useState<{ record: WorkerKey; plaintext: string } | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await workerKeysAPI.list(org.id);
      setKeys(res.data || []);
    } catch (err: any) {
      setError(`Failed to load worker keys: ${apiErrorMessage(err)}`);
    } finally {
      setLoading(false);
    }
  }, [org.id]);

  useEffect(() => {
    load();
  }, [load]);

  const handleCreate = async () => {
    const name = window.prompt('Name for the new worker key (e.g. "shop-floor-pc"):');
    if (!name || !name.trim()) return;
    setCreating(true);
    setError('');
    try {
      const res = await workerKeysAPI.create(org.id, name.trim());
      setNewKey({ record: res.data.key_record, plaintext: res.data.key });
      await load();
    } catch (err: any) {
      setError(`Failed to create worker key: ${apiErrorMessage(err)}`);
    } finally {
      setCreating(false);
    }
  };

  const handleRevoke = async (key: WorkerKey) => {
    if (!window.confirm(`Revoke the worker key "${key.name}"? Workers using it will stop authenticating.`)) return;
    try {
      await workerKeysAPI.revoke(org.id, key.id);
      await load();
      setError('');
    } catch (err: any) {
      setError(`Failed to revoke worker key: ${apiErrorMessage(err)}`);
    }
  };

  const fmtDate = (value: string | null | undefined) =>
    value ? new Date(value).toLocaleString() : '—';

  return (
    <>
      {error && (
        <div
          style={{
            background: 'var(--tint-red)',
            border: '1px solid var(--danger)',
            color: 'var(--danger-strong)',
            padding: '10px 14px',
            borderRadius: 4,
            marginBottom: 16,
            fontSize: 13,
          }}
        >
          {error}{' '}
          <button
            onClick={() => setError('')}
            style={{ background: 'none', border: 'none', color: 'var(--danger-strong)', cursor: 'pointer', width: 'auto', padding: 0 }}
          >
            ✕
          </button>
        </div>
      )}

      <HostedRunnerCard orgId={org.id} isAdmin={isAdmin} />

      <MyRunnerCard orgId={org.id} onKeysChanged={load} />

      <div className="card">
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 10 }}>
          <h3 style={{ marginBottom: 0 }}>Workspace keys</h3>
          {isAdmin && (
            <button
              className="button"
              style={{ padding: '6px 14px', width: 'auto' }}
              onClick={handleCreate}
              disabled={creating}
            >
              {creating ? 'Creating…' : '+ Create key'}
            </button>
          )}
        </div>
        <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
          Worker keys authenticate agent runner machines (agentd) against this workspace. Each key
          is scoped to this workspace only.
        </p>

        {loading ? (
          <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading worker keys…</div>
        ) : keys.length === 0 ? (
          <div style={{ color: 'var(--neutral)', fontSize: 13 }}>No worker keys yet.</div>
        ) : (
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr>
                <th style={th}>Name</th>
                <th style={{ ...th, width: 130 }}>Type</th>
                <th style={th}>Created</th>
                <th style={th}>Last used</th>
                <th style={{ ...th, width: 90 }}>Status</th>
                <th style={{ ...th, width: 80 }}></th>
              </tr>
            </thead>
            <tbody>
              {keys.map((k) => (
                <tr key={k.id}>
                  <td style={{ ...td, fontWeight: 600 }}>
                    {k.name}
                    {k.name === 'hosted-runner' && (
                      <span style={{ ...chip('var(--accent)'), marginLeft: 6 }}>hosted</span>
                    )}
                  </td>
                  <td style={td}>
                    {k.user_id ? (
                      <span style={chip('var(--purple)')} title={k.user_name || undefined}>
                        Personal{k.user_name ? ` · ${k.user_name}` : ''}
                      </span>
                    ) : (
                      <span style={chip('var(--text-muted)')}>Workspace</span>
                    )}
                  </td>
                  <td style={td}>{fmtDate(k.created_at)}</td>
                  <td style={td}>{fmtDate(k.last_used_at)}</td>
                  <td style={td}>
                    {k.revoked ? (
                      <span style={chip('var(--danger)')}>revoked</span>
                    ) : (
                      <span style={chip('var(--success)')}>active</span>
                    )}
                  </td>
                  <td style={{ ...td, textAlign: 'right' }}>
                    {isAdmin && !k.revoked && (
                      <button
                        onClick={() => handleRevoke(k)}
                        style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', fontSize: 12, width: 'auto', padding: 2 }}
                      >
                        Revoke
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {newKey && (
        <RunnerKeyModal
          title={`Worker key created: ${newKey.record.name}`}
          plaintext={newKey.plaintext}
          onClose={() => setNewKey(null)}
        />
      )}
    </>
  );
};
