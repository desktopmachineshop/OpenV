import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useParams } from 'react-router-dom';
import {
  AgentDef,
  AgentRun,
  agentRunsAPI,
  agentsAPI,
  Artifact,
  artifactAPI,
  authAPI,
  Crew,
  crewsAPI,
  User,
  WorkItem,
  workItemsAPI,
} from '../api/client';
import { useAppStore } from '../state/store';
import { WorkItemDrawer } from '../components/kanban/WorkItemDrawer';

const COLUMNS: { key: string; label: string; color: string }[] = [
  { key: 'backlog', label: 'Backlog', color: 'var(--neutral)' },
  { key: 'todo', label: 'To Do', color: 'var(--accent)' },
  { key: 'in-progress', label: 'In Progress', color: 'var(--warning)' },
  { key: 'review', label: 'Review', color: 'var(--purple-soft)' },
  { key: 'done', label: 'Done', color: 'var(--success)' },
];

const LIVE_STATUSES = ['queued', 'claimed', 'running'];

interface ComposerState {
  title: string;
  description: string;
  assignee: string; // '', 'me', 'agent:<id>', 'team:<id>'
  artifactIds: string;
  expanded: boolean;
}

const emptyComposer = (): ComposerState => ({
  title: '',
  description: '',
  assignee: '',
  artifactIds: '',
  expanded: false,
});

export const KanbanBoard: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const currentUser = useAppStore((s) => s.currentUser);
  const projectId = params.projectId || storeProjectId;
  const activeOrgId = useAppStore((s) => s.activeOrgId);

  const [items, setItems] = useState<WorkItem[]>([]);
  const [agents, setAgents] = useState<AgentDef[]>([]);
  const [crews, setCrews] = useState<Crew[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [artifactMap, setArtifactMap] = useState<Record<string, Artifact>>({});
  const [runsByWorkItem, setRunsByWorkItem] = useState<Record<string, AgentRun>>({});
  const [error, setError] = useState('');
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [composers, setComposers] = useState<Record<string, ComposerState>>({});
  const [dragOverColumn, setDragOverColumn] = useState<string | null>(null);
  const dragItemId = useRef<string | null>(null);

  const loadItems = useCallback(() => {
    if (!projectId) return;
    workItemsAPI
      .list(projectId)
      .then((res) => setItems(res.data || []))
      .catch((err: any) =>
        setError(err.response?.data?.error || err.message || 'Failed to load board')
      );
    // activeOrgId: requests are scoped by the X-Org-ID header the API client
    // injects, so refetch when the active workspace changes (e.g.
    // ProjectLayout's cross-org deep-link sync, issue #99).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId, activeOrgId]);

  useEffect(() => {
    loadItems();
  }, [loadItems]);

  // Static-ish caches
  useEffect(() => {
    agentsAPI.list().then((res) => setAgents(res.data || [])).catch(() => setAgents([]));
    crewsAPI.list(projectId).then((res) => setCrews(res.data || [])).catch(() => setCrews([]));
    authAPI.listUsers().then((res) => setUsers(res.data || [])).catch(() => setUsers([]));
    if (projectId) {
      artifactAPI
        .list(projectId)
        .then((res) => {
          const map: Record<string, Artifact> = {};
          (res.data || []).forEach((a) => {
            map[a.id] = a;
          });
          setArtifactMap(map);
        })
        .catch(() => setArtifactMap({}));
    }
    // activeOrgId: same X-Org-ID refetch rationale as loadItems (issue #99).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId, activeOrgId]);

  // Poll agent runs every 5s to show live indicators
  useEffect(() => {
    if (!projectId) return;
    let cancelled = false;
    const poll = () => {
      agentRunsAPI
        .list({ project_id: projectId, limit: 50 })
        .then((res) => {
          if (cancelled) return;
          const byItem: Record<string, AgentRun> = {};
          (res.data || []).forEach((run) => {
            if (!run.work_item_id) return;
            const existing = byItem[run.work_item_id];
            if (!existing || (run.created_at > existing.created_at)) {
              byItem[run.work_item_id] = run;
            }
          });
          setRunsByWorkItem(byItem);
        })
        .catch(() => undefined);
    };
    poll();
    const timer = window.setInterval(poll, 5000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
    // activeOrgId: same X-Org-ID refetch rationale as loadItems (issue #99).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId, activeOrgId]);

  const agentById = useMemo(() => {
    const m: Record<string, AgentDef> = {};
    agents.forEach((a) => {
      m[a.id] = a;
    });
    return m;
  }, [agents]);

  const crewById = useMemo(() => {
    const m: Record<string, Crew> = {};
    crews.forEach((c) => {
      m[c.id] = c;
    });
    return m;
  }, [crews]);

  const userById = useMemo(() => {
    const m: Record<string, User> = {};
    users.forEach((u) => {
      m[u.id] = u;
    });
    return m;
  }, [users]);

  const itemsByColumn = useMemo(() => {
    const map: Record<string, WorkItem[]> = {};
    COLUMNS.forEach((c) => {
      map[c.key] = [];
    });
    items.forEach((item) => {
      const col = map[item.column] ? item.column : 'backlog';
      map[col].push(item);
    });
    Object.values(map).forEach((list) => list.sort((a, b) => a.sort_order - b.sort_order));
    return map;
  }, [items]);

  const isRunLive = (item: WorkItem): boolean => {
    const run = runsByWorkItem[item.id];
    return !!run && LIVE_STATUSES.includes(run.status);
  };

  const isDragDisabled = (item: WorkItem): boolean =>
    (item.assignee_type === 'agent' || item.assignee_type === 'team') && isRunLive(item);

  const handleDrop = async (columnKey: string) => {
    const id = dragItemId.current;
    dragItemId.current = null;
    setDragOverColumn(null);
    if (!id) return;
    const item = items.find((i) => i.id === id);
    if (!item) return;
    const targetIndex = (itemsByColumn[columnKey] || []).filter((i) => i.id !== id).length;
    // Optimistic update
    setItems((prev) =>
      prev.map((i) => (i.id === id ? { ...i, column: columnKey, sort_order: targetIndex } : i))
    );
    try {
      await workItemsAPI.move(id, columnKey, targetIndex);
      loadItems();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to move card');
      loadItems();
    }
  };

  const setComposer = (col: string, update: Partial<ComposerState>) => {
    setComposers((prev) => ({
      ...prev,
      [col]: { ...(prev[col] || emptyComposer()), ...update },
    }));
  };

  const handleCreate = async (col: string) => {
    const c = composers[col];
    if (!c || !c.title.trim() || !projectId) return;
    let assignee_type: WorkItem['assignee_type'] = 'user';
    let assignee_id: string | null = null;
    if (c.assignee === 'me') {
      assignee_type = 'user';
      assignee_id = currentUser?.id || null;
    } else if (c.assignee.startsWith('agent:')) {
      assignee_type = 'agent';
      assignee_id = c.assignee.slice(6);
    } else if (c.assignee.startsWith('team:')) {
      assignee_type = 'team';
      assignee_id = c.assignee.slice(5);
    }
    const artifact_ids = c.artifactIds
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean);
    try {
      await workItemsAPI.create(projectId, {
        title: c.title.trim(),
        description: c.description,
        column: col,
        assignee_type,
        assignee_id,
        artifact_ids,
      });
      setComposers((prev) => ({ ...prev, [col]: emptyComposer() }));
      loadItems();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to create card');
    }
  };

  const renderAssigneeBadge = (item: WorkItem) => {
    if (!item.assignee_id) return null;
    if (item.assignee_type === 'agent') {
      const agent = agentById[item.assignee_id];
      return (
        <span
          title={`Agent: ${agent?.name || item.assignee_id}`}
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 4,
            fontSize: 11,
            background: 'var(--tint-purple)',
            border: '1px solid var(--tint-purple-border)',
            borderRadius: 10,
            padding: '2px 8px',
            color: 'var(--text)',
          }}
        >
          <span role="img" aria-label="agent">🤖</span>
          {agent?.name || 'agent'}
        </span>
      );
    }
    // assignee_type 'team' is the wire value for crews (unchanged JSON).
    if (item.assignee_type === 'team') {
      const crew = crewById[item.assignee_id];
      return (
        <span
          title={`Crew: ${crew?.name || item.assignee_id}`}
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 4,
            fontSize: 11,
            background: 'var(--tint-blue)',
            border: '1px solid var(--tint-blue-border)',
            borderRadius: 10,
            padding: '2px 8px',
            color: 'var(--text)',
          }}
        >
          <span role="img" aria-label="crew">👥</span>
          {crew?.name || 'crew'}
        </span>
      );
    }
    const user = userById[item.assignee_id];
    const label = user?.name || user?.email || '?';
    return (
      <span
        title={label}
        style={{
          width: 22,
          height: 22,
          borderRadius: '50%',
          background: 'var(--accent)',
          color: '#fff',
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          fontSize: 11,
          fontWeight: 700,
        }}
      >
        {label.charAt(0).toUpperCase()}
      </span>
    );
  };

  return (
    <div style={{ padding: 20, height: '100%', display: 'flex', flexDirection: 'column' }}>
      <style>
        {`@keyframes ovPulse { 0% { opacity: 1; } 50% { opacity: 0.25; } 100% { opacity: 1; } }`}
      </style>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 14, marginBottom: 6 }}>
        <h2 style={{ color: 'var(--text)', margin: 0 }}>Board</h2>
        <span style={{ color: 'var(--text-muted)', fontSize: 13 }}>
          Tip: assign a card to an agent and drag it to To Do to launch the agent.
        </span>
      </div>
      {error && <div style={{ color: 'var(--danger)', fontSize: 13, marginBottom: 8 }}>{error}</div>}

      <div style={{ display: 'flex', gap: 12, flex: 1, overflowX: 'auto', alignItems: 'flex-start' }}>
        {COLUMNS.map((col) => {
          const composer = composers[col.key] || emptyComposer();
          return (
            <div
              key={col.key}
              onDragOver={(e) => {
                e.preventDefault();
                setDragOverColumn(col.key);
              }}
              onDragLeave={() => setDragOverColumn((c) => (c === col.key ? null : c))}
              onDrop={(e) => {
                e.preventDefault();
                handleDrop(col.key);
              }}
              style={{
                flex: '0 0 260px',
                background: dragOverColumn === col.key ? 'var(--tint-blue)' : 'var(--surface-alt)',
                borderRadius: 6,
                padding: 10,
                maxHeight: '100%',
                overflowY: 'auto',
                border: dragOverColumn === col.key ? '2px dashed var(--accent)' : '2px solid transparent',
              }}
            >
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 8,
                  marginBottom: 10,
                  padding: '0 2px',
                }}
              >
                <span
                  style={{ width: 10, height: 10, borderRadius: '50%', background: col.color }}
                />
                <strong style={{ fontSize: 13, color: 'var(--text)' }}>{col.label}</strong>
                <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                  {(itemsByColumn[col.key] || []).length}
                </span>
              </div>

              {(itemsByColumn[col.key] || []).map((item) => {
                const disabled = isDragDisabled(item);
                const live = isRunLive(item);
                return (
                  <div
                    key={item.id}
                    draggable={!disabled}
                    onDragStart={() => {
                      dragItemId.current = item.id;
                    }}
                    onClick={() => setSelectedId(item.id)}
                    title={disabled ? 'Agent run in progress — card is locked' : undefined}
                    style={{
                      background: 'var(--surface)',
                      borderRadius: 4,
                      boxShadow: '0 1px 3px rgba(0,0,0,0.12)',
                      padding: '10px 12px',
                      marginBottom: 8,
                      cursor: disabled ? 'not-allowed' : 'grab',
                      opacity: disabled ? 0.85 : 1,
                      border: '1px solid var(--surface-alt)',
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'flex-start', gap: 6 }}>
                      <div style={{ flex: 1, fontSize: 13, color: 'var(--text)', fontWeight: 600 }}>
                        {item.title}
                      </div>
                      {live && (
                        <span
                          title={`Run ${runsByWorkItem[item.id]?.status}`}
                          style={{
                            width: 9,
                            height: 9,
                            borderRadius: '50%',
                            background: 'var(--accent)',
                            animation: 'ovPulse 1.2s ease-in-out infinite',
                            marginTop: 3,
                            flexShrink: 0,
                          }}
                        />
                      )}
                    </div>
                    <div
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 6,
                        marginTop: 8,
                        flexWrap: 'wrap',
                      }}
                    >
                      {renderAssigneeBadge(item)}
                      {item.artifact_ids && item.artifact_ids.length > 0 && (
                        <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
                          🔗 {item.artifact_ids.length}
                        </span>
                      )}
                    </div>
                  </div>
                );
              })}

              {/* Composer */}
              {composer.expanded ? (
                <div
                  style={{
                    background: 'var(--surface)',
                    borderRadius: 4,
                    padding: 10,
                    border: '1px solid var(--border)',
                  }}
                >
                  <input
                    value={composer.title}
                    autoFocus
                    onChange={(e) => setComposer(col.key, { title: e.target.value })}
                    placeholder="Card title"
                    style={{ fontSize: 13, marginBottom: 8 }}
                  />
                  <textarea
                    value={composer.description}
                    onChange={(e) => setComposer(col.key, { description: e.target.value })}
                    placeholder="Description (optional)"
                    style={{ fontSize: 13, minHeight: 50, marginBottom: 8 }}
                  />
                  <select
                    value={composer.assignee}
                    onChange={(e) => setComposer(col.key, { assignee: e.target.value })}
                    style={{ fontSize: 13, marginBottom: 8 }}
                  >
                    <option value="">Unassigned</option>
                    <option value="me">Me{currentUser?.name ? ` (${currentUser.name})` : ''}</option>
                    <optgroup label="Agents">
                      {agents.map((a) => (
                        <option key={a.id} value={`agent:${a.id}`}>
                          🤖 {a.name}
                        </option>
                      ))}
                    </optgroup>
                    <optgroup label="Crews">
                      {crews.map((c) => (
                        <option key={c.id} value={`team:${c.id}`}>
                          👥 {c.name}
                        </option>
                      ))}
                    </optgroup>
                  </select>
                  <input
                    value={composer.artifactIds}
                    onChange={(e) => setComposer(col.key, { artifactIds: e.target.value })}
                    placeholder="Artifact IDs, comma-separated (optional)"
                    style={{ fontSize: 12, marginBottom: 8 }}
                  />
                  <div style={{ display: 'flex', gap: 6 }}>
                    <button
                      className="button"
                      style={{ padding: '6px 12px', fontSize: 12 }}
                      disabled={!composer.title.trim()}
                      onClick={() => handleCreate(col.key)}
                    >
                      Add card
                    </button>
                    <button
                      className="button-secondary"
                      style={{ padding: '6px 12px', fontSize: 12 }}
                      onClick={() => setComposer(col.key, { expanded: false })}
                    >
                      Cancel
                    </button>
                  </div>
                </div>
              ) : (
                <button
                  onClick={() => setComposer(col.key, { expanded: true })}
                  style={{
                    width: '100%',
                    background: 'none',
                    border: 'none',
                    color: 'var(--text-muted)',
                    fontSize: 13,
                    padding: '6px 4px',
                    textAlign: 'left',
                    cursor: 'pointer',
                  }}
                >
                  + Add card
                </button>
              )}
            </div>
          );
        })}
      </div>

      {selectedId && projectId && (
        <WorkItemDrawer
          workItemId={selectedId}
          projectId={projectId}
          artifactMap={artifactMap}
          onClose={() => setSelectedId(null)}
          onChanged={loadItems}
          onDeleted={() => {
            setSelectedId(null);
            loadItems();
          }}
        />
      )}
    </div>
  );
};
