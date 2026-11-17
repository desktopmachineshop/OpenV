import { describe, expect, it } from 'vitest';
import type { AgentDef } from '../../api/client';
import {
  GROUP_COLORS,
  buildHierarchy,
  groupColor,
  placementOf,
  possibleParents,
  subtree,
  totalsOf,
} from './agentHierarchy';

const agent = (id: string, extra: Partial<AgentDef> = {}): AgentDef =>
  ({
    id,
    slug: id,
    name: id.toUpperCase(),
    description: '',
    provider: 'claude-code',
    model: '',
    effort: '',
    allowed_tools: ['mcp__openv__*'],
    write_mode: 'proposal',
    repo_access: false,
    max_turns: 50,
    timeout_seconds: 600,
    system_prompt: '',
    file_path: '',
    ...extra,
  }) as AgentDef;

const names = (nodes: { agent: AgentDef }[]) => nodes.map((n) => n.agent.id);

// The seeded arrangement: a lead at the top, three groups beneath.
const seeded = [
  agent('chief'),
  agent('analyst', { parent_agent_id: 'chief', group_name: 'Requirements' }),
  agent('interviewer', { parent_agent_id: 'analyst', group_name: 'Requirements' }),
  agent('vv', { parent_agent_id: 'chief', group_name: 'Verification', sort_order: 1 }),
  agent('author', { parent_agent_id: 'vv', group_name: 'Verification' }),
  agent('assistant', { parent_agent_id: 'vv', group_name: 'Verification', sort_order: 1 }),
  agent('dev', { parent_agent_id: 'chief', group_name: 'Engineering', sort_order: 2 }),
  agent('reviewer', { parent_agent_id: 'dev' }),
];

describe('buildHierarchy', () => {
  it('puts the ungrouped lead on top and one column per group, leads with their reports nested', () => {
    const tree = buildHierarchy(seeded);
    expect(names(tree.top)).toEqual(['chief']);
    expect(tree.top[0].children).toEqual([]);
    expect(tree.groups.map((g) => g.name)).toEqual(['Requirements', 'Verification', 'Engineering']);
    const verification = tree.groups[1];
    expect(names(verification.leads)).toEqual(['vv']);
    expect(names(verification.leads[0].children)).toEqual(['author', 'assistant']);
  });

  it('draws an agent with no group in its lead’s group, nested beneath it', () => {
    const engineering = buildHierarchy(seeded).groups[2];
    expect(names(engineering.leads)).toEqual(['dev']);
    expect(engineering.leads[0].children[0]).toMatchObject({ group: 'Engineering' });
    expect(names(engineering.leads[0].children)).toEqual(['reviewer']);
  });

  it('orders siblings by sort order, then name', () => {
    const tree = buildHierarchy([
      agent('lead'),
      agent('b', { parent_agent_id: 'lead', sort_order: 0 }),
      agent('a', { parent_agent_id: 'lead', sort_order: 1 }),
      agent('c', { parent_agent_id: 'lead', sort_order: 0 }),
    ]);
    expect(names(tree.top[0].children)).toEqual(['b', 'c', 'a']);
  });

  it('loses no agent: a parent that is gone, or a cycle of hand edits, puts it on top', () => {
    const tree = buildHierarchy([
      agent('orphan', { parent_agent_id: 'gone' }),
      agent('x', { parent_agent_id: 'y' }),
      agent('y', { parent_agent_id: 'x' }),
    ]);
    expect(tree.top.flatMap(subtree).map((a) => a.id).sort()).toEqual(['orphan', 'x', 'y']);
  });
});

describe('totals and colours', () => {
  it('sums a group’s numbers', () => {
    const stats = {
      vv: { agent_id: 'vv', open: 1, done: 2, cost_usd: 0.5, pending_proposals: 1 },
      author: { agent_id: 'author', open: 0, done: 3, cost_usd: 1.25, pending_proposals: 4 },
    };
    const verification = buildHierarchy(seeded).groups[1];
    expect(totalsOf(verification.leads.flatMap(subtree), stats)).toEqual({ open: 1, done: 5, cost: 1.75, pending: 5 });
  });

  it('gives a group the same theme colour every time, from its name', () => {
    expect(groupColor('Verification')).toBe(groupColor(' verification '));
    expect(GROUP_COLORS).toContain(groupColor('Verification'));
    expect(groupColor('')).toBe('var(--neutral)');
  });
});

describe('arranging', () => {
  it('offers as a parent any agent but the agent and those below it', () => {
    expect(possibleParents(seeded[3], seeded).map((a) => a.id)).not.toContain('author');
    expect(possibleParents(seeded[3], seeded).map((a) => a.id)).not.toContain('vv');
    expect(possibleParents(seeded[0], seeded).map((a) => a.id)).toEqual([]);
  });

  it('writes a place with the parent’s slug', () => {
    const byId = new Map(seeded.map((a) => [a.id, a]));
    expect(placementOf(seeded[4], byId)).toEqual({ slug: 'author', reports_to: 'vv', group_name: 'Verification', sort_order: 0 });
  });
});
