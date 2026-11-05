import React, { useState } from 'react';
import type { AgentDef, AgentStats } from '../../api/client';
import { buttonStyle, chipStyle } from '../ui';
import { HierarchyNode, groupColor, possibleParents } from './agentHierarchy';

// One card of the Agents page's hierarchy (issue 733), with the agents that
// report to it nested beneath on a guide line in its group's colour. In Edit
// mode the card shows its Reports to and Group pickers and ↑ ↓ buttons in
// place of its numbers' actions.

export interface CardArrange {
  agents: AgentDef[];
  groups: string[];
  onReportsTo: (agent: AgentDef, parentId: string) => void;
  onGroup: (agent: AgentDef, group: string) => void;
  onMove: (agent: AgentDef, delta: -1 | 1) => void;
  saving: boolean;
}

interface CardProps {
  node: HierarchyNode;
  stats: Record<string, AgentStats>;
  /** Set in Edit mode. */
  arrange?: CardArrange;
  /** The node's place among its siblings, for ↑ ↓. */
  first?: boolean;
  last?: boolean;
  onOpen: (agent: AgentDef) => void;
  onEdit: (agent: AgentDef) => void;
  onLaunch: (agent: AgentDef) => void;
  depth?: number;
}

const money = (usd: number) => `$${usd.toFixed(usd >= 100 ? 0 : 2)}`;

export const StatLine: React.FC<{ open: number; done: number; cost: number; pending: number }> = ({
  open,
  done,
  cost,
  pending,
}) => (
  <div className="agent-card-stats" style={{ display: 'flex', gap: 12, flexWrap: 'wrap', fontSize: 12, color: 'var(--text-muted)' }}>
    <span title="Runs still open, of those started in the last 7 days">
      <strong style={{ color: 'var(--text)' }}>{open}</strong> open
    </span>
    <span title="Runs that succeeded in the last 7 days">
      <strong style={{ color: 'var(--text)' }}>{done}</strong> done
    </span>
    <span title="What its runs of the last 7 days cost">
      <strong style={{ color: 'var(--text)' }}>{money(cost)}</strong>
    </span>
    <span title="Proposals of its runs waiting for review">
      <strong style={{ color: pending ? 'var(--warning-text)' : 'var(--text)' }}>{pending}</strong> to approve
    </span>
  </div>
);

export const AgentHierarchyCard: React.FC<CardProps> = ({
  node,
  stats,
  arrange,
  first,
  last,
  onOpen,
  onEdit,
  onLaunch,
  depth = 0,
}) => {
  const { agent, group } = node;
  const color = groupColor(group);
  const [open, setOpen] = useState(false);
  const s = stats[agent.id];
  const parentOptions = arrange ? possibleParents(agent, arrange.agents) : [];
  return (
    <div className="agent-tree-node" style={{ marginTop: depth ? 8 : 0 }}>
      <div
        className="agent-card"
        data-agent={agent.slug}
        role="button"
        tabIndex={0}
        aria-label={`${agent.name}: see its runs and proposals`}
        onClick={() => !arrange && onOpen(agent)}
        onKeyDown={(e) => {
          if (!arrange && (e.key === 'Enter' || e.key === ' ')) {
            e.preventDefault();
            onOpen(agent);
          }
        }}
        style={{
          background: 'var(--surface)',
          border: '1px solid var(--border)',
          borderTop: `3px solid ${color}`,
          borderRadius: 6,
          padding: '10px 12px',
          cursor: arrange ? 'default' : 'pointer',
          boxShadow: '0 1px 3px rgba(0,0,0,0.06)',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
          <strong style={{ flex: 1, minWidth: 0, fontSize: 14, color: 'var(--text)' }}>🤖 {agent.name}</strong>
          {group && (
            <span className="agent-group-chip" style={{ ...chipStyle('neutral'), borderLeft: `3px solid ${color}` }}>
              {group}
            </span>
          )}
          {agent.locked && <span title="Locked: platform updates never change this agent">🔒</span>}
        </div>
        {arrange ? (
          <div style={{ display: 'grid', gap: 6, marginTop: 8 }} onClick={(e) => e.stopPropagation()}>
            <label style={{ fontSize: 12, color: 'var(--text-muted)' }}>
              Reports to
              <select
                aria-label={`${agent.name} reports to`}
                value={agent.parent_agent_id || ''}
                disabled={arrange.saving}
                onChange={(e) => arrange.onReportsTo(agent, e.target.value)}
                style={{ display: 'block', width: '100%', marginTop: 2 }}
              >
                <option value="">No one (the top)</option>
                {parentOptions.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </label>
            <label style={{ fontSize: 12, color: 'var(--text-muted)' }}>
              Group
              <input
                aria-label={`${agent.name} group`}
                list="agent-groups"
                defaultValue={agent.group_name || ''}
                disabled={arrange.saving}
                placeholder={group && !agent.group_name ? `${group} (from its lead)` : 'No group'}
                onBlur={(e) => {
                  if (e.target.value.trim() !== (agent.group_name || '')) arrange.onGroup(agent, e.target.value.trim());
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') (e.target as HTMLInputElement).blur();
                }}
                style={{ display: 'block', width: '100%', marginTop: 2 }}
              />
            </label>
            <div style={{ display: 'flex', gap: 6 }}>
              <button
                type="button"
                aria-label={`Move ${agent.name} up`}
                style={buttonStyle('secondary', { compact: true, disabled: first || arrange.saving })}
                disabled={first || arrange.saving}
                onClick={() => arrange.onMove(agent, -1)}
              >
                ↑
              </button>
              <button
                type="button"
                aria-label={`Move ${agent.name} down`}
                style={buttonStyle('secondary', { compact: true, disabled: last || arrange.saving })}
                disabled={last || arrange.saving}
                onClick={() => arrange.onMove(agent, 1)}
              >
                ↓
              </button>
            </div>
          </div>
        ) : (
          <>
            <div style={{ marginTop: 6 }}>
              <StatLine open={s?.open || 0} done={s?.done || 0} cost={s?.cost_usd || 0} pending={s?.pending_proposals || 0} />
            </div>
            <div style={{ display: 'flex', gap: 6, marginTop: 8, flexWrap: 'wrap' }} onClick={(e) => e.stopPropagation()}>
              <button
                type="button"
                aria-expanded={open}
                style={buttonStyle('ghost', { compact: true })}
                onClick={() => setOpen((o) => !o)}
              >
                {open ? '▾' : '▸'} Mission and tools
              </button>
              <button type="button" data-help-key="agents.launch" style={buttonStyle('secondary', { compact: true })} onClick={() => onLaunch(agent)}>
                ▶ Launch
              </button>
              <button type="button" data-help-key="agents.editor" style={buttonStyle('secondary', { compact: true })} onClick={() => onEdit(agent)}>
                Edit agent
              </button>
            </div>
            {open && (
              <div className="agent-mission" style={{ fontSize: 12, color: 'var(--text-body)', marginTop: 8, display: 'grid', gap: 4 }}>
                {agent.description && <div>{agent.description}</div>}
                <div>
                  <span style={{ color: 'var(--text-muted)' }}>Runs on </span>
                  {agent.provider}
                  {agent.model ? ` · ${agent.model}` : ''}
                </div>
                <div>
                  <span style={{ color: 'var(--text-muted)' }}>Write mode </span>
                  {agent.write_mode === 'direct' ? 'direct: writes land at once' : 'proposal: writes wait for review'}
                </div>
                <div style={{ wordBreak: 'break-word' }}>
                  <span style={{ color: 'var(--text-muted)' }}>Tools </span>
                  {(agent.allowed_tools || []).join(', ') || 'none'}
                </div>
              </div>
            )}
          </>
        )}
      </div>
      {node.children.length > 0 && (
        <div
          className="agent-tree-children"
          style={{ marginLeft: 14, paddingLeft: 12, borderLeft: `2px solid ${color}` }}
        >
          {node.children.map((child, i) => (
            <AgentHierarchyCard
              key={child.agent.id}
              node={child}
              stats={stats}
              arrange={arrange}
              first={i === 0}
              last={i === node.children.length - 1}
              onOpen={onOpen}
              onEdit={onEdit}
              onLaunch={onLaunch}
              depth={depth + 1}
            />
          ))}
        </div>
      )}
    </div>
  );
};
