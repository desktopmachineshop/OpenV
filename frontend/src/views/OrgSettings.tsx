import React, { useEffect, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { orgsAPI } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';
import { Navbar } from '../components/Navbar';
import { OrgMembersTab } from '../components/org/OrgMembersTab';
import { OrgTeamsTab } from '../components/org/OrgTeamsTab';
import { OrgProvidersTab } from '../components/org/OrgProvidersTab';
import { WorkerKeysTab } from '../components/org/WorkerKeysTab';
import { ErrorBanner } from '../components/ui';

type Tab = 'general' | 'members' | 'teams' | 'providers' | 'worker-keys';

const TABS: { key: Tab; label: string }[] = [
  { key: 'general', label: 'General' },
  { key: 'members', label: 'Members' },
  { key: 'teams', label: 'Teams' },
  { key: 'providers', label: 'AI Providers' },
  { key: 'worker-keys', label: 'Runners' },
];

// Workspace (org) settings: general info, members, people-teams, AI providers
// and worker keys. Rendered outside any project (route /org/settings).
export const OrgSettings: React.FC = () => {
  const navigate = useNavigate();
  const { orgs, activeOrgId, orgsLoaded, setOrgs, currentUser } = useAppStore();
  const org = orgs.find((o) => o.id === activeOrgId) || null;
  const isAdmin = Boolean(org && (org.role === 'admin' || currentUser?.is_admin));

  // The active tab lives in the URL (?tab=…) so refreshes and deep links keep
  // it; unknown values fall back to the first tab.
  const [searchParams, setSearchParams] = useSearchParams();
  const tabParam = searchParams.get('tab');
  const tab: Tab = TABS.some((t) => t.key === tabParam) ? (tabParam as Tab) : TABS[0].key;
  const setTab = (next: Tab) =>
    setSearchParams(
      (prev) => {
        prev.set('tab', next);
        return prev;
      },
      { replace: true }
    );
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  // General tab
  const [nameDraft, setNameDraft] = useState('');
  const [savingName, setSavingName] = useState(false);

  useEffect(() => {
    if (org) setNameDraft(org.name);
  }, [org]);

  useEffect(() => {
    if (orgsLoaded && !org) navigate('/projects');
  }, [orgsLoaded, org, navigate]);

  if (!orgsLoaded) {
    return <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>Loading…</div>;
  }
  if (!org) return null;

  const flash = (msg: string) => {
    setNotice(msg);
    window.setTimeout(() => setNotice(''), 2500);
  };

  const handleSaveName = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!nameDraft.trim() || nameDraft.trim() === org.name) return;
    setSavingName(true);
    setError('');
    try {
      const res = await orgsAPI.update(org.id, { name: nameDraft.trim() });
      setOrgs(orgs.map((o) => (o.id === org.id ? { ...o, ...res.data } : o)));
      flash('Workspace name updated.');
    } catch (err: any) {
      setError(`Failed to update workspace: ${apiErrorMessage(err)}`);
    } finally {
      setSavingName(false);
    }
  };

  return (
    <div style={{ minHeight: '100vh', background: 'var(--bg-app)' }}>
      <Navbar title="Workspace Settings" showWorkspaceControls />
      <div style={{ padding: 24, maxWidth: 900, margin: '0 auto' }}>
        <Link to="/projects" style={{ fontSize: 13, color: 'var(--accent)', textDecoration: 'none' }}>
          ← Back to projects
        </Link>
        <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, margin: '10px 0 16px' }}>
          <h2 style={{ color: 'var(--text)', margin: 0 }}>{org.name}</h2>
          <span
            style={{
              display: 'inline-block',
              padding: '2px 10px',
              borderRadius: 10,
              fontSize: 11,
              fontWeight: 600,
              color: org.type === 'personal' ? 'var(--text-muted)' : '#fff',
              background: org.type === 'personal' ? 'var(--neutral-soft)' : 'var(--accent)',
            }}
          >
            {org.type === 'personal' ? 'personal workspace' : 'company workspace'}
          </span>
        </div>

        <div style={{ display: 'flex', gap: 4, borderBottom: '2px solid var(--border)', marginBottom: 20 }}>
          {TABS.map((t) => (
            <button
              key={t.key}
              onClick={() => setTab(t.key)}
              style={{
                background: 'none',
                border: 'none',
                borderBottom: tab === t.key ? '2px solid var(--accent)' : '2px solid transparent',
                marginBottom: -2,
                padding: '10px 16px',
                fontSize: 14,
                fontWeight: tab === t.key ? 600 : 400,
                color: tab === t.key ? 'var(--text)' : 'var(--text-muted)',
                cursor: 'pointer',
                width: 'auto',
              }}
            >
              {t.label}
            </button>
          ))}
        </div>

        <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 16 }} />
        {notice && (
          <div
            style={{
              background: 'var(--tint-green)',
              border: '1px solid var(--success)',
              color: 'var(--success-text)',
              padding: '10px 14px',
              borderRadius: 4,
              marginBottom: 16,
              fontSize: 13,
            }}
          >
            {notice}
          </div>
        )}

        {tab === 'general' && (
          <>
            <div className="card">
              <h3>Workspace name</h3>
              {isAdmin ? (
                <form onSubmit={handleSaveName} style={{ display: 'flex', gap: 10, alignItems: 'flex-end', flexWrap: 'wrap' }}>
                  <div style={{ flex: 1, minWidth: 220 }}>
                    <label style={{ fontSize: 12 }}>Name</label>
                    <input value={nameDraft} onChange={(e) => setNameDraft(e.target.value)} />
                  </div>
                  <button
                    type="submit"
                    className="button"
                    disabled={savingName || !nameDraft.trim() || nameDraft.trim() === org.name}
                  >
                    {savingName ? 'Saving…' : 'Save'}
                  </button>
                </form>
              ) : (
                <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 0 }}>
                  Only workspace admins can rename the workspace.
                </p>
              )}
            </div>

            <div className="card">
              <h3>Plan</h3>
              <p style={{ fontSize: 13, color: 'var(--text)', marginBottom: 0 }}>
                Plan: <strong>{org.plan || 'Free'}</strong> — billing coming soon.
              </p>
            </div>

            <div className="card">
              <h3>Details</h3>
              <div style={{ fontSize: 13, color: 'var(--text)', display: 'grid', gridTemplateColumns: '110px 1fr', rowGap: 8 }}>
                <span style={{ color: 'var(--text-muted)' }}>Workspace ID</span>
                <code style={{ fontSize: 12 }}>{org.id}</code>
                <span style={{ color: 'var(--text-muted)' }}>Slug</span>
                <code style={{ fontSize: 12 }}>{org.slug || '—'}</code>
                <span style={{ color: 'var(--text-muted)' }}>Created</span>
                <span>{org.created_at ? new Date(org.created_at).toLocaleDateString() : '—'}</span>
              </div>
            </div>
          </>
        )}

        {tab === 'members' && <OrgMembersTab org={org} isAdmin={isAdmin} currentUser={currentUser} />}
        {tab === 'teams' && <OrgTeamsTab org={org} isAdmin={isAdmin} />}
        {tab === 'providers' && <OrgProvidersTab isAdmin={isAdmin} />}
        {tab === 'worker-keys' && <WorkerKeysTab org={org} isAdmin={isAdmin} />}
      </div>
    </div>
  );
};
