import React, { useCallback, useEffect, useState } from 'react';
import { useAppStore } from '../state/store';
import { ProviderSetting, providerSettingsAPI, notificationPrefsAPI } from '../api/client';
import { MyRunnerCard } from './org/MyRunnerCard';
import { CloudRunnerCard } from './org/CloudRunnerCard';
import { ProviderConnectCard } from './agents/ProviderConnectCard';
import { ThemeSwitcher } from './ThemeSwitcher';

interface UserSettingsPanelProps {
  onClose: () => void;
}

// CLI providers that support local subscription sign-in.
const CLI_PROVIDERS: { key: string; label: string }[] = [
  { key: 'claude-code', label: 'Claude Code' },
  { key: 'codex-cli', label: 'Codex CLI' },
  { key: 'gemini-cli', label: 'Gemini CLI' },
];

// UserSettingsPanel is the per-user settings modal opened from the user info
// block in the bottom-left of the sidebar. This is where local agent auth
// lives: the user's personal runner and their own CLI provider sign-ins,
// which execute on their machine via the Agent Connector. (Projects only
// choose between "user account" and "API key" auth — the sign-in itself
// always happens here.)
export const UserSettingsPanel: React.FC<UserSettingsPanelProps> = ({ onClose }) => {
  const { currentUser, activeOrgId, orgs } = useAppStore();
  const activeOrg = orgs.find((o) => o.id === activeOrgId);
  const [providers, setProviders] = useState<ProviderSetting[]>([]);

  // Email-notification opt-out (issue #187). Loaded from the server so the
  // toggle reflects the stored preference, not just the initial /me payload.
  const [emailNotifications, setEmailNotifications] = useState<boolean>(true);
  const [emailPrefSaving, setEmailPrefSaving] = useState(false);

  useEffect(() => {
    let cancelled = false;
    notificationPrefsAPI
      .get()
      .then((res) => {
        if (!cancelled) setEmailNotifications(res.data.email_notifications);
      })
      .catch(() => {
        // Non-fatal: leave the default (on) if the prefs endpoint is unreachable.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const toggleEmailNotifications = useCallback(async () => {
    const next = !emailNotifications;
    setEmailNotifications(next); // optimistic
    setEmailPrefSaving(true);
    try {
      await notificationPrefsAPI.update(next);
    } catch {
      setEmailNotifications(!next); // revert on failure
    } finally {
      setEmailPrefSaving(false);
    }
  }, [emailNotifications]);

  // Load provider settings so each per-user card reflects the real detected
  // sign-in state (mirrors OrgProvidersTab): a connected CLI shows "Re-connect"
  // instead of a first-time "Connect".
  const loadProviders = useCallback(async () => {
    if (!activeOrgId) return;
    try {
      const res = await providerSettingsAPI.list();
      setProviders(res.data || []);
    } catch {
      // Non-fatal: fall back to showing "Connect" for every provider.
      setProviders([]);
    }
  }, [activeOrgId]);

  useEffect(() => {
    loadProviders();
  }, [loadProviders]);

  const loggedInFor = (provider: string): boolean => {
    const p = providers.find((s) => s.provider === provider);
    return Boolean((p?.last_detected || {})['logged_in']);
  };

  return (
    <div
      onClick={onClose}
      style={{
        position: 'fixed',
        inset: 0,
        background: 'var(--overlay)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 2000,
      }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          width: 720,
          maxWidth: '94vw',
          maxHeight: '88vh',
          overflowY: 'auto',
          background: 'var(--bg-app)',
          borderRadius: 8,
          padding: 24,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 16 }}>
          {currentUser?.avatar_url ? (
            <img src={currentUser.avatar_url} alt="" style={{ width: 40, height: 40, borderRadius: '50%' }} />
          ) : (
            <div
              style={{
                width: 40,
                height: 40,
                borderRadius: '50%',
                background: 'var(--accent)',
                color: 'var(--accent-fg)',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                fontSize: 17,
                fontWeight: 700,
              }}
            >
              {(currentUser?.name || currentUser?.email || '?').charAt(0).toUpperCase()}
            </div>
          )}
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontSize: 16, fontWeight: 700, color: 'var(--text)' }}>
              {currentUser?.name || currentUser?.email || 'My settings'}
            </div>
            <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
              Personal settings{activeOrg ? ` · ${activeOrg.name}` : ''}
            </div>
          </div>
          <button
            onClick={onClose}
            style={{ background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 18, width: 'auto' }}
            title="Close"
          >
            ✕
          </button>
        </div>

        <div className="card">
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
            <div>
              <h3 style={{ marginBottom: 4 }}>Appearance</h3>
              <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 0 }}>
                Theme for this browser. “System” follows your OS setting.
              </p>
            </div>
            <ThemeSwitcher />
          </div>
        </div>

        <div className="card">
          <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 12 }}>
            <div style={{ flex: 1, minWidth: 0 }}>
              <h3 style={{ marginBottom: 4 }}>Notifications</h3>
              <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 0 }}>
                Email me about high-signal events (failed runs, proposals awaiting review, review
                requests, and workspace budget alerts). In-app notifications are always on.
                Email requires the server to have SMTP configured.
              </p>
            </div>
            <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer', whiteSpace: 'nowrap' }}>
              <input
                type="checkbox"
                checked={emailNotifications}
                disabled={emailPrefSaving}
                onChange={toggleEmailNotifications}
                style={{ width: 'auto' }}
              />
              <span style={{ fontSize: 13, color: 'var(--text)' }}>Email me</span>
            </label>
          </div>
        </div>

        {!activeOrgId ? (
          <div className="card">
            <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 0 }}>
              Select a workspace to manage your runner and agent sign-ins.
            </p>
          </div>
        ) : (
          <>
            <MyRunnerCard orgId={activeOrgId} />

            <CloudRunnerCard orgId={activeOrgId} onChanged={loadProviders} />

            <div className="card">
              <h3 style={{ marginBottom: 6 }}>Agent sign-ins</h3>
              <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
                Sign the vendor CLIs into your own subscriptions. The flow runs on{' '}
                <b>whichever runner of yours is online</b> — your own machine via the Agent
                Connector, or the cloud runner above — and the credentials stay there. A cloud
                runner is wiped when its lease ends, so those sign-ins are per-session; sign-ins on
                your own machine persist. Projects set to “API key” auth override these sign-ins
                for their runs.
              </p>
              {CLI_PROVIDERS.map((p) => (
                <ProviderConnectCard
                  key={p.key}
                  provider={p.key}
                  loggedIn={loggedInFor(p.key)}
                  target="user"
                  title={p.label}
                  onComplete={loadProviders}
                />
              ))}
            </div>

            <div className="card" style={{ background: 'var(--tint-blue)', border: '1px solid var(--accent)' }}>
              <p style={{ fontSize: 13, color: 'var(--text)', marginBottom: 0 }}>
                Looking for repository locations? Your per-project local paths are set on each
                project's Settings → Repositories tab (“your local path”), since they differ per
                project.
              </p>
            </div>
          </>
        )}
      </div>
    </div>
  );
};
