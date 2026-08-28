import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useLocation, useNavigate, useParams } from 'react-router-dom';
import {
  AgentDef,
  agentRunsAPI,
  agentsAPI,
  Crew,
  CrewGraph,
  crewsAPI,
  OrgMember,
  orgsAPI,
} from '../api/client';
import { useAppStore } from '../state/store';
import { CrewCanvas, CrewFilter } from '../components/crews/CrewCanvas';
import { NodeConfigPanel } from '../components/crews/NodeConfigPanel';
import { EdgeConfigPanel } from '../components/crews/EdgeConfigPanel';

const LIVE_STATUSES = ['queued', 'claimed', 'running'];
const EDGE_TYPES: { value: string; label: string; color: string }[] = [
  { value: 'delegates-to', label: 'Delegates to', color: 'var(--accent)' },
  { value: 'hands-off-to', label: 'Hands off to', color: 'var(--success)' },
  { value: 'reviews', label: 'Reviews', color: 'var(--warning)' },
];
// Human targets can't be delegated to — hand-offs and reviews instead create
// a Board card assigned to that person.
const HUMAN_TARGET_EDGE_TYPES: { value: string; label: string; color: string }[] = [
  {
    value: 'hands-off-to',
    label: 'Hands off to (creates a Board card for this person)',
    color: 'var(--success)',
  },
  {
    value: 'reviews',
    label: 'Reviews (creates a Board card for this person)',
    color: 'var(--warning)',
  },
];

const FILTERS: { value: CrewFilter; label: string }[] = [
  { value: 'employees', label: 'Employees' },
  { value: 'agents', label: 'Agents' },
  { value: 'all', label: 'All' },
];

export const CrewBuilder: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;
  const activeOrgId = useAppStore((s) => s.activeOrgId);
  const navigate = useNavigate();
  const location = useLocation();

  // The view is derived from the route (/crew ↔ org chart, /crew/network ↔
  // network) so deep links and refreshes land on the right view.
  const view: 'org' | 'network' = location.pathname.replace(/\/+$/, '').endsWith('/network')
    ? 'network'
    : 'org';
  const setView = useCallback(
    (v: 'org' | 'network') => {
      if (!projectId) return;
      navigate(`/projects/${projectId}/crew${v === 'network' ? '/network' : ''}`);
    },
    [navigate, projectId]
  );

  const [crews, setCrews] = useState<Crew[]>([]);
  const [crewId, setCrewId] = useState<string>('');
  const [graph, setGraph] = useState<CrewGraph | null>(null);
  const [agents, setAgents] = useState<AgentDef[]>([]);
  const [members, setMembers] = useState<OrgMember[]>([]);
  const [filter, setFilter] = useState<CrewFilter>('all');
  const [connectMode, setConnectMode] = useState(false);
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [selectedEdgeId, setSelectedEdgeId] = useState<string | null>(null);
  const [pendingEdge, setPendingEdge] = useState<{ from: string; to: string } | null>(null);
  const [activeNodeIds, setActiveNodeIds] = useState<string[]>([]);
  const [error, setError] = useState('');

  // Add-node form
  const [showAddNode, setShowAddNode] = useState(false);
  const [newNodeKind, setNewNodeKind] = useState<'agent' | 'person'>('agent');
  const [newNodeAgent, setNewNodeAgent] = useState('');
  const [newNodeUser, setNewNodeUser] = useState('');
  const [newNodeLabel, setNewNodeLabel] = useState('');
  const [newNodeDepartment, setNewNodeDepartment] = useState('');

  // Run-crew modal
  const [showRunModal, setShowRunModal] = useState(false);
  const [runPrompt, setRunPrompt] = useState('');
  const [launching, setLaunching] = useState(false);

  const loadCrews = useCallback(() => {
    crewsAPI
      .list(projectId || undefined)
      .then((res) => {
        const list = res.data || [];
        setCrews(list);
        setCrewId((current) => {
          if (current && list.some((c) => c.id === current)) return current;
          const def = list.find((c) => c.is_default);
          return def?.id || list[0]?.id || '';
        });
      })
      .catch((err: any) =>
        setError(err.response?.data?.error || err.message || 'Failed to load crews')
      );
  }, [projectId]);

  useEffect(() => {
    loadCrews();
    agentsAPI.list().then((res) => setAgents(res.data || [])).catch(() => setAgents([]));
  }, [loadCrews]);

  // Org members feed the person picker + human-node label fallbacks.
  useEffect(() => {
    if (!activeOrgId) {
      setMembers([]);
      return;
    }
    orgsAPI.members
      .list(activeOrgId)
      .then((res) => setMembers(res.data || []))
      .catch(() => setMembers([]));
  }, [activeOrgId]);

  const loadGraph = useCallback(() => {
    if (!crewId) {
      setGraph(null);
      return;
    }
    crewsAPI
      .get(crewId)
      .then((res) => setGraph(res.data))
      .catch((err: any) =>
        setError(err.response?.data?.error || err.message || 'Failed to load crew graph')
      );
  }, [crewId]);

  useEffect(() => {
    setSelectedNodeId(null);
    setSelectedEdgeId(null);
    setPendingEdge(null);
    loadGraph();
  }, [loadGraph]);

  // Poll runs for active node indicators
  useEffect(() => {
    if (!projectId) return;
    let cancelled = false;
    const poll = () => {
      agentRunsAPI
        .list({ project_id: projectId, limit: 20 })
        .then((res) => {
          if (cancelled) return;
          const ids = (res.data || [])
            .filter((r) => r.team_node_id && LIVE_STATUSES.includes(r.status))
            .map((r) => r.team_node_id as string);
          setActiveNodeIds(Array.from(new Set(ids)));
        })
        .catch(() => undefined);
    };
    poll();
    const timer = window.setInterval(poll, 5000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [projectId]);

  const selectedCrew = crews.find((c) => c.id === crewId) || null;
  const defaultCrew = crews.find((c) => c.is_default) || null;
  const selectedNode = graph?.nodes.find((n) => n.id === selectedNodeId) || null;
  const selectedEdge = graph?.edges.find((e) => e.id === selectedEdgeId) || null;

  const handleNewCrew = async () => {
    const name = window.prompt('New crew name');
    if (!name || !name.trim()) return;
    try {
      const res = await crewsAPI.create({ name: name.trim(), project_id: projectId || null });
      loadCrews();
      setCrewId(res.data.id);
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to create crew');
    }
  };

  const handleClone = async () => {
    if (!defaultCrew) {
      setError('No default crew available to clone.');
      return;
    }
    const name = window.prompt('Name for the new crew', `${defaultCrew.name} (copy)`);
    if (!name || !name.trim()) return;
    try {
      const res = await crewsAPI.clone(defaultCrew.id, name.trim(), projectId || null);
      loadCrews();
      setCrewId(res.data.id);
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to clone crew');
    }
  };

  const handleDeleteCrew = async () => {
    if (!selectedCrew) return;
    if (!window.confirm(`Delete crew "${selectedCrew.name}"?`)) return;
    try {
      await crewsAPI.remove(selectedCrew.id);
      setCrewId('');
      loadCrews();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to delete crew');
    }
  };

  const handleAddNode = async () => {
    if (!crewId) return;
    try {
      if (newNodeKind === 'person') {
        if (!newNodeUser) return;
        const member = members.find((m) => m.user_id === newNodeUser);
        await crewsAPI.addNode(crewId, {
          node_type: 'human',
          user_id: newNodeUser,
          label: newNodeLabel.trim() || member?.user_name || 'person',
          department: newNodeDepartment.trim(),
        });
      } else {
        if (!newNodeAgent) return;
        const agent = agents.find((a) => a.id === newNodeAgent);
        await crewsAPI.addNode(crewId, {
          node_type: 'agent',
          agent_id: newNodeAgent,
          label: newNodeLabel.trim() || agent?.name || 'node',
          department: newNodeDepartment.trim(),
        });
      }
      setShowAddNode(false);
      setNewNodeLabel('');
      setNewNodeAgent('');
      setNewNodeUser('');
      setNewNodeDepartment('');
      loadGraph();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to add node');
    }
  };

  const handleMoveNode = useCallback(
    async (nodeId: string, pos: { x: number; y: number }) => {
      const node = graph?.nodes.find((n) => n.id === nodeId);
      if (!node) return;
      try {
        await crewsAPI.updateNode(nodeId, {
          position: { ...(node.position || {}), [view]: pos },
        });
        // Update local copy so a rebuild keeps positions without a refetch flash.
        setGraph((g) =>
          g
            ? {
                ...g,
                nodes: g.nodes.map((n) =>
                  n.id === nodeId
                    ? { ...n, position: { ...(n.position || {}), [view]: pos } }
                    : n
                ),
              }
            : g
        );
      } catch (err: any) {
        setError(err.response?.data?.error || err.message || 'Failed to save node position');
      }
    },
    [graph, view]
  );

  const handleConnect = useCallback((from: string, to: string) => {
    setPendingEdge({ from, to });
  }, []);

  const handleCreateEdge = async (edgeType: string) => {
    if (!pendingEdge || !crewId) return;
    try {
      await crewsAPI.addEdge(crewId, {
        from_node_id: pendingEdge.from,
        to_node_id: pendingEdge.to,
        edge_type: edgeType,
      });
      setPendingEdge(null);
      setConnectMode(false);
      setError('');
      loadGraph();
    } catch (err: any) {
      setPendingEdge(null);
      setError(
        err.response?.data?.error || err.message || 'The server rejected this connection.'
      );
    }
  };

  const handleLaunch = async () => {
    if (!crewId || !runPrompt.trim()) return;
    setLaunching(true);
    try {
      await crewsAPI.launchRun(crewId, { project_id: projectId || undefined, prompt: runPrompt.trim() });
      setShowRunModal(false);
      setRunPrompt('');
      navigate(`/projects/${projectId}/agent-runs`);
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to launch crew run');
    } finally {
      setLaunching(false);
    }
  };

  const nodeLabel = useMemo(
    () => (id: string) => graph?.nodes.find((n) => n.id === id)?.label || id,
    [graph]
  );

  const pendingTargetIsHuman =
    !!pendingEdge &&
    graph?.nodes.find((n) => n.id === pendingEdge.to)?.node_type === 'human';
  const pendingEdgeTypes = pendingTargetIsHuman ? HUMAN_TARGET_EDGE_TYPES : EDGE_TYPES;

  const canvasMembers = useMemo(
    () => members.map((m) => ({ user_id: m.user_id, user_name: m.user_name })),
    [members]
  );

  return (
    <div style={{ padding: 20, height: '100%', display: 'flex', flexDirection: 'column' }}>
      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 12, flexWrap: 'wrap' }}>
        <h2 style={{ color: 'var(--text)', margin: 0 }}>Crew Builder</h2>
        <select
          value={crewId}
          onChange={(e) => setCrewId(e.target.value)}
          style={{ width: 240, padding: '6px 10px' }}
        >
          {crews.length === 0 && <option value="">No crews</option>}
          {crews.map((c) => (
            <option key={c.id} value={c.id}>
              {c.name}
              {c.is_default ? ' (Default)' : ''}
            </option>
          ))}
        </select>
        {selectedCrew?.is_default && (
          <span
            style={{
              fontSize: 11,
              background: 'var(--accent)',
              color: '#fff',
              borderRadius: 10,
              padding: '2px 8px',
            }}
          >
            Default
          </span>
        )}
        <button className="button-secondary" style={{ padding: '6px 12px', fontSize: 13 }} onClick={handleNewCrew}>
          New crew
        </button>
        <button
          className="button-secondary"
          style={{ padding: '6px 12px', fontSize: 13 }}
          onClick={handleClone}
          title="Start from default crew"
        >
          Start from default crew
        </button>
        {selectedCrew && !selectedCrew.is_default && (
          <button
            style={{
              background: 'var(--danger)',
              color: '#fff',
              border: 'none',
              padding: '6px 12px',
              borderRadius: 4,
              cursor: 'pointer',
              fontSize: 13,
            }}
            onClick={handleDeleteCrew}
          >
            Delete
          </button>
        )}
        <div style={{ flex: 1 }} />
        <button className="button" onClick={() => setShowRunModal(true)} disabled={!crewId}>
          ▶ Run crew
        </button>
      </div>

      {/* Toolbar */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 12, flexWrap: 'wrap' }}>
        <div style={{ display: 'inline-flex', border: '1px solid var(--border)', borderRadius: 4, overflow: 'hidden' }}>
          {(['org', 'network'] as const).map((v) => (
            <button
              key={v}
              onClick={() => setView(v)}
              style={{
                padding: '6px 14px',
                border: 'none',
                cursor: 'pointer',
                fontSize: 13,
                background: view === v ? 'var(--accent)' : 'var(--surface)',
                color: view === v ? '#fff' : 'var(--text)',
              }}
            >
              {v === 'org' ? 'Org Chart' : 'Network'}
            </button>
          ))}
        </div>
        <div style={{ display: 'inline-flex', border: '1px solid var(--border)', borderRadius: 4, overflow: 'hidden' }}>
          {FILTERS.map((f) => (
            <button
              key={f.value}
              onClick={() => setFilter(f.value)}
              style={{
                padding: '6px 14px',
                border: 'none',
                cursor: 'pointer',
                fontSize: 13,
                background: filter === f.value ? 'var(--purple)' : 'var(--surface)',
                color: filter === f.value ? '#fff' : 'var(--text)',
              }}
            >
              {f.label}
            </button>
          ))}
        </div>
        <button
          onClick={() => setConnectMode(!connectMode)}
          style={{
            padding: '6px 14px',
            borderRadius: 4,
            border: '1px solid ' + (connectMode ? 'var(--accent)' : 'var(--border)'),
            cursor: 'pointer',
            fontSize: 13,
            background: connectMode ? 'var(--accent)' : 'var(--surface)',
            color: connectMode ? '#fff' : 'var(--text)',
          }}
        >
          {connectMode ? 'Connecting: click source, then target' : 'Connect'}
        </button>
        <button
          className="button-secondary"
          style={{ padding: '6px 12px', fontSize: 13 }}
          onClick={() => setShowAddNode(!showAddNode)}
        >
          + Add node
        </button>
        {showAddNode && (
          <span style={{ display: 'inline-flex', gap: 6, alignItems: 'center', flexWrap: 'wrap' }}>
            <span style={{ display: 'inline-flex', border: '1px solid var(--border)', borderRadius: 4, overflow: 'hidden' }}>
              {(['agent', 'person'] as const).map((k) => (
                <button
                  key={k}
                  onClick={() => setNewNodeKind(k)}
                  style={{
                    padding: '6px 12px',
                    border: 'none',
                    cursor: 'pointer',
                    fontSize: 13,
                    background: newNodeKind === k ? (k === 'person' ? 'var(--purple)' : 'var(--accent)') : 'var(--surface)',
                    color: newNodeKind === k ? '#fff' : 'var(--text)',
                  }}
                >
                  {k === 'agent' ? 'Agent' : 'Person'}
                </button>
              ))}
            </span>
            {newNodeKind === 'agent' ? (
              <select
                value={newNodeAgent}
                onChange={(e) => setNewNodeAgent(e.target.value)}
                style={{ width: 180, padding: '6px 8px', fontSize: 13 }}
              >
                <option value="">Choose agent…</option>
                {agents.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name}
                  </option>
                ))}
              </select>
            ) : (
              <select
                value={newNodeUser}
                onChange={(e) => {
                  const uid = e.target.value;
                  setNewNodeUser(uid);
                  const member = members.find((m) => m.user_id === uid);
                  // Label defaults to the selected member's name.
                  setNewNodeLabel((current) => {
                    const wasDefault =
                      !current ||
                      members.some((m) => m.user_name === current);
                    return wasDefault ? member?.user_name || '' : current;
                  });
                }}
                style={{ width: 220, padding: '6px 8px', fontSize: 13 }}
              >
                <option value="">Choose person…</option>
                {members.map((m) => (
                  <option key={m.user_id} value={m.user_id}>
                    {m.user_name} ({m.user_email})
                  </option>
                ))}
              </select>
            )}
            <input
              value={newNodeLabel}
              onChange={(e) => setNewNodeLabel(e.target.value)}
              placeholder="Label (optional)"
              style={{ width: 160, padding: '6px 8px', fontSize: 13 }}
            />
            <input
              value={newNodeDepartment}
              onChange={(e) => setNewNodeDepartment(e.target.value)}
              placeholder="Department (optional)"
              list="crew-departments-toolbar"
              style={{ width: 170, padding: '6px 8px', fontSize: 13 }}
            />
            <datalist id="crew-departments-toolbar">
              {Array.from(
                new Set((graph?.nodes || []).map((n) => (n.department || '').trim()).filter(Boolean))
              ).map((d) => (
                <option key={d} value={d} />
              ))}
            </datalist>
            <button
              className="button"
              style={{ padding: '6px 12px', fontSize: 13 }}
              disabled={newNodeKind === 'agent' ? !newNodeAgent : !newNodeUser}
              onClick={handleAddNode}
            >
              Add
            </button>
          </span>
        )}
      </div>

      {error && (
        <div
          style={{
            background: 'var(--tint-red)',
            border: '1px solid var(--danger)',
            color: 'var(--danger-strong)',
            borderRadius: 4,
            padding: '8px 12px',
            marginBottom: 10,
            fontSize: 13,
          }}
        >
          {error}
          <button
            onClick={() => setError('')}
            style={{ background: 'none', border: 'none', float: 'right', cursor: 'pointer', color: 'var(--danger-strong)' }}
          >
            ×
          </button>
        </div>
      )}

      {/* Canvas + side panel */}
      <div style={{ display: 'flex', gap: 14, flex: 1, minHeight: 0 }}>
        <div style={{ flex: 1, minWidth: 0 }}>
          {graph ? (
            <CrewCanvas
              graph={graph}
              view={view}
              filter={filter}
              agents={agents}
              members={canvasMembers}
              activeNodeIds={activeNodeIds}
              connectMode={connectMode}
              onSelectNode={(id) => {
                setSelectedNodeId(id);
                if (id) setSelectedEdgeId(null);
              }}
              onSelectEdge={(id) => {
                setSelectedEdgeId(id);
                if (id) setSelectedNodeId(null);
              }}
              onMoveNode={handleMoveNode}
              onConnect={handleConnect}
            />
          ) : (
            <div style={{ color: 'var(--text-muted)', padding: 40 }}>
              {crews.length === 0 ? 'Create a crew to get started.' : 'Loading crew…'}
            </div>
          )}
        </div>
        {(selectedNode || selectedEdge) && graph && (
          <div style={{ width: 320, flexShrink: 0, overflowY: 'auto' }}>
            {selectedNode && (
              <NodeConfigPanel
                node={selectedNode}
                graph={graph}
                agents={agents}
                members={members}
                onChanged={loadGraph}
                onClose={() => setSelectedNodeId(null)}
              />
            )}
            {selectedEdge && (
              <EdgeConfigPanel
                edge={selectedEdge}
                graph={graph}
                onChanged={loadGraph}
                onClose={() => setSelectedEdgeId(null)}
              />
            )}
          </div>
        )}
      </div>

      {/* Edge-type picker popup */}
      {pendingEdge && (
        <div
          style={{
            position: 'fixed',
            inset: 0,
            background: 'rgba(0,0,0,0.35)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            zIndex: 200,
          }}
          onClick={() => setPendingEdge(null)}
        >
          <div
            className="card"
            style={{ width: 360, marginBottom: 0 }}
            onClick={(e) => e.stopPropagation()}
          >
            <h3 style={{ fontSize: 15 }}>Connection type</h3>
            <p style={{ fontSize: 13 }}>
              <strong>{nodeLabel(pendingEdge.from)}</strong> → <strong>{nodeLabel(pendingEdge.to)}</strong>
            </p>
            {pendingTargetIsHuman && (
              <p style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                The target is a person — work sent to them appears as a Board card assigned to
                them. Delegation to people isn't supported.
              </p>
            )}
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              {pendingEdgeTypes.map((et) => (
                <button
                  key={et.value}
                  onClick={() => handleCreateEdge(et.value)}
                  style={{
                    background: et.color,
                    color: '#fff',
                    border: 'none',
                    padding: '10px 14px',
                    borderRadius: 4,
                    cursor: 'pointer',
                    fontSize: 13,
                    fontWeight: 600,
                    textAlign: 'left',
                  }}
                >
                  {et.label}
                </button>
              ))}
              <button
                className="button-secondary"
                style={{ padding: '8px 14px', fontSize: 13 }}
                onClick={() => setPendingEdge(null)}
              >
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Run crew modal */}
      {showRunModal && (
        <div
          style={{
            position: 'fixed',
            inset: 0,
            background: 'rgba(0,0,0,0.35)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            zIndex: 200,
          }}
          onClick={() => setShowRunModal(false)}
        >
          <div
            className="card"
            style={{ width: 520, marginBottom: 0 }}
            onClick={(e) => e.stopPropagation()}
          >
            <h3 style={{ fontSize: 15 }}>Run crew: {selectedCrew?.name}</h3>
            <div className="form-group">
              <label>Prompt</label>
              <textarea
                value={runPrompt}
                onChange={(e) => setRunPrompt(e.target.value)}
                placeholder="What should the crew work on?"
                style={{ minHeight: 110, fontSize: 13 }}
                autoFocus
              />
            </div>
            <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
              <button className="button-secondary" onClick={() => setShowRunModal(false)}>
                Cancel
              </button>
              <button className="button" onClick={handleLaunch} disabled={launching || !runPrompt.trim()}>
                {launching ? 'Launching…' : 'Launch'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
