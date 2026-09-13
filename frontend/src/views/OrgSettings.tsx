import React, { useEffect, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { Org, OrgFeatures, orgsAPI, releaseAPI } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';
import { Navbar } from '../components/Navbar';
import { OrgLimitsTab } from '../components/org/OrgLimitsTab';
import { OrgMembersTab } from '../components/org/OrgMembersTab';
import { OrgTeamsTab } from '../components/org/OrgTeamsTab';
import { OrgProvidersTab } from '../components/org/OrgProvidersTab';
import { WorkerKeysTab } from '../components/org/WorkerKeysTab';
import { OrgUsageTab } from '../components/org/OrgUsageTab';
import { ErrorBanner } from '../components/ui';
import { QualityRulesEditor } from '../components/QualityRulesEditor';
import { useViewport } from '../hooks/useViewport';

type Tab = 'general' | 'members' | 'teams' | 'providers' | 'worker-keys' | 'quality' | 'usage' | 'limits';

const TABS: { key: Tab; label: string }[] = [
  { key: 'general', label: 'General' },
  { key: 'members', label: 'Members' },
  { key: 'limits', label: 'Limits' },
  { key: 'teams', label: 'Teams' },
  { key: 'providers', label: 'AI Providers' },
  { key: 'worker-keys', label: 'Runners' },
  { key: 'quality', label: 'Quality rules' },
  { key: 'usage', label: 'Usage' },
];

// Workspace (org) settings: general info, members, people-teams, AI providers
// and worker keys. Rendered outside any project (route /org/settings).
export const OrgSettings: React.FC = () => {
  const navigate = useNavigate();
  const { orgs, activeOrgId, orgsLoaded, setOrgs, currentUser } = useAppStore();
  const org = orgs.find((o) => o.id === activeOrgId) || null;
  const isAdmin = Boolean(org && (org.role === 'admin' || currentUser?.is_admin));
  const { isPhone } = useViewport();

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
  const [deleteConfirm, setDeleteConfirm] = useState('');
  const [deleting, setDeleting] = useState(false);
  // Logo card: logoVersion busts the browser cache after an upload so the
  // <img> refetches the new file from the same URL.
  const [logoBusy, setLogoBusy] = useState(false);
  const [logoError, setLogoError] = useState('');
  const [logoVersion, setLogoVersion] = useState(0);
  // Release channel (REQ-136). The running release comes from the API so
  // the card can say which release the workspace is on today.
  const [channelSaving, setChannelSaving] = useState(false);
  const [currentRelease, setCurrentRelease] = useState('');
  useEffect(() => {
    let cancelled = false;
    releaseAPI
      .current()
      .then((res) => {
        if (!cancelled) setCurrentRelease(res.data.version || '');
      })
      .catch(() => {
        // The card still shows the channel without a release name.
      });
    return () => {
      cancelled = true;
    };
  }, []);
  // Upgrade window and the caller's own preview (REQ-138). The gates come
  // from the store (loaded per active workspace) when this is the active
  // workspace, and are fetched here otherwise.
  const { features: activeFeatures, setFeatures } = useAppStore();
  const [windowDay, setWindowDay] = useState(0);
  const [windowHour, setWindowHour] = useState(9);
  const [windowTz, setWindowTz] = useState('');
  const [windowSaving, setWindowSaving] = useState(false);
  const [previewSaving, setPreviewSaving] = useState(false);
  const [gates, setGates] = useState<OrgFeatures | null>(null);
  useEffect(() => {
    if (!org) return;
    setWindowDay(org.upgrade_day || 0);
    setWindowHour(org.upgrade_day ? org.upgrade_hour || 0 : 9);
    setWindowTz(org.upgrade_timezone || Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC');
  }, [org?.id, org?.upgrade_day, org?.upgrade_hour, org?.upgrade_timezone]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (!org) return;
    if (org.id === activeOrgId && activeFeatures) {
      setGates(activeFeatures);
      return;
    }
    let cancelled = false;
    orgsAPI
      .features(org.id)
      .then((res) => {
        if (!cancelled) setGates(res.data);
      })
      .catch(() => {
        // The card shows the window without the schedule.
      });
    return () => {
      cancelled = true;
    };
  }, [org?.id, activeOrgId, activeFeatures]); // eslint-disable-line react-hooks/exhaustive-deps

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

  const handleDelete = async (e: React.FormEvent) => {
    e.preventDefault();
    if (deleteConfirm !== org.name) return;
    setDeleting(true);
    setError('');
    try {
      await orgsAPI.remove(org.id);
      // The workspace is gone from the picker; land the user in another one.
      const remaining = orgs.filter((o) => o.id !== org.id);
      setOrgs(remaining);
      const fallback = remaining.find((o) => o.type === 'personal') || remaining[0];
      if (fallback) {
        orgsAPI.activate(fallback.id).catch(() => {});
        useAppStore.getState().setActiveOrgId(fallback.id, { clearProjects: true });
      }
      navigate('/projects');
    } catch (err: any) {
      setError(`Failed to delete workspace: ${apiErrorMessage(err)}`);
      setDeleting(false);
    }
  };

  const applyGates = (next: OrgFeatures) => {
    setGates(next);
    if (org && org.id === activeOrgId) setFeatures(next);
  };
  const handleSaveWindow = async (e: React.FormEvent) => {
    e.preventDefault();
    setWindowSaving(true);
    setError('');
    try {
      const res = await orgsAPI.update(org.id, {
        upgrade_window: windowDay ? { day: windowDay, hour: windowHour, timezone: windowTz } : null,
      } as Partial<Org>);
      setOrgs(orgs.map((o) => (o.id === org.id ? { ...o, ...res.data } : o)));
      flash(windowDay ? 'Upgrade window saved.' : 'Upgrade window cleared: stable releases turn on at the cut.');
      const gatesRes = await orgsAPI.features(org.id);
      applyGates(gatesRes.data);
    } catch (err: any) {
      setError(`Failed to save the upgrade window: ${apiErrorMessage(err)}`);
    } finally {
      setWindowSaving(false);
    }
  };
  const handlePreviewToggle = async (enabled: boolean) => {
    setPreviewSaving(true);
    setError('');
    try {
      const res = await orgsAPI.setStablePreview(org.id, enabled);
      applyGates(res.data);
      flash(enabled ? 'You are now previewing the next stable release.' : 'Preview off.');
    } catch (err: any) {
      setError(`Failed to change the preview: ${apiErrorMessage(err)}`);
    } finally {
      setPreviewSaving(false);
    }
  };

  const handleChannelChange = async (e: React.ChangeEvent<HTMLSelectElement>) => {
    const channel = e.target.value as 'nightly' | 'stable';
    setChannelSaving(true);
    setError('');
    try {
      const res = await orgsAPI.update(org.id, { release_channel: channel });
      setOrgs(orgs.map((o) => (o.id === org.id ? { ...o, ...res.data } : o)));
      flash(`Release channel set to ${channel}.`);
    } catch (err: any) {
      setError(`Failed to change the release channel: ${apiErrorMessage(err)}`);
    } finally {
      setChannelSaving(false);
    }
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

  const applyOrg = (updated: Partial<typeof org>) =>
    setOrgs(orgs.map((o) => (o.id === org.id ? { ...o, ...updated } : o)));

  const handleLogoChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const input = e.target;
    const file = input.files?.[0];
    if (!file) return;
    setLogoBusy(true);
    setLogoError('');
    try {
      const res = await orgsAPI.uploadLogo(org.id, file);
      applyOrg(res.data);
      setLogoVersion((v) => v + 1);
      flash('Workspace logo updated.');
    } catch (err: any) {
      setLogoError(`Failed to upload logo: ${apiErrorMessage(err)}`);
    } finally {
      setLogoBusy(false);
      // Reset so picking the same file again re-triggers onChange.
      input.value = '';
    }
  };

  const handleRemoveLogo = async () => {
    setLogoBusy(true);
    setLogoError('');
    try {
      const res = await orgsAPI.removeLogo(org.id);
      applyOrg(res.data);
      flash('Workspace logo removed.');
    } catch (err: any) {
      setLogoError(`Failed to remove logo: ${apiErrorMessage(err)}`);
    } finally {
      setLogoBusy(false);
    }
  };

  return (
    <div style={{ minHeight: '100vh', background: 'var(--bg-app)' }}>
      <Navbar title="Workspace Settings" showWorkspaceControls />
      <div style={{ padding: 24, maxWidth: 900, margin: '0 auto' }}>
        <Link to="/projects" style={{ fontSize: 13, color: 'var(--accent)', textDecoration: 'none' }}>
          ← Back to projects
        </Link>
        <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, margin: '10px 0 16px', flexWrap: 'wrap' }}>
          <h2 style={{ color: 'var(--text)', margin: 0, overflowWrap: 'anywhere' }}>{org.name}</h2>
          <span
            style={{
              display: 'inline-block',
              padding: '2px 10px',
              borderRadius: 10,
              fontSize: 12,
              fontWeight: 600,
              color: org.type === 'personal' ? 'var(--text-muted)' : 'var(--accent-fg)',
              background: org.type === 'personal' ? 'var(--neutral-soft)' : 'var(--accent)',
            }}
          >
            {org.type === 'personal' ? 'personal workspace' : 'company workspace'}
          </span>
        </div>

        <div className="tab-strip" role="tablist">
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
                  <div style={{ flex: 1, minWidth: 220, maxWidth: 480 }}>
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
              <h3>Workspace logo</h3>
              <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
                Shown on the cover page of PDF and Word downloads. PNG, JPG, GIF or WebP up to 2 MB.
              </p>
              <div style={{ display: 'flex', gap: 16, alignItems: 'center', flexWrap: 'wrap' }}>
                {org.has_logo ? (
                  <img
                    src={`${orgsAPI.logoUrl(org.id)}?v=${logoVersion}`}
                    alt="Workspace logo"
                    style={{ maxHeight: 64, maxWidth: 240, objectFit: 'contain' }}
                  />
                ) : (
                  <span style={{ fontSize: 13, color: 'var(--text-muted)' }}>No logo yet</span>
                )}
                {isAdmin && (
                  <div style={{ display: 'flex', gap: 10, alignItems: 'center', flexWrap: 'wrap' }}>
                    <input
                      type="file"
                      accept="image/png,image/jpeg,image/gif,image/webp"
                      aria-label="Upload workspace logo"
                      disabled={logoBusy}
                      onChange={handleLogoChange}
                      style={{ fontSize: 13 }}
                    />
                    {org.has_logo && (
                      <button type="button" className="button-secondary" disabled={logoBusy} onClick={handleRemoveLogo}>
                        {logoBusy ? 'Working…' : 'Remove logo'}
                      </button>
                    )}
                  </div>
                )}
              </div>
              {logoError && (
                <p style={{ fontSize: 13, color: 'var(--danger-text, #c0392b)', marginBottom: 0, marginTop: 8 }}>
                  {logoError}
                </p>
              )}
            </div>

            <div className="card">
              <h3>Plan</h3>
              <p style={{ fontSize: 13, color: 'var(--text)', marginBottom: 0 }}>
                Plan: <strong>{org.plan || 'Free'}</strong> — free during the alpha with every feature included. See <a href="/pricing" target="_blank" rel="noreferrer">what free means</a>.
              </p>
            </div>

            <div className="card">
              <h3>Release channel</h3>
              <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
                {org.release_channel === 'stable'
                  ? 'Stable: new features turn on for this workspace at the monthly release. Fixes arrive with every nightly.'
                  : 'Nightly: every release reaches this workspace the day it ships.'}
                {org.release_channel_locked && ' This plan always runs the nightly channel.'}
                {currentRelease && ` The platform is on release ${currentRelease}.`}
              </p>
              {isAdmin && !org.release_channel_locked ? (
                <label style={{ fontSize: 13, display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
                  Channel
                  <select
                    value={org.release_channel || 'stable'}
                    disabled={channelSaving}
                    onChange={handleChannelChange}
                    aria-label="Release channel"
                    style={{ padding: '5px 8px', fontSize: 13 }}
                  >
                    <option value="stable">Stable (monthly)</option>
                    <option value="nightly">Nightly (latest)</option>
                  </select>
                </label>
              ) : (
                <p style={{ fontSize: 13, color: 'var(--text)', marginBottom: 0 }}>
                  Channel: <strong>{org.release_channel === 'stable' ? 'Stable' : 'Nightly'}</strong>
                </p>
              )}
              {org.release_channel === 'stable' && (
                <div style={{ marginTop: 14, borderTop: '1px solid var(--surface-inset)', paddingTop: 12 }}>
                  <p style={{ fontSize: 13, color: 'var(--text)', marginTop: 0 }}>
                    {org.stable_release
                      ? `This workspace runs stable release ${org.stable_release}.`
                      : 'No stable release has turned on for this workspace yet.'}
                    {gates?.next_stable_release && gates.next_stable_at && (
                      <>
                        {' '}Stable release {gates.next_stable_release} turns on{' '}
                        <strong>{new Date(gates.next_stable_at).toLocaleString()}</strong>.
                      </>
                    )}
                  </p>
                  {isAdmin && (
                    <form onSubmit={handleSaveWindow} style={{ display: 'flex', gap: 10, alignItems: 'flex-end', flexWrap: 'wrap' }}>
                      <div>
                        <label style={{ fontSize: 12, display: 'block' }}>Upgrade window</label>
                        <select
                          value={windowDay}
                          onChange={(e) => setWindowDay(Number(e.target.value))}
                          aria-label="Upgrade day of month"
                          style={{ padding: '5px 8px', fontSize: 13 }}
                        >
                          <option value={0}>At the cut</option>
                          {Array.from({ length: 28 }, (_, i) => i + 1).map((d) => (
                            <option key={d} value={d}>
                              Day {d} of the month
                            </option>
                          ))}
                        </select>
                      </div>
                      {windowDay > 0 && (
                        <>
                          <div>
                            <label style={{ fontSize: 12, display: 'block' }}>Hour</label>
                            <select
                              value={windowHour}
                              onChange={(e) => setWindowHour(Number(e.target.value))}
                              aria-label="Upgrade hour"
                              style={{ padding: '5px 8px', fontSize: 13 }}
                            >
                              {Array.from({ length: 24 }, (_, i) => i).map((h) => (
                                <option key={h} value={h}>
                                  {String(h).padStart(2, '0')}:00
                                </option>
                              ))}
                            </select>
                          </div>
                          <div style={{ flex: '1 1 180px', maxWidth: 260 }}>
                            <label style={{ fontSize: 12, display: 'block' }}>Time zone</label>
                            <input
                              value={windowTz}
                              onChange={(e) => setWindowTz(e.target.value)}
                              aria-label="Upgrade time zone"
                              placeholder="Europe/London"
                              style={{ width: '100%', padding: '5px 8px', fontSize: 13 }}
                            />
                          </div>
                        </>
                      )}
                      <button type="submit" className="button-primary" disabled={windowSaving}>
                        {windowSaving ? 'Saving…' : 'Save window'}
                      </button>
                    </form>
                  )}
                  <p style={{ fontSize: 12, color: 'var(--text-muted)', margin: '8px 0 0' }}>
                    A stable release turns on at the window that follows its cut, and no later than 14 days after it.
                    Fixes reach every workspace with each nightly regardless.
                  </p>
                  <label style={{ fontSize: 13, display: 'flex', alignItems: 'center', gap: 8, marginTop: 10 }}>
                    <input
                      type="checkbox"
                      checked={Boolean(gates?.preview)}
                      disabled={previewSaving}
                      onChange={(e) => void handlePreviewToggle(e.target.checked)}
                    />
                    Try the next stable release early for my account only
                  </label>
                </div>
              )}
            </div>

            <div className="card">
              <h3>Details</h3>
              <div
                style={{
                  fontSize: 13,
                  color: 'var(--text)',
                  display: 'grid',
                  // One column on a phone: a UUID beside a label does not fit.
                  gridTemplateColumns: isPhone ? '1fr' : '110px 1fr',
                  rowGap: isPhone ? 4 : 8,
                }}
              >
                <span style={{ color: 'var(--text-muted)' }}>Workspace ID</span>
                <code style={{ fontSize: 12, wordBreak: 'break-all', marginBottom: isPhone ? 8 : 0 }}>{org.id}</code>
                <span style={{ color: 'var(--text-muted)' }}>Slug</span>
                <code style={{ fontSize: 12, wordBreak: 'break-all', marginBottom: isPhone ? 8 : 0 }}>{org.slug || '—'}</code>
                <span style={{ color: 'var(--text-muted)' }}>Created</span>
                <span>{org.created_at ? new Date(org.created_at).toLocaleDateString() : '—'}</span>
              </div>
            </div>

            {isAdmin && org.type !== 'personal' && (
              <div className="card" style={{ borderColor: 'var(--danger, #c0392b)' }}>
                <h3 style={{ color: 'var(--danger-text, #c0392b)' }}>Danger zone</h3>
                <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
                  Deleting this workspace hides it immediately and locks out every member. All its
                  projects, requirements, links, baselines, and agent data are kept for{' '}
                  <strong>30 days</strong> — an admin can restore it in that window — and then
                  permanently deleted. Type the workspace name to confirm.
                </p>
                <form onSubmit={handleDelete} style={{ display: 'flex', gap: 10, alignItems: 'flex-end', flexWrap: 'wrap' }}>
                  <div style={{ flex: 1, minWidth: 220, maxWidth: 480 }}>
                    <label style={{ fontSize: 12 }}>Workspace name</label>
                    <input
                      value={deleteConfirm}
                      onChange={(e) => setDeleteConfirm(e.target.value)}
                      placeholder={org.name}
                    />
                  </div>
                  <button
                    type="submit"
                    className="button"
                    style={{ background: 'var(--danger, #c0392b)', color: '#fff' }}
                    disabled={deleting || deleteConfirm !== org.name}
                  >
                    {deleting ? 'Deleting…' : 'Delete workspace'}
                  </button>
                </form>
              </div>
            )}
          </>
        )}

        {tab === 'members' && <OrgMembersTab org={org} isAdmin={isAdmin} currentUser={currentUser} />}
        {tab === 'limits' && <OrgLimitsTab org={org} />}
        {tab === 'teams' && <OrgTeamsTab org={org} isAdmin={isAdmin} />}
        {tab === 'providers' && <OrgProvidersTab isAdmin={isAdmin} />}
        {tab === 'worker-keys' && <WorkerKeysTab org={org} isAdmin={isAdmin} />}
        {tab === 'quality' && (
          <div className="card">
            <h3>Requirement quality rules</h3>
            <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
              The house style every project in this workspace inherits: which keywords state a
              binding requirement, and how loudly each wording check speaks. A project can override
              any of it in its own settings. Agents read the resolved rules before they draft.
            </p>
            <QualityRulesEditor
              level="workspace"
              id={org.id}
              canEdit={isAdmin}
              onSaved={() => flash('Quality rules saved')}
            />
            {!isAdmin && (
              <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 12, marginBottom: 0 }}>
                Only workspace admins can change the house style.
              </p>
            )}
          </div>
        )}

        {tab === 'usage' && <OrgUsageTab org={org} />}
      </div>
    </div>
  );
};
