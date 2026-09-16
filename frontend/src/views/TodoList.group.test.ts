import { ProjectMember, WorkItem } from '../api/client';

// The router is mocked here as it is in the other view tests: this suite
// exercises a pure function, so inert stand-ins keep the module graph small
// and the test independent of routing.
vi.mock('react-router-dom', () => ({
  Link: ({ to, children, ...rest }: any) =>
    require('react').createElement('a', { href: String(to), ...rest }, children),
  useParams: () => ({ projectId: 'p1' }),
  useSearchParams: () => [new URLSearchParams(''), vi.fn()],
}));

import { groupByAssignee } from './TodoList';

const item = (over: Partial<WorkItem>): WorkItem => ({
  id: 'w1',
  project_id: 'p1',
  title: 'Check the seal spec',
  description: '',
  column: 'todo',
  sort_order: 0,
  assignee_type: 'user',
  assignee_id: null,
  artifact_ids: [],
  created_at: '2026-09-01T00:00:00Z',
  updated_at: '2026-09-01T00:00:00Z',
  ...over,
});

const member = (id: string, name: string): ProjectMember => ({
  project_id: 'p1',
  user_id: id,
  role: 'editor',
  user_name: name,
  user_email: `${name.toLowerCase()}@example.com`,
});

const groupNamed = (groups: ReturnType<typeof groupByAssignee>, name: string) =>
  groups.find((g) => g.name === name);

describe('groupByAssignee', () => {
  it('gives every member a group, including those who owe nothing', () => {
    const groups = groupByAssignee([item({ id: 'a', assignee_id: 'u1' })], [
      member('u1', 'Dave'),
      member('u2', 'Priya'),
    ]);

    expect(groups.map((g) => g.name)).toEqual(['Dave', 'Priya']);
    expect(groupNamed(groups, 'Dave')!.items).toHaveLength(1);
    // The point of the page is who owes what; someone with a clean slate is
    // an answer to that question, not an omission.
    expect(groupNamed(groups, 'Priya')!.items).toHaveLength(0);
  });

  it('separates unassigned work from agents and teams', () => {
    const groups = groupByAssignee(
      [
        item({ id: 'a', assignee_id: null }),
        item({ id: 'b', assignee_type: 'agent', assignee_id: 'agent-1' }),
        item({ id: 'c', assignee_type: 'team', assignee_id: 'team-1' }),
      ],
      []
    );

    expect(groupNamed(groups, 'Unassigned')!.items.map((i) => i.id)).toEqual(['a']);
    expect(groupNamed(groups, 'Agents and teams')!.items.map((i) => i.id)).toEqual(['b', 'c']);
  });

  it('keeps work assigned to someone no longer in the project', () => {
    const groups = groupByAssignee([item({ id: 'a', assignee_id: 'gone' })], [member('u1', 'Dave')]);

    // Dropping it would quietly lose work; it shows under a group that says
    // what happened instead.
    expect(groupNamed(groups, 'Former member')!.items.map((i) => i.id)).toEqual(['a']);
  });

  it('sorts people above the catch-all groups', () => {
    const groups = groupByAssignee(
      [item({ id: 'a', assignee_id: null }), item({ id: 'b', assignee_id: 'u1' })],
      [member('u1', 'Zoe')]
    );

    // "Zoe" sorts after "Unassigned" alphabetically; rank must win.
    expect(groups.map((g) => g.name)).toEqual(['Zoe', 'Unassigned']);
  });

  it('puts due work first, soonest first', () => {
    const groups = groupByAssignee(
      [
        item({ id: 'later', assignee_id: 'u1', due_date: '2026-12-01T00:00:00Z' }),
        item({ id: 'undated', assignee_id: 'u1' }),
        item({ id: 'sooner', assignee_id: 'u1', due_date: '2026-10-01T00:00:00Z' }),
      ],
      [member('u1', 'Dave')]
    );

    expect(groupNamed(groups, 'Dave')!.items.map((i) => i.id)).toEqual([
      'sooner',
      'later',
      'undated',
    ]);
  });

  it('puts live work above the backlog, not in board order', () => {
    const groups = groupByAssignee(
      [
        item({ id: 'backlog', assignee_id: 'u1', column: 'backlog', title: 'A' }),
        item({ id: 'doing', assignee_id: 'u1', column: 'in-progress', title: 'Z' }),
      ],
      [member('u1', 'Dave')]
    );

    // The board reads Backlog first; a list of what someone owes reads the
    // other way round. The title sorting later must not matter either.
    expect(groupNamed(groups, 'Dave')!.items.map((i) => i.id)).toEqual(['doing', 'backlog']);
  });

  it('ignores a due date on finished work when ordering', () => {
    const groups = groupByAssignee(
      [
        item({ id: 'done', assignee_id: 'u1', column: 'done', due_date: '2026-01-01T00:00:00Z' }),
        item({ id: 'open', assignee_id: 'u1', column: 'todo', due_date: '2026-11-01T00:00:00Z' }),
      ],
      [member('u1', 'Dave')]
    );

    // A date that has passed on something already finished is not urgent.
    expect(groupNamed(groups, 'Dave')!.items.map((i) => i.id)).toEqual(['open', 'done']);
  });

  it('falls back to the email when a member has no name', () => {
    const groups = groupByAssignee([], [
      { project_id: 'p1', user_id: 'u1', role: 'viewer', user_email: 'new@example.com' },
    ]);

    expect(groups[0].name).toBe('new@example.com');
  });
});
