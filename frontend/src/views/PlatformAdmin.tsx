import React, { Suspense, useCallback, useEffect, useState } from 'react';
import { Navigate } from 'react-router-dom';
import { AdminUser, AdminWorkspace, PLANS, adminAPI } from '../api/client';
import { ADMIN_USAGE_FEATURE } from '../features';
import { useFeature } from '../hooks/useFeature';
import { AdminToggle, PlanSelect, ResetLinkButton, linkButton } from '../components/admin/adminControls';
import { UsageOverview } from '../components/admin/UsageOverview';
import { apiErrorMessage } from '../api/errors';
import { Navbar } from '../components/Navbar';
import { UserDashboard } from '../components/admin/UserDashboard';
import { ErrorBanner, useConfirm } from '../components/ui';
import { useAppStore } from '../state/store';
import { useViewport } from '../hooks/useViewport';
import { registeredAdminSections } from '../ext/registry';

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
  // What modules add here (ext/modules.ts): the hosted service's site
  // Visitors dashboard, which a self-hosted OpenV has none of.
  const sections = registeredAdminSections(useAppStore((s) => s.capabilities));
  const confirm = useConfirm();
  const [workspaces, setWorkspaces] = useState<AdminWorkspace[] | null>(null);
  const [users, setUsers] = useState<AdminUser[] | null>(null);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState<string>('');
  // A reset link minted for one account (REQ-158): shown once, here, for
  // the admin to pass on; the server never repeats it.
  const [resetLink, setResetLink] = useState<{ user: AdminUser; link: string; expiresAt: string } | null>(null);
  const [copied, setCopied] = useState(false);
  // The usage view replaces the two plain tables once it is in the release.
  const usageOn = useFeature(ADMIN_USAGE_FEATURE);
  const [reload, setReload] = useState(0);

  const load = useCallback(async () => {
    try {
      const [w, u] = await Promise.all([adminAPI.workspaces(), adminAPI.users()]);
      setWorkspaces(w.data || []);
      setUsers(u.data || []);
      setReload((n) => n + 1);
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

  const issueResetLink = async (u: AdminUser) => {
    const ok = await confirm({
      title: 'Make a password reset link',
      message: `A link that lets whoever holds it set a new password for ${u.name || u.email}. It works once and expires after 24 hours. Pass it to the person yourself; nothing is emailed.`,
      confirmLabel: 'Make link',
    });
    if (!ok) return;
    setBusy(u.id);
    setError('');
    setCopied(false);
    try {
      const res = await adminAPI.issuePasswordReset(u.id);
      setResetLink({ user: u, link: res.data.link, expiresAt: res.data.expires_at });
    } catch (err: any) {
      setError(`Could not make a reset link: ${apiErrorMessage(err)}`);
    } finally {
      setBusy('');
    }
  };

  const copyResetLink = async () => {
    if (!resetLink) return;
    try {
      await navigator.clipboard.writeText(resetLink.link);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };

  const personActions = { currentUserId: currentUser?.id, busy, onToggleAdmin: toggleAdmin, onIssueReset: issueResetLink };

  const resetBanner = resetLink && (
      <div
        role="status"
        className="card"
        style={{ padding: '12px 14px', marginBottom: 12, background: 'var(--tint-blue)', border: '1px solid var(--accent)' }}
      >
        <div style={{ fontSize: 13, marginBottom: 6 }}>
          Reset link for <strong>{resetLink.user.name || resetLink.user.email}</strong>, valid until{' '}
          {new Date(resetLink.expiresAt).toLocaleString()}. It is shown once: copy it now and pass it on.
        </div>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          <input
            type="text"
            readOnly
            value={resetLink.link}
            aria-label="Password reset link"
            onFocus={(e) => e.currentTarget.select()}
            style={{ flex: '1 1 240px', padding: '6px 8px', fontSize: 12, fontFamily: 'var(--font-mono, monospace)' }}
          />
          <button type="button" onClick={copyResetLink} style={linkButton}>
            {copied ? 'Copied' : 'Copy'}
          </button>
          <button type="button" onClick={() => setResetLink(null)} style={linkButton}>
            Done
          </button>
        </div>
      </div>
  );

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

        {sections.map(({ key, Component }) => (
          <Suspense key={key} fallback={null}>
            <Component compact={compact} />
          </Suspense>
        ))}

        <UserDashboard compact={compact} />

        {usageOn && resetBanner}
        {usageOn && (
          <UsageOverview compact={compact} workspaces={workspaces} users={users} onChangePlan={changePlan} reload={reload} {...personActions} />
        )}

        <section className="card" hidden={usageOn} style={{ padding: compact ? 14 : 20, marginBottom: 16 }}>
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
                        <PlanSelect ws={ws} disabled={busy === ws.id} onChange={changePlan} />
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
          <p style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: 0 }}>
            <em>Reset link</em> makes a one-time password reset link for a password account, for when the person
            cannot use the emailed one or the server sends no mail.
          </p>
          {!usageOn && resetBanner}
          {usageOn ? (
            <div style={{ fontSize: 13 }}>
              Make admins and reset links from <em>Usage</em>, <em>By person</em>.
            </div>
          ) : users === null ? (
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
                    <th style={{ ...th, width: 120 }}>Password</th>
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
                        <AdminToggle u={u} {...personActions} />
                      </td>
                      <td style={td}>
                        <ResetLinkButton u={u} {...personActions} />
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
