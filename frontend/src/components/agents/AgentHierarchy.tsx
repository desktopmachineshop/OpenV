import React, { useMemo, useState } from 'react';
import type { AgentDef, AgentPlacement, AgentStats } from '../../api/client';
import { useViewport } from '../../hooks/useViewport';
import { AgentHierarchyCard, CardArrange, StatLine } from './AgentHierarchyCard';
import { HierarchyNode, buildHierarchy, groupColor, placementOf, subtree, totalsOf } from './agentHierarchy';

// The Agents page's hierarchy of cards (issue 733): the top cards full
// width, then one column per group with its leads and their reports nested
// beneath. On a phone the groups stack in one column, each collapsible.
// Edit mode arranges it: Reports to and Group on each card, ↑ ↓ among
// siblings, each change saved as it is made (onArrange).

interface Props {
  agents: AgentDef[];
  stats: Record<string, AgentStats>;
  editing: boolean;
  onArrange: (placements: AgentPlacement[]) => Promise<void>;
  onOpen: (agent: AgentDef) => void;
  onEdit: (agent: AgentDef) => void;
  onLaunch: (agent: AgentDef) => void;
}

export const AgentHierarchy: React.FC<Props> = ({ agents, stats, editing, onArrange, onOpen, onEdit, onLaunch }) => {
  const { isPhone } = useViewport();
  const tree = useMemo(() => buildHierarchy(agents), [agents]);
  const byId = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);
  const [saving, setSaving] = useState(false);
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});

  const save = async (placements: AgentPlacement[]) => {
    setSaving(true);
    try {
      await onArrange(placements);
    } finally {
      setSaving(false);
    }
  };

  // The siblings an agent moves among: the children of its drawn parent, or
  // the roots of its column.
  const siblingsOf = (agent: AgentDef): HierarchyNode[] => {
    const find = (nodes: HierarchyNode[]): HierarchyNode[] | null => {
      if (nodes.some((n) => n.agent.id === agent.id)) return nodes;
      for (const n of nodes) {
        const got = find(n.children);
        if (got) return got;
      }
      return null;
    };
    return find(tree.top) || tree.groups.map((g) => find(g.leads)).find(Boolean) || [];
  };

  const arrange: CardArrange | undefined = editing
    ? {
        agents,
        groups: tree.groups.map((g) => g.name),
        saving,
        onReportsTo: (agent, parentId) =>
          save([{ ...placementOf(agent, byId), reports_to: parentId ? byId.get(parentId)?.slug || '' : '' }]),
        onGroup: (agent, group) => save([{ ...placementOf(agent, byId), group_name: group }]),
        onMove: (agent, delta) => {
          const siblings = siblingsOf(agent).map((n) => n.agent);
          const i = siblings.findIndex((a) => a.id === agent.id);
          const j = i + delta;
          if (i < 0 || j < 0 || j >= siblings.length) return;
          const order = [...siblings];
          [order[i], order[j]] = [order[j], order[i]];
          // Renumber the siblings 0, 1, 2…, so ties left by older agents
          // (all 0) stop deciding the order by name.
          save(order.map((a, k) => ({ ...placementOf(a, byId), sort_order: k })));
        },
      }
    : undefined;

  const cards = (nodes: HierarchyNode[]) =>
    nodes.map((n, i) => (
      <AgentHierarchyCard
        key={n.agent.id}
        node={n}
        stats={stats}
        arrange={arrange}
        first={i === 0}
        last={i === nodes.length - 1}
        onOpen={onOpen}
        onEdit={onEdit}
        onLaunch={onLaunch}
      />
    ));

  const all = totalsOf(agents, stats);
  return (
    <div className="agent-hierarchy" data-help-key="agents.list">
      <datalist id="agent-groups">
        {tree.groups.map((g) => (
          <option key={g.name} value={g.name} />
        ))}
      </datalist>
      {tree.top.length > 0 && (
        <div className="agent-top" style={{ display: 'grid', gap: 10, marginBottom: 16 }}>
          {cards(tree.top)}
          <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
            Everyone: <StatLineInline totals={all} />
          </div>
        </div>
      )}
      <div
        className="agent-groups"
        style={{
          display: 'grid',
          gap: 14,
          gridTemplateColumns: isPhone ? '1fr' : 'repeat(auto-fit, minmax(240px, 1fr))',
          alignItems: 'start',
        }}
      >
        {tree.groups.map((g) => {
          const members = g.leads.flatMap(subtree);
          const totals = totalsOf(members, stats);
          const shut = isPhone && collapsed[g.name];
          return (
            <section key={g.name} className="agent-group" aria-label={`Group ${g.name}`}>
              <button
                type="button"
                className="agent-group-header"
                aria-expanded={!shut}
                onClick={() => isPhone && setCollapsed((c) => ({ ...c, [g.name]: !c[g.name] }))}
                style={{
                  display: 'block',
                  width: '100%',
                  textAlign: 'left',
                  background: 'none',
                  border: 'none',
                  padding: '0 0 8px',
                  cursor: isPhone ? 'pointer' : 'default',
                  color: 'var(--text)',
                }}
              >
                <span style={{ display: 'flex', alignItems: 'center', gap: 8, fontWeight: 700, fontSize: 14 }}>
                  <span
                    aria-hidden="true"
                    style={{ width: 10, height: 10, borderRadius: '50%', background: groupColor(g.name), flexShrink: 0 }}
                  />
                  {g.name}
                  <span style={{ fontWeight: 400, fontSize: 12, color: 'var(--text-muted)' }}>
                    {members.length} agent{members.length === 1 ? '' : 's'}
                  </span>
                  {isPhone && <span style={{ marginLeft: 'auto' }}>{shut ? '▸' : '▾'}</span>}
                </span>
                <span style={{ display: 'block', marginTop: 4 }}>
                  <StatLine open={totals.open} done={totals.done} cost={totals.cost} pending={totals.pending} />
                </span>
              </button>
              {!shut && <div style={{ display: 'grid', gap: 10 }}>{cards(g.leads)}</div>}
            </section>
          );
        })}
      </div>
    </div>
  );
};

const StatLineInline: React.FC<{ totals: ReturnType<typeof totalsOf> }> = ({ totals }) => (
  <span style={{ display: 'inline-block', verticalAlign: 'middle' }}>
    <StatLine open={totals.open} done={totals.done} cost={totals.cost} pending={totals.pending} />
  </span>
);
