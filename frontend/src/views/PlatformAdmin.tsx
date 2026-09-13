import React, { useCallback, useEffect, useState } from 'react';
import { Navigate } from 'react-router-dom';
import { AdminUser, AdminWorkspace, PLANS, adminAPI } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { Navbar } from '../components/Navbar';
import { ErrorBanner, useConfirm } from '../components/ui';
import { useAppStore } from '../state/store';
import { useViewport } from '../hooks/useViewport';

// PlatformAdmin is the page for the deployment's operators (REQ-155): every
// workspace with its plan, which is where the open-source tier is granted
// (REQ-154), and every account with its platform-admin standing. Only a
// platform admin reaches it; anybody else is sent to their projects.

const th: React.CSSProperties = {
  textAlign: 'left',
  fontSize: 12,
  color: 'var(--text-muted)',
  fontWeight: 600,
  padding: '8px 10px',
  borderBottom: '1px solid var(--border)',
};

const td: React.CSSProperties = {
  padding: '8px 10px',
  fontSize: 13,
  color: 'var(--text)',
  borderBottom: '1px solid var(--border-soft)',
  verticalAlign: 'middle',
};

const planLabel = (plan: string): string => PLANS.find((p) => p.value === plan)?.label || plan;

export const PlatformAdmin: React.FC = () => {
  const { currentUser } = useAppStore();
  const { isCompact: compact } = useViewport();
  const confirm = useConfirm();
  const [workspaces, setWorkspaces] = useState<AdminWorkspace[] | null>(null);
  const [users, setUsers] = useState<AdminUser[] | null>(null);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState<string>('');

  const load = useCallback(async () => {
    try {
      const [w, u] = await Promise.all([adminAPI.workspaces(), adminAPI.users()]);
      setWorkspaces(w.data || []);
      setUsers(u.data || []);
    } catch (err: any) {
      setError(apiErrorMessage(err, 'Could not load the platform overview'));
    }
  }, []);

  useEffect(() => {
    if (currentUser?.is_admin) load();
  }, [currentUser?.is_admin, load]);

  if (currentUser && !currentUser.is_admin) return <Navigate to="/projects" replace />;

  const flash = (text: string) => {
    setNotice(text);
    window.setTimeout(() => setNotice(''), 4000);
  };

  const changePlan = async (ws: AdminWorkspace, plan: string) => {
    if (plan === ws.plan) return;
    const ok = await confirm({
      title: 'Change plan',
      message:
        plan === 'open_source'
          ? `Move "${ws.name}" to the open-source plan? Every project's latest baseline becomes public on the open-source page.`
          : `Move "${ws.name}" from ${planLabel(ws.plan)} to ${planLabel(plan)}?`,
      confirmLabel: 'Change plan',
    });
    if (!ok) return;
    setBusy(ws.id);
    setError('');
    try {
      await adminAPI.setPlan(ws.id, plan);
      flash(`${ws.name} is now on ${planLabel(plan)}.`);
      await load();
    } catch (err: any) {
      setError(`Could not change the plan: ${apiErrorMessage(err)}`);
    } finally {
      setBusy('');
    }
  };

  const toggleAdmin = async (u: AdminUser) => {
    const grant = !u.is_admin;
    const ok = await confirm({
      title: grant ? 'Make platform admin' : 'Remove platform admin',
      message: grant
        ? `${u.name || u.email} will be able to see every workspace, change any plan and make other platform admins. Continue?`
        : `${u.name || u.email} will lose platform-admin standing. Continue?`,
      confirmLabel: grant ? 'Make admin' : 'Remove',
    });
    if (!ok) return;
    setBusy(u.id);
    setError('');
    try {
      await adminAPI.setAdmin(u.id, grant);
      flash(grant ? `${u.name || u.email} is now a platform admin.` : `${u.name || u.email} is no longer a platform admin.`);
      await load();
    } catch (err: any) {
      setError(`Could not update platform-admin standing: ${apiErrorMessage(err)}`);
    } finally {
      setBusy('');
    }
  };

  const selectStyle: React.CSSProperties = { padding: '5px 8px', fontSize: 13, width: 'auto', minWidth: 140 };
  const linkButton: React.CSSProperties = {
    background: 'none',
    border: '1px solid var(--border)',
    borderRadius: 4,
    color: 'var(--text)',
    cursor: 'pointer',
    fontSize: 13,
    width: 'auto',
    padding: '5px 10px',
    minHeight: 32,
  };

  return (
    <div style={{ minHeight: '100vh', background: 'var(--bg-app)' }}>
      <Navbar title="Platform admin" showWorkspaceControls />
      <div style={{ maxWidth: 1000, margin: '0 auto', padding: compact ? '16px 12px' : '24px 20px' }}>
        <p style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 0 }}>
          The whole deployment: every workspace and its plan, and who administers the platform. A plan decides a
          workspace's limits and release channel; the open-source plan also publishes each project's latest baseline.
        </p>
        {error && <ErrorBanner message={error} onDismiss={() => setError('')} />}
        {notice && (
          <div className="card" style={{ padding: '10px 14px', marginBottom: 12, fontSize: 13, color: 'var(--success-text)' }}>
            {notice}
          </div>
        )}

        <section className="card" style={{ padding: compact ? 14 : 20, marginBottom: 16 }}>
          <h2 style={{ margin: '0 0 10px', fontSize: 18 }}>Workspaces</h2>
          {workspaces === null ? (
            <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading…</div>
          ) : (
            <div className="table-scroll">
              <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                <thead>
                  <tr>
                    <th style={th}>Workspace</th>
                    <th style={th}>Type</th>
                    <th style={{ ...th, width: 170 }}>Plan</th>
                    <th style={{ ...th, width: 90 }}>Members</th>
                    <th style={{ ...th, width: 110 }}>Created</th>
                  </tr>
                </thead>
                <tbody>
                  {workspaces.map((ws) => (
                    <tr key={ws.id}>
                      <td style={td}>
                        <div style={{ fontWeight: 600 }}>{ws.name}</div>
                        <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>{ws.slug}</div>
                      </td>
                      <td style={td}>{ws.type}</td>
                      <td style={td}>
                        <select
                          value={PLANS.some((p) => p.value === ws.plan) ? ws.plan : ''}
                          onChange={(e) => changePlan(ws, e.target.value)}
                          disabled={busy === ws.id}
                          aria-label={`Plan for ${ws.name}`}
                          style={selectStyle}
                        >
                          {!PLANS.some((p) => p.value === ws.plan) && <option value="">{ws.plan}</option>}
                          {PLANS.map((p) => (
                            <option key={p.value} value={p.value}>
                              {p.label}
                            </option>
                          ))}
                        </select>
                      </td>
                      <td style={td}>{ws.members}</td>
                      <td style={td}>{new Date(ws.created_at).toLocaleDateString()}</td>
                    </tr>
                  ))}
                  {workspaces.length === 0 && (
                    <tr>
                      <td style={td} colSpan={5}>
                        No workspaces yet.
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          )}
        </section>

        <section className="card" style={{ padding: compact ? 14 : 20 }}>
          <h2 style={{ margin: '0 0 6px', fontSize: 18 }}>Platform admins</h2>
          <p style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 0 }}>
            A platform admin passes every check: every workspace, every project, every plan. The first account ever
            registered on a deployment is one; grant it sparingly. You cannot remove your own standing, and the last
            admin cannot be removed.
          </p>
          {users === null ? (
            <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading…</div>
          ) : (
            <div className="table-scroll">
              <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                <thead>
                  <tr>
                    <th style={th}>Person</th>
                    <th style={th}>Email</th>
                    <th style={{ ...th, width: 110 }}>Sign-in</th>
                    <th style={{ ...th, width: 150 }}>Platform admin</th>
                  </tr>
                </thead>
                <tbody>
                  {users.map((u) => (
                    <tr key={u.id}>
                      <td style={td}>
                        {u.name || '—'}
                        {currentUser && u.id === currentUser.id && (
                          <span style={{ color: 'var(--text-muted)', fontSize: 12 }}> (you)</span>
                        )}
                      </td>
                      <td style={td}>{u.email}</td>
                      <td style={td}>{u.auth_provider || 'password'}</td>
                      <td style={td}>
                        {u.is_admin ? (
                          <span style={{ display: 'inline-flex', gap: 8, alignItems: 'center' }}>
                            <span className="badge">admin</span>
                            {currentUser && u.id !== currentUser.id && (
                              <button type="button" onClick={() => toggleAdmin(u)} disabled={busy === u.id} style={linkButton}>
                                Remove
                              </button>
                            )}
                          </span>
                        ) : (
                          <button type="button" onClick={() => toggleAdmin(u)} disabled={busy === u.id} style={linkButton}>
                            Make admin
                          </button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      </div>
    </div>
  );
};
