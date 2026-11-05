// The Agents page's hierarchy (issue 733): the workspace's agents arranged by
// who each reports to (parent_agent_id) and the group each is in, the way the
// page draws them. Display and arrangement only: no agent starts another.
//
// - The top: agents in no group (their own, or inherited from the agent they
//   report to) and reporting to no one, with the ungrouped agents beneath them.
// - One column per group: the group's leads (agents of the group whose parent
//   is outside it, or who have none), each with the agents of the group that
//   report to it nested beneath, as deep as they go.
//
// An agent with no group takes its parent's, so a sub-agent need not repeat
// its lead's group. Siblings are ordered by sort_order, then name.

import type { AgentDef, AgentStats } from '../../api/client';

export interface HierarchyNode {
  agent: AgentDef;
  /** The group the agent is drawn in: its own, or the nearest parent's. */
  group: string;
  children: HierarchyNode[];
}

export interface HierarchyGroup {
  name: string;
  leads: HierarchyNode[];
}

export interface Hierarchy {
  top: HierarchyNode[];
  groups: HierarchyGroup[];
}

const bySortThenName = (a: AgentDef, b: AgentDef) =>
  (a.sort_order ?? 0) - (b.sort_order ?? 0) || a.name.localeCompare(b.name) || a.slug.localeCompare(b.slug);

/** The group an agent is drawn in: its own, else the nearest parent's. */
export const effectiveGroup = (agent: AgentDef, byId: Map<string, AgentDef>): string => {
  const seen = new Set<string>();
  for (let at: AgentDef | undefined = agent; at && !seen.has(at.id); at = byId.get(at.parent_agent_id || '')) {
    seen.add(at.id);
    if (at.group_name) return at.group_name;
  }
  return '';
};

export const buildHierarchy = (agents: AgentDef[]): Hierarchy => {
  const byId = new Map(agents.map((a) => [a.id, a]));
  const group = new Map(agents.map((a) => [a.id, effectiveGroup(a, byId)]));
  const childrenOf = new Map<string, AgentDef[]>();
  const roots: AgentDef[] = [];
  for (const a of agents) {
    const parent = a.parent_agent_id ? byId.get(a.parent_agent_id) : undefined;
    // A parent in another group draws the agent as a lead of its own group.
    if (parent && parent.id !== a.id && group.get(parent.id) === group.get(a.id)) {
      childrenOf.set(parent.id, [...(childrenOf.get(parent.id) || []), a]);
    } else {
      roots.push(a);
    }
  }
  const placed = new Set<string>();
  const node = (a: AgentDef): HierarchyNode => {
    placed.add(a.id);
    const kids = (childrenOf.get(a.id) || []).filter((c) => !placed.has(c.id)).sort(bySortThenName);
    return { agent: a, group: group.get(a.id) || '', children: kids.map(node) };
  };
  roots.sort(bySortThenName);
  const top = roots.filter((a) => !group.get(a.id)).map(node);
  const groups = new Map<string, HierarchyNode[]>();
  for (const a of roots) {
    const g = group.get(a.id) || '';
    if (!g) continue;
    groups.set(g, [...(groups.get(g) || []), node(a)]);
  }
  // Agents caught in a cycle of hand edits are nobody's child and nobody's
  // root; draw them at the top so none goes missing.
  for (const a of agents) if (!placed.has(a.id)) top.push(node(a));
  const ordered = [...groups.entries()]
    .map(([name, leads]) => ({ name, leads }))
    .sort((a, b) => {
      const ao = Math.min(...a.leads.map((l) => l.agent.sort_order ?? 0));
      const bo = Math.min(...b.leads.map((l) => l.agent.sort_order ?? 0));
      return ao - bo || a.name.localeCompare(b.name);
    });
  return { top, groups: ordered };
};

/** Every agent of a subtree, the node's own included. */
export const subtree = (n: HierarchyNode): AgentDef[] => [n.agent, ...n.children.flatMap(subtree)];

export interface Totals {
  open: number;
  done: number;
  cost: number;
  pending: number;
}

export const totalsOf = (agents: AgentDef[], stats: Record<string, AgentStats>): Totals =>
  agents.reduce(
    (t, a) => {
      const s = stats[a.id];
      if (!s) return t;
      return {
        open: t.open + s.open,
        done: t.done + s.done,
        cost: t.cost + s.cost_usd,
        pending: t.pending + s.pending_proposals,
      };
    },
    { open: 0, done: 0, cost: 0, pending: 0 }
  );

// The theme's accent tokens, each readable in light and dark mode. A group's
// colour is one of them, chosen from its name, so a group keeps its colour
// on every visit and for every member.
export const GROUP_COLORS = [
  'var(--accent)',
  'var(--success)',
  'var(--warning)',
  'var(--purple)',
  'var(--danger)',
  'var(--accent-alt)',
  'var(--success-bright)',
  'var(--warning-strong)',
];

/** A group's colour: deterministic from its name; the neutral for none. */
export const groupColor = (name: string): string => {
  if (!name) return 'var(--neutral)';
  let h = 0;
  for (const ch of name.trim().toLowerCase()) h = (h * 31 + ch.codePointAt(0)!) >>> 0;
  return GROUP_COLORS[h % GROUP_COLORS.length];
};

/** The agent's place, as PUT /agent-arrangement takes it. */
export const placementOf = (agent: AgentDef, byId: Map<string, AgentDef>) => ({
  slug: agent.slug,
  reports_to: (agent.parent_agent_id && byId.get(agent.parent_agent_id)?.slug) || '',
  group_name: agent.group_name || '',
  sort_order: agent.sort_order ?? 0,
});

/** The agents an agent may report to: any but itself and those below it. */
export const possibleParents = (agent: AgentDef, agents: AgentDef[]): AgentDef[] => {
  const below = new Set<string>([agent.id]);
  let grew = true;
  while (grew) {
    grew = false;
    for (const a of agents) {
      if (a.parent_agent_id && below.has(a.parent_agent_id) && !below.has(a.id)) {
        below.add(a.id);
        grew = true;
      }
    }
  }
  return agents.filter((a) => !below.has(a.id)).sort((a, b) => a.name.localeCompare(b.name));
};
