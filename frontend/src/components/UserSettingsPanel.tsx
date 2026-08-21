import React from 'react';
import { useAppStore } from '../state/store';
import { MyRunnerCard } from './org/MyRunnerCard';
import { ProviderConnectCard } from './agents/ProviderConnectCard';

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

  return (
    <div
      onClick={onClose}
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(44, 62, 80, 0.55)',
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
          background: '#f5f6fa',
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
                background: '#3498db',
                color: '#fff',
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
            <div style={{ fontSize: 16, fontWeight: 700, color: '#2c3e50' }}>
              {currentUser?.name || currentUser?.email || 'My settings'}
            </div>
            <div style={{ fontSize: 12, color: '#7f8c8d' }}>
              Personal settings{activeOrg ? ` · ${activeOrg.name}` : ''}
            </div>
          </div>
          <button
            onClick={onClose}
            style={{ background: 'none', border: 'none', color: '#7f8c8d', cursor: 'pointer', fontSize: 18, width: 'auto' }}
            title="Close"
          >
            ✕
          </button>
        </div>

        {!activeOrgId ? (
          <div className="card">
            <p style={{ fontSize: 13, color: '#7f8c8d', marginBottom: 0 }}>
              Select a workspace to manage your runner and agent sign-ins.
            </p>
          </div>
        ) : (
          <>
            <MyRunnerCard orgId={activeOrgId} />

            <div className="card">
              <h3 style={{ marginBottom: 6 }}>Agent sign-ins</h3>
              <p style={{ fontSize: 13, color: '#7f8c8d' }}>
                Sign the vendor CLIs on <b>your machine</b> into your own subscriptions. The flow
                runs through your personal runner (Agent Connector), and credentials never leave
                your machine. Projects set to “API key” auth override these sign-ins for their runs.
              </p>
              {CLI_PROVIDERS.map((p) => (
                <ProviderConnectCard
                  key={p.key}
                  provider={p.key}
                  loggedIn={false}
                  target="user"
                  title={p.label}
                  onComplete={() => {}}
                />
              ))}
            </div>

            <div className="card" style={{ background: '#eaf4fc', border: '1px solid #3498db' }}>
              <p style={{ fontSize: 13, color: '#2c3e50', marginBottom: 0 }}>
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
