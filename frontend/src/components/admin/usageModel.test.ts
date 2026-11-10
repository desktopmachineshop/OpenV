import { describe, expect, it } from 'vitest';
import type { AdminUsage, AdminUser, AdminWorkspace } from '../../api/client';
import { NO_FILTER, filterPeople, groupWorkspaces, joinPeople, membersOf, paceOf } from './usageModel';

const ws = (id: string, plan: string, type: 'company' | 'personal', members = 1): AdminWorkspace =>
  ({ id, name: id.toUpperCase(), slug: id, plan, type, members, created_at: '2026-01-01T00:00:00Z' }) as any;
const person = (id: string, name: string): AdminUser => ({ id, name, email: `${id}@example.com`, auth_provider: 'password', is_admin: false, created_at: '' });
const weeks = (...v: number[]) => [...Array(12 - v.length).fill(0), ...v];

const usage: AdminUsage = {
  generated_at: '',
  weeks: Array(12).fill('2026-11-10'),
  pace_weeks: 4,
  workspaces: [
    { id: 'acme', projects: 1, requirements: 10, test_cases: 4, other_artifacts: 0, links: 8, test_runs: 1, test_results: 4, comments: 2,
      members: 2, active_members_30d: 1, changes: weeks(2, 2, 2, 2, 5, 5, 5, 5), pace: { recent: 20, previous: 8, change: 1.5, trend: 'accelerating' } },
  ],
  users: [
    { id: 'ann', workspaces: [{ workspace_id: 'acme', role: 'owner' }], changes: weeks(1), active_days: weeks(1), last_active: '2026-11-10',
      pace: { recent: 1, previous: 0, change: 0, trend: 'new' } },
    { id: 'bob', workspaces: [{ workspace_id: 'acme', role: 'member' }, { workspace_id: 'bobs', role: 'owner' }], changes: weeks(0), active_days: weeks(0),
      pace: { recent: 0, previous: 0, change: 0, trend: 'idle' } },
  ],
};

describe('paceOf', () => {
  it('compares the last window with the one before, as the server does', () => {
    expect(paceOf(weeks(4, 4, 4, 4, 6, 6, 6, 6), 4)).toMatchObject({ recent: 24, previous: 16, change: 0.5, trend: 'accelerating' });
    expect(paceOf(weeks(4, 4, 4, 4, 3, 3, 3, 3), 4).trend).toBe('slowing');
    expect(paceOf(weeks(10, 10, 10, 10, 11, 10, 10, 10), 4).trend).toBe('steady');
    expect(paceOf(weeks(5, 0, 0, 0, 0), 4).trend).toBe('dormant');
    expect(paceOf(weeks(1), 4).trend).toBe('new');
    expect(paceOf(weeks(), 4).trend).toBe('idle');
  });
});

describe('groupWorkspaces', () => {
  it('groups by plan in plan order, company before personal, busiest first', () => {
    const groups = groupWorkspaces([ws('bobs', 'single', 'personal'), ws('quiet', 'business', 'company'), ws('acme', 'business', 'company', 2), ws('home', 'business', 'personal')], usage);
    expect(groups.map((g) => g.plan)).toEqual(['single', 'business']);
    const business = groups[1];
    expect(business.kinds.map((k) => k.kind)).toEqual(['company', 'personal']);
    expect(business.kinds[0].rows.map((r) => r.workspace.id)).toEqual(['acme', 'quiet']);
    expect(business.workspaces).toBe(3);
    expect(business.members).toBe(4);
    expect(business.pace).toMatchObject({ recent: 20, previous: 8, trend: 'accelerating' });
    // A workspace the server sent nothing for is idle with zero counts.
    expect(business.kinds[0].rows[1].usage).toMatchObject({ requirements: 0, pace: { trend: 'idle' } });
  });
});

describe('people', () => {
  const workspaces = [ws('acme', 'business', 'company'), ws('bobs', 'single', 'personal')];
  const people = joinPeople([person('bob', 'Bob'), person('ann', 'Ann'), person('cat', 'Cat')], workspaces, usage);

  it('joins each person to their workspaces and roles', () => {
    expect(people.find((p) => p.user.id === 'bob')?.workspaces.map((m) => [m.workspace.id, m.role])).toEqual([['acme', 'member'], ['bobs', 'owner']]);
    expect(people.find((p) => p.user.id === 'cat')?.usage.pace.trend).toBe('idle');
  });

  it('filters by plan, workspace, pace and search', () => {
    const ids = (f: Partial<typeof NO_FILTER>) => filterPeople(people, { ...NO_FILTER, ...f }).map((p) => p.user.id);
    expect(ids({})).toEqual(['ann', 'bob', 'cat']);
    expect(ids({ plan: 'single' })).toEqual(['bob']);
    expect(ids({ workspaceId: 'acme' })).toEqual(['ann', 'bob']);
    expect(ids({ trend: 'new' })).toEqual(['ann']);
    expect(ids({ search: 'CAT@' })).toEqual(['cat']);
  });

  it('lists a workspace’s members with their role there', () => {
    expect(membersOf(people, 'bobs').map((p) => [p.user.id, p.role])).toEqual([['bob', 'owner']]);
  });
});
