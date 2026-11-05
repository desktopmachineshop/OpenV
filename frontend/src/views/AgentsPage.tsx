import React, { useCallback, useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { AgentDef, agentsAPI } from '../api/client';
import { useAppStore } from '../state/store';
import { useViewport } from '../hooks/useViewport';
import { useFeature } from '../hooks/useFeature';
import { AGENT_HIERARCHY_FEATURE } from '../features';
import { AgentEditor } from '../components/agents/AgentEditor';
import { LaunchRunModal } from '../components/agents/LaunchRunModal';
import { ErrorBanner, useConfirm, Sheet, chipStyle, buttonStyle } from '../components/ui';
import { AgentsHierarchyPage } from './AgentsHierarchyPage';

const providerBadgeColor = (provider: string): string => {
  switch (provider) {
    case 'claude-code':
    case 'anthropic-api':
      return '#d97757';
    case 'codex-cli':
    case 'openai-api':
      return '#10a37f';
    case 'gemini-cli':
    case 'antigravity-cli':
    case 'google-api':
      return '#4285f4';
    default:
      return 'var(--neutral)';
  }
};

// The Agents page: with the agent-hierarchy feature, the hierarchy of cards
// (AgentsHierarchyPage); without it, the list beside the editor.
export const AgentsPage: React.FC = () =>
  useFeature(AGENT_HIERARCHY_FEATURE) ? <AgentsHierarchyPage /> : <AgentsList />;

export const AgentsList: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;
  // Compact viewports have no room for a list beside an editor: the list
  // takes the width and the editor opens as a full-screen sheet.
  const { isCompact } = useViewport();
  const activeOrgId = useAppStore((s) => s.activeOrgId);
  const confirm = useConfirm();

  const [agents, setAgents] = useState<AgentDef[]>([]);
  const [selectedSlug, setSelectedSlug] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState('');
  const [syncing, setSyncing] = useState(false);

  const [launchAgent, setLaunchAgent] = useState<AgentDef | null>(null);

  const load = useCallback(() => {
    agentsAPI
      .list()
      .then((res) => setAgents(res.data || []))
      .catch((err: any) =>
        setError(err.response?.data?.error || err.message || 'Failed to load agents')
      );
    // The list is scoped by the X-Org-ID header the API client injects, so
    // refetch when the active workspace changes (e.g. ProjectLayout's
    // cross-org deep-link sync, issue #99).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeOrgId]);

  useEffect(() => {
    load();
  }, [load]);

  const selected = agents.find((a) => a.slug === selectedSlug) || null;

  const handleSync = async () => {
    setSyncing(true);
    setError('');
    try {
      const res = await agentsAPI.sync();
      setAgents(res.data || []);
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Sync failed');
    } finally {
      setSyncing(false);
    }
  };

  const handleDelete = async (agent: AgentDef) => {
    const ok = await confirm({
      title: 'Delete agent',
      message: `Delete agent "${agent.name}"? This removes its definition file.`,
      confirmLabel: 'Delete',
      danger: true,
    });
    if (!ok) return;
    try {
      await agentsAPI.remove(agent.slug);
      if (selectedSlug === agent.slug) setSelectedSlug(null);
      load();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to delete agent');
    }
  };

  const closeEditor = () => {
    setCreating(false);
    setSelectedSlug(null);
  };
  const onEditorSaved = () => {
    setCreating(false);
    load();
  };

  return (
    <div style={{ padding: 20, height: '100%', display: 'flex', flexDirection: 'column' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 14, flexWrap: 'wrap' }}>
        <h2 style={{ color: 'var(--text)', margin: 0, fontSize: 20, fontWeight: 700 }}>Agents</h2>
        <div style={{ flex: 1 }} />
        <button
          className="button-secondary"
          data-help-key="agents.list"
          onClick={handleSync}
          disabled={syncing}
          title="Re-read agent definition files from disk"
        >
          {syncing ? 'Syncing…' : 'Sync from disk'}
        </button>
        <button
          className="button"
          data-help-key="agents.editor"
          onClick={() => {
            setCreating(true);
            setSelectedSlug(null);
          }}
        >
          New agent
        </button>
      </div>

      <ErrorBanner message={error} onDismiss={() => setError('')} />

      <div style={{ display: 'flex', gap: 16, flex: 1, minHeight: 0 }}>
        {/* Agent list */}
        <div data-help-key="agents.list" style={isCompact ? { flex: 1, minWidth: 0, overflowY: 'auto' } : { width: 320, flexShrink: 0, overflowY: 'auto' }}>
          {agents.length === 0 && (
            <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>
              No agents defined yet. Create one or sync from disk.
            </div>
          )}
          {agents.map((agent) => (
            <div
              key={agent.slug}
              onClick={() => {
                setSelectedSlug(agent.slug);
                setCreating(false);
              }}
              style={{
                background: 'var(--surface)',
                border:
                  selectedSlug === agent.slug ? '2px solid var(--accent)' : '1px solid var(--border)',
                borderRadius: 4,
                padding: '12px 14px',
                marginBottom: 10,
                cursor: 'pointer',
                boxShadow: '0 1px 3px rgba(0,0,0,0.06)',
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <strong style={{ flex: 1, fontSize: 14, color: 'var(--text)' }}>
                  🤖 {agent.name}
                </strong>
                {agent.locked && (
                  <span title="Locked: platform updates never change this agent" style={{ fontSize: 13 }}>
                    🔒
                  </span>
                )}
                {agent.repo_access && (
                  <span title="Has repository access" style={{ fontSize: 13 }}>
                    📁
                  </span>
                )}
              </div>
              <div style={{ display: 'flex', gap: 6, marginTop: 8, flexWrap: 'wrap' }}>
                <span
                  style={{
                    ...chipStyle('neutral'),
                    borderLeft: `3px solid ${providerBadgeColor(agent.provider)}`,
                  }}
                >
                  {agent.provider}
                </span>
                <span
                  style={chipStyle(agent.write_mode === 'direct' ? 'danger' : 'warning')}
                  title={
                    agent.write_mode === 'direct'
                      ? 'Writes changes directly'
                      : 'Changes go through proposal review'
                  }
                >
                  {agent.write_mode}
                </span>
              </div>
              {agent.description && (
                <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 6 }}>
                  {agent.description}
                </div>
              )}
              <div style={{ display: 'flex', gap: 6, marginTop: 10 }}>
                <button
                  className="button"
                  data-help-key="agents.launch"
                  style={{ padding: '4px 10px', fontSize: 12, minHeight: 36 }}
                  onClick={(e) => {
                    e.stopPropagation();
                    setLaunchAgent(agent);
                  }}
                >
                  ▶ Launch run
                </button>
                <button
                  style={{ ...buttonStyle('danger', { compact: true }), minHeight: 36 }}
                  onClick={(e) => {
                    e.stopPropagation();
                    handleDelete(agent);
                  }}
                >
                  Delete
                </button>
              </div>
            </div>
          ))}
        </div>

        {/* Editor */}
        {isCompact ? (
          (creating || selected) && (
            <Sheet label={creating ? 'New agent' : 'Edit agent'} onClose={closeEditor}>
              <AgentEditor agent={creating ? null : selected} onSaved={onEditorSaved} onCancel={closeEditor} />
            </Sheet>
          )
        ) : (
          <div style={{ flex: 1, overflowY: 'auto', minWidth: 0 }}>
            {creating || selected ? (
              <AgentEditor agent={creating ? null : selected} onSaved={onEditorSaved} onCancel={closeEditor} />
            ) : (
              <div style={{ color: 'var(--text-muted)', padding: 40, textAlign: 'center' }}>
                Select an agent to edit it, or create a new one.
              </div>
            )}
          </div>
        )}
      </div>

      {launchAgent && (
        <LaunchRunModal agent={launchAgent} projectId={projectId} onClose={() => setLaunchAgent(null)} onError={setError} />
      )}
    </div>
  );
};
