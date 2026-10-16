import React, { useCallback, useEffect, useState } from 'react';
import { ConnectedApp, connectedAppsAPI } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { ErrorBanner, useConfirm } from './ui';

// The AI apps the member has connected to their workspaces over the remote
// MCP endpoint (REQ-44), each with the workspace it acts in, what it may do
// and when it last did anything, and a Disconnect that ends its access at
// once. Connecting starts in the app itself, with the address shown here.

const when = (iso?: string): string => (iso ? new Date(iso).toLocaleString() : 'never');

export const ConnectedAppsCard: React.FC<{ apiBase: string }> = ({ apiBase }) => {
  const confirm = useConfirm();
  const [apps, setApps] = useState<ConnectedApp[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    try {
      const res = await connectedAppsAPI.list();
      setApps(res.data);
      setError('');
    } catch (err) {
      setError(`Failed to load your connected apps: ${apiErrorMessage(err)}`);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const disconnect = async (app: ConnectedApp) => {
    const ok = await confirm({
      title: 'Disconnect app',
      message: `Disconnect ${app.client_name} from ${app.org_name}? It stops working at once; connecting it again asks you to approve it again.`,
      confirmLabel: 'Disconnect',
      danger: true,
    });
    if (!ok) return;
    try {
      await connectedAppsAPI.revoke(app.id);
      await load();
    } catch (err) {
      setError(`Failed to disconnect ${app.client_name}: ${apiErrorMessage(err)}`);
    }
  };

  const endpoint = `${apiBase.replace(/\/$/, '')}/mcp`;
  return (
    <div className="card">
      <h3 style={{ marginBottom: 6 }}>Connected apps</h3>
      <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
        Connect an AI app such as Claude or Cursor to a workspace without a runner or a key. In Claude, open Settings →
        Connectors, choose Add custom connector and paste <code>{endpoint}</code>; in another app, add that address
        as a remote MCP server. Approve it when it asks, and it acts as you, with the tools an OpenV agent has.
      </p>
      {error && <ErrorBanner message={error} />}
      {loading ? (
        <p style={{ fontSize: 13 }}>Loading…</p>
      ) : apps.length === 0 ? (
        <p style={{ fontSize: 13, marginBottom: 0 }}>No apps connected.</p>
      ) : (
        <ul style={{ listStyle: 'none', padding: 0, margin: 0 }}>
          {apps.map((app) => (
            <li
              key={app.id}
              style={{
                display: 'flex',
                gap: 12,
                alignItems: 'center',
                justifyContent: 'space-between',
                flexWrap: 'wrap',
                padding: '8px 0',
                borderTop: '1px solid var(--border)',
              }}
            >
              <div style={{ minWidth: 0 }}>
                <div style={{ fontWeight: 600 }}>{app.client_name}</div>
                <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                  {app.org_name} · {app.scope.includes('openv:write') ? 'read and change' : 'read only'} · last used{' '}
                  {when(app.last_used_at)}
                </div>
              </div>
              <button type="button" className="button-secondary" onClick={() => disconnect(app)}>
                Disconnect
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
};
