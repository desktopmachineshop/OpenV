import React from 'react';
import { Project } from './shared';

// The Agents tab of project settings: how the project's agent runs
// authenticate with their AI provider.
// Props-only (refactor plan F6): the ProjectSettings shell owns the state, the
// loads and the handlers, so a tab switch keeps unsaved input.
interface AgentsTabProps {
  project: Project | null;
  savingAuth: boolean;
  handleSetAgentAuth: (mode: 'user-account' | 'api-key') => void;
}

export const AgentsTab: React.FC<AgentsTabProps> = ({ project, savingAuth, handleSetAgentAuth }) => (
  <div className="card">
    <h3>Agent authentication</h3>
    <p style={{ fontSize: 13, color: 'var(--text-muted)' }}>
      How agent runs in this project authenticate with their AI provider. This only picks the
      credential and routing — sign-ins themselves live in each member's user settings (the
      user menu, bottom left).
    </p>
    {!project ? (
      <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading project…</div>
    ) : (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        <label
          style={{
            display: 'flex',
            gap: 10,
            alignItems: 'flex-start',
            border: '1px solid',
            borderColor: project.agent_auth !== 'api-key' ? 'var(--accent)' : 'var(--border-soft)',
            borderRadius: 4,
            padding: '10px 12px',
            cursor: 'pointer',
            marginBottom: 0,
            fontWeight: 400,
          }}
        >
          <input
            type="radio"
            name="agent-auth"
            style={{ width: 'auto', marginTop: 3 }}
            checked={project.agent_auth !== 'api-key'}
            disabled={savingAuth}
            onChange={() => handleSetAgentAuth('user-account')}
          />
          <span style={{ fontSize: 13, color: 'var(--text)' }}>
            <b>User account</b>
            <br />
            <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>
              Runs use each member's own CLI sign-in on their machine (set up in user
              settings). No sign-in happens here — this just routes runs to the launcher's
              local login.
            </span>
          </span>
        </label>
        <label
          style={{
            display: 'flex',
            gap: 10,
            alignItems: 'flex-start',
            border: '1px solid',
            borderColor: project.agent_auth === 'api-key' ? 'var(--accent)' : 'var(--border-soft)',
            borderRadius: 4,
            padding: '10px 12px',
            cursor: 'pointer',
            marginBottom: 0,
            fontWeight: 400,
          }}
        >
          <input
            type="radio"
            name="agent-auth"
            style={{ width: 'auto', marginTop: 3 }}
            checked={project.agent_auth === 'api-key'}
            disabled={savingAuth}
            onChange={() => handleSetAgentAuth('api-key')}
          />
          <span style={{ fontSize: 13, color: 'var(--text)' }}>
            <b>API key</b>
            <br />
            <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>
              Overrides members' local sign-ins: runs use the workspace's API key (the
              provider's key environment variable, configured in Workspace settings → AI
              providers, set on the runner host). For now runs still execute through each
              member's OpenV connector.
            </span>
          </span>
        </label>
      </div>
    )}
  </div>
);
