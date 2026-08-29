import React, { useCallback, useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { AgentDef, agentsAPI } from '../api/client';
import { useAppStore } from '../state/store';
import { AgentEditor } from '../components/agents/AgentEditor';
import { ErrorBanner, Modal, useConfirm } from '../components/ui';

const providerBadgeColor = (provider: string): string => {
  switch (provider) {
    case 'claude-code':
    case 'anthropic-api':
      return '#d97757';
    case 'codex-cli':
    case 'openai-api':
      return '#10a37f';
    case 'gemini-cli':
    case 'google-api':
      return '#4285f4';
    default:
      return 'var(--neutral)';
  }
};

export const AgentsPage: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;
  const activeOrgId = useAppStore((s) => s.activeOrgId);
  const navigate = useNavigate();
  const confirm = useConfirm();

  const [agents, setAgents] = useState<AgentDef[]>([]);
  const [selectedSlug, setSelectedSlug] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState('');
  const [syncing, setSyncing] = useState(false);

  // Launch modal
  const [launchAgent, setLaunchAgent] = useState<AgentDef | null>(null);
  const [launchPrompt, setLaunchPrompt] = useState('');
  const [launching, setLaunching] = useState(false);

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

  const handleLaunch = async () => {
    if (!launchAgent || !launchPrompt.trim()) return;
    setLaunching(true);
    try {
      await agentsAPI.launchRun(launchAgent.slug, {
        project_id: projectId || undefined,
        prompt: launchPrompt.trim(),
      });
      setLaunchAgent(null);
      setLaunchPrompt('');
      navigate(`/projects/${projectId}/agent-runs`);
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to launch run');
    } finally {
      setLaunching(false);
    }
  };

  return (
    <div style={{ padding: 20, height: '100%', display: 'flex', flexDirection: 'column' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 14, flexWrap: 'wrap' }}>
        <h2 style={{ color: 'var(--text)', margin: 0 }}>Agents</h2>
        <div style={{ flex: 1 }} />
        <button
          className="button-secondary"
          onClick={handleSync}
          disabled={syncing}
          title="Re-read agent definition files from disk"
        >
          {syncing ? 'Syncing…' : 'Sync from disk'}
        </button>
        <button
          className="button"
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
        <div style={{ width: 320, flexShrink: 0, overflowY: 'auto' }}>
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
                {agent.repo_access && (
                  <span title="Has repository access" style={{ fontSize: 13 }}>
                    📁
                  </span>
                )}
              </div>
              <div style={{ display: 'flex', gap: 6, marginTop: 8, flexWrap: 'wrap' }}>
                <span
                  style={{
                    fontSize: 11,
                    background: providerBadgeColor(agent.provider),
                    color: '#fff',
                    borderRadius: 10,
                    padding: '2px 8px',
                    fontWeight: 600,
                  }}
                >
                  {agent.provider}
                </span>
                <span
                  style={{
                    fontSize: 11,
                    background: agent.write_mode === 'direct' ? 'var(--danger)' : 'var(--warning)',
                    color: '#fff',
                    borderRadius: 10,
                    padding: '2px 8px',
                    fontWeight: 600,
                  }}
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
                  style={{ padding: '4px 10px', fontSize: 12 }}
                  onClick={(e) => {
                    e.stopPropagation();
                    setLaunchAgent(agent);
                    setLaunchPrompt('');
                  }}
                >
                  ▶ Launch run
                </button>
                <button
                  style={{
                    padding: '4px 10px',
                    fontSize: 12,
                    background: 'var(--danger)',
                    color: '#fff',
                    border: 'none',
                    borderRadius: 4,
                    cursor: 'pointer',
                  }}
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
        <div style={{ flex: 1, overflowY: 'auto', minWidth: 0 }}>
          {creating || selected ? (
            <AgentEditor
              agent={creating ? null : selected}
              onSaved={() => {
                setCreating(false);
                load();
              }}
              onCancel={() => {
                setCreating(false);
                setSelectedSlug(null);
              }}
            />
          ) : (
            <div style={{ color: 'var(--text-muted)', padding: 40, textAlign: 'center' }}>
              Select an agent to edit it, or create a new one.
            </div>
          )}
        </div>
      </div>

      {/* Launch modal */}
      {launchAgent && (
        <Modal
          title={`Launch run: ${launchAgent.name}`}
          width={520}
          onClose={() => setLaunchAgent(null)}
        >
            <div className="form-group">
              <label>Prompt</label>
              <textarea
                value={launchPrompt}
                onChange={(e) => setLaunchPrompt(e.target.value)}
                placeholder="What should this agent do?"
                style={{ minHeight: 110, fontSize: 13 }}
                autoFocus
              />
              <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 6 }}>
                Keep prompts lean — the agent fetches requirements itself via its OpenV tools.
              </div>
            </div>
            <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
              <button className="button-secondary" onClick={() => setLaunchAgent(null)}>
                Cancel
              </button>
              <button
                className="button"
                onClick={handleLaunch}
                disabled={launching || !launchPrompt.trim()}
              >
                {launching ? 'Launching…' : 'Launch'}
              </button>
            </div>
        </Modal>
      )}
    </div>
  );
};
