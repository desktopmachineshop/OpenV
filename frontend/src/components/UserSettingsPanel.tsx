import { useViewport } from '../hooks/useViewport';
import React, { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { useAppStore } from '../state/store';
import { ProviderSetting, providerSettingsAPI, notificationPrefsAPI, pushAPI } from '../api/client';
import {
  PushUnavailableReason,
  currentPermission,
  getExistingSubscription,
  subscribeThisDevice,
  supportsPush,
  unsubscribeThisDevice,
} from '../push/webPush';
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
  const { isPhone } = useViewport();
  const { currentUser, activeOrgId, orgs, emailVerificationRequired } = useAppStore();
  const activeOrg = orgs.find((o) => o.id === activeOrgId);
  const [providers, setProviders] = useState<ProviderSetting[]>([]);

  // Email-notification opt-out (issue #187). Loaded from the server so the
  // toggle reflects the stored preference, not just the initial /me payload.
  const [emailNotifications, setEmailNotifications] = useState<boolean>(true);
  const [emailPrefSaving, setEmailPrefSaving] = useState(false);

  // Web push on THIS device (REQ-109). Three things have to line up: the
  // server has VAPID keys, the browser has the APIs, and the member has both
  // granted permission and subscribed here. pushOn is true only when all
  // three hold; pushUnavailable says which one does not.
  const [pushOn, setPushOn] = useState(false);
  const [pushKey, setPushKey] = useState('');
  const [pushUnavailable, setPushUnavailable] = useState<PushUnavailableReason>('');
  const [pushSaving, setPushSaving] = useState(false);
  const [pushError, setPushError] = useState('');

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

  // Reflect the real state of this device: the server's configuration, the
  // browser's permission, and whether a subscription already exists here.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      if (!supportsPush()) {
        if (!cancelled) setPushUnavailable('unsupported');
        return;
      }
      try {
        const res = await pushAPI.config();
        if (cancelled) return;
        if (!res.data.enabled || !res.data.public_key) {
          setPushUnavailable('not-configured');
          return;
        }
        setPushKey(res.data.public_key);
        if (currentPermission() === 'denied') {
          setPushUnavailable('denied');
          return;
        }
        setPushUnavailable('');
        const existing = await getExistingSubscription();
        if (!cancelled) setPushOn(Boolean(existing) && currentPermission() === 'granted');
      } catch {
        // The config endpoint is the only signal that push exists at all; if
        // it cannot be read, present push as unavailable rather than offering
        // a toggle that cannot work.
        if (!cancelled) setPushUnavailable('not-configured');
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const toggleEmailNotifications = useCallback(async () => {
    const next = !emailNotifications;
    setEmailNotifications(next); // optimistic
    setEmailPrefSaving(true);
    try {
      await notificationPrefsAPI.update({ email_notifications: next });
    } catch {
      setEmailNotifications(!next); // revert on failure
    } finally {
      setEmailPrefSaving(false);
    }
  }, [emailNotifications]);

  // Turning push on is a four-step handshake (permission, subscribe, register
  // the device, set the per-user opt-in); turning it off withdraws the device
  // but leaves the opt-in alone, since the member may still want push on
  // their other devices.
  const togglePushNotifications = useCallback(async () => {
    if (pushUnavailable) return;
    setPushSaving(true);
    setPushError('');
    try {
      if (pushOn) {
        await unsubscribeThisDevice();
        setPushOn(false);
      } else {
        await subscribeThisDevice(pushKey);
        await notificationPrefsAPI.update({ push_notifications: true });
        setPushOn(true);
      }
    } catch (err) {
      setPushError(err instanceof Error ? err.message : 'Could not change push notifications.');
      if (currentPermission() === 'denied') setPushUnavailable('denied');
    } finally {
      setPushSaving(false);
    }
  }, [pushOn, pushKey, pushUnavailable]);

  const pushUnavailableText = (reason: PushUnavailableReason): string => {
    switch (reason) {
      case 'unsupported':
        return 'This browser cannot receive push notifications. On an iPhone or iPad, install OpenV to the Home Screen first.';
      case 'not-configured':
        return 'Push notifications are not configured on this server.';
      case 'denied':
        return 'Notifications are blocked for this site. Allow them in your browser settings, then try again.';
      default:
        return '';
    }
  };

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
        className={isPhone ? 'safe-area-top safe-area-bottom' : undefined}
        style={
          isPhone
            ? {
                // A full-screen sheet on a phone: the settings are a page of
                // cards, and a page deserves the whole screen.
                width: '100vw',
                height: '100vh',
                maxHeight: '100dvh',
                overflowY: 'auto',
                background: 'var(--bg-app)',
                padding: 16,
                boxSizing: 'border-box',
              }
            : {
                width: 720,
                maxWidth: '94vw',
                maxHeight: '88vh',
                overflowY: 'auto',
                background: 'var(--bg-app)',
                borderRadius: 8,
                padding: 24,
              }
        }
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
            {emailVerificationRequired && currentUser && (
              <div style={{ fontSize: 12, marginTop: 4, display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
                <span style={{ color: 'var(--text-muted)', overflowWrap: 'anywhere' }}>{currentUser.email}</span>
                {currentUser.email_verified ? (
                  <span
                    style={{
                      padding: '1px 8px',
                      borderRadius: 10,
                      fontSize: 12,
                      fontWeight: 600,
                      background: 'var(--tint-green)',
                      color: 'var(--success)',
                    }}
                  >
                    Verified
                  </span>
                ) : (
                  <Link to="/verify-email" style={{ color: 'var(--accent)', fontSize: 12 }}>
                    Unverified — verify now
                  </Link>
                )}
              </div>
            )}
          </div>
          <button
            onClick={onClose}
            aria-label="Close settings"
            style={{ background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: 20, width: 40, minHeight: 40, padding: 0, flexShrink: 0 }}
            title="Close"
          >
            ✕
          </button>
        </div>

        <div className="card">
          {/* The text and the control share a row that wraps: on a phone the
              theme switcher drops under the description instead of being
              squeezed until its last option is cut off. */}
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap' }}>
            <div style={{ flex: '1 1 220px', minWidth: 0 }}>
              <h3 style={{ marginBottom: 4 }}>Appearance</h3>
              <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 0 }}>
                Theme for this browser. “System” follows your OS setting.
              </p>
            </div>
            <ThemeSwitcher />
          </div>
        </div>

        <div className="card">
          <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap' }}>
            <div style={{ flex: '1 1 220px', minWidth: 0 }}>
              <h3 style={{ marginBottom: 4 }}>Notifications</h3>
              <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 0 }}>
                Email me about high-signal events (failed runs, proposals awaiting review, review
                requests, and workspace budget alerts). In-app notifications are always on.
                Email requires the server to have SMTP configured.
              </p>
            </div>
            <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer', whiteSpace: 'nowrap', minHeight: 40, flexShrink: 0 }}>
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

          {/* Push is per DEVICE, not per account: the switch below applies to
              the browser it is tapped in, and each phone or tablet is opted in
              on its own. */}
          <div
            style={{
              display: 'flex',
              alignItems: 'flex-start',
              justifyContent: 'space-between',
              gap: 12,
              flexWrap: 'wrap',
              marginTop: 16,
              paddingTop: 16,
              borderTop: '1px solid var(--border)',
            }}
          >
            <div style={{ flex: '1 1 220px', minWidth: 0 }}>
              <h4 style={{ margin: '0 0 4px', fontSize: 14, color: 'var(--text)' }}>
                Push notifications on this device
              </h4>
              <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 0 }}>
                Send the same high-signal events to this browser, even when OpenV is closed.
                Each device is turned on separately.
              </p>
              {pushUnavailable && (
                <p style={{ fontSize: 12, color: 'var(--text-muted)', margin: '6px 0 0' }}>
                  {pushUnavailableText(pushUnavailable)}
                </p>
              )}
              {pushError && !pushUnavailable && (
                <p style={{ fontSize: 12, color: 'var(--danger)', margin: '6px 0 0' }}>{pushError}</p>
              )}
            </div>
            <label
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 8,
                cursor: pushUnavailable ? 'not-allowed' : 'pointer',
                whiteSpace: 'nowrap',
                minHeight: 40,
                flexShrink: 0,
                opacity: pushUnavailable ? 0.6 : 1,
              }}
            >
              <input
                type="checkbox"
                checked={pushOn}
                disabled={pushSaving || Boolean(pushUnavailable)}
                onChange={togglePushNotifications}
                aria-label="Push notifications on this device"
                style={{ width: 'auto' }}
              />
              <span style={{ fontSize: 13, color: 'var(--text)' }}>
                {pushSaving ? 'Working…' : 'Push here'}
              </span>
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
