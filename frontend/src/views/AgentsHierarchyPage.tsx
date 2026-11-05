import React, { useCallback, useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { AgentDef, AgentPlacement, AgentStats, agentsAPI } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';
import { useViewport } from '../hooks/useViewport';
import { AgentEditor } from '../components/agents/AgentEditor';
import { AgentHierarchy } from '../components/agents/AgentHierarchy';
import { LaunchRunModal } from '../components/agents/LaunchRunModal';
import { ErrorBanner, Modal, Sheet } from '../components/ui';

// The Agents page as a hierarchy of cards (issue 733, the agent-hierarchy
// feature): every agent with its numbers over the last 7 days, read in one
// call (GET /agent-stats) and refreshed while the page is open. A card opens
// the agent's runs and proposals; Edit arranges who reports to whom.

const STATS_REFRESH_MS = 30_000;

export const AgentsHierarchyPage: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;
  const activeOrgId = useAppStore((s) => s.activeOrgId);
  const isAdmin = useAppStore((s) =>
    Boolean(s.currentUser?.is_admin || s.orgs.find((o) => o.id === s.activeOrgId)?.role === 'admin')
  );
  const { isCompact } = useViewport();
  const navigate = useNavigate();

  const [agents, setAgents] = useState<AgentDef[]>([]);
  const [stats, setStats] = useState<Record<string, AgentStats>>({});
  const [error, setError] = useState('');
  const [editing, setEditing] = useState(false);
  const [editor, setEditor] = useState<{ agent: AgentDef | null } | null>(null);
  const [launchAgent, setLaunchAgent] = useState<AgentDef | null>(null);

  const load = useCallback(() => {
    agentsAPI
      .list()
      .then((res) => setAgents(res.data || []))
      .catch((err) => setError(apiErrorMessage(err, 'Failed to load agents')));
    // The list is scoped by the X-Org-ID header, so refetch on a workspace
    // switch (issue #99).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeOrgId]);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    if (!projectId) return;
    let live = true;
    const read = () =>
      agentsAPI
        .stats(projectId)
        .then((res) => {
          if (!live) return;
          const byAgent: Record<string, AgentStats> = {};
          (res.data?.agents || []).forEach((s) => {
            byAgent[s.agent_id] = s;
          });
          setStats(byAgent);
        })
        .catch(() => {
          // The numbers are best-effort: the cards still show without them.
        });
    read();
    const timer = window.setInterval(read, STATS_REFRESH_MS);
    return () => {
      live = false;
      window.clearInterval(timer);
    };
  }, [projectId, activeOrgId]);

  const arrange = async (placements: AgentPlacement[]) => {
    try {
      const res = await agentsAPI.arrange(placements);
      setAgents(res.data || []);
    } catch (err) {
      setError(apiErrorMessage(err, 'Failed to arrange the agents'));
    }
  };

  const openRuns = (agent: AgentDef) =>
    navigate(`/projects/${projectId}/agent-runs?agent=${encodeURIComponent(agent.id)}`);

  const closeEditor = () => setEditor(null);
  const onSaved = () => {
    setEditor(null);
    load();
  };
  const editorBody = editor && (
    <AgentEditor agent={editor.agent} onSaved={onSaved} onCancel={closeEditor} />
  );

  return (
    <div style={{ padding: 20, height: '100%', overflowY: 'auto' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 4, flexWrap: 'wrap' }}>
        <h2 style={{ color: 'var(--text)', margin: 0, fontSize: 20, fontWeight: 700 }}>Agents</h2>
        <div style={{ flex: 1 }} />
        {isAdmin && (
          <button
            className={editing ? 'button' : 'button-secondary'}
            aria-pressed={editing}
            data-help-key="agents.hierarchy"
            onClick={() => setEditing((e) => !e)}
          >
            {editing ? 'Done' : 'Edit'}
          </button>
        )}
        <button className="button" data-help-key="agents.editor" onClick={() => setEditor({ agent: null })}>
          New agent
        </button>
      </div>
      <p style={{ margin: '0 0 14px', fontSize: 13, color: 'var(--text-muted)' }}>
        {editing
          ? 'Arrange the agents: choose who each reports to and its group, and move it up or down among its peers.'
          : 'Live numbers over the last 7 days. Choose an agent to see its runs and proposals.'}
      </p>

      <ErrorBanner message={error} onDismiss={() => setError('')} />

      {agents.length === 0 ? (
        <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>No agents defined yet. Create one.</div>
      ) : (
        <AgentHierarchy
          agents={agents}
          stats={stats}
          editing={editing}
          onArrange={arrange}
          onOpen={openRuns}
          onEdit={(agent) => setEditor({ agent })}
          onLaunch={setLaunchAgent}
        />
      )}

      {editor &&
        (isCompact ? (
          <Sheet label={editor.agent ? 'Edit agent' : 'New agent'} onClose={closeEditor}>
            {editorBody}
          </Sheet>
        ) : (
          <Modal title={editor.agent ? `Edit ${editor.agent.name}` : 'New agent'} width={820} onClose={closeEditor}>
            {editorBody}
          </Modal>
        ))}
      {launchAgent && (
        <LaunchRunModal agent={launchAgent} projectId={projectId} onClose={() => setLaunchAgent(null)} onError={setError} />
      )}
    </div>
  );
};
