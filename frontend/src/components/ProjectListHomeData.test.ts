import type { CoverageReport } from '../api/client';
import { buildTodo, coverageFigures, percent, todoNeeds } from './ProjectListHomeData';

// #720: the coverage report's summary counts requirements by rollup
// (internal/domain/vv/coverage.go: Summary map[string]int, rollup -> count).
// Read as {total, covered, verified}, every project said "No requirements to
// trace yet". This is the shape GET /projects/:id/vv/coverage answers with.
const REAL: CoverageReport = {
  project_id: 'p1',
  entries: [
    { requirement_id: 'r1', title: 'A', verification_method: 'test', verification_status: '', test_case_ids: ['t1'], latest_results: { t1: 'pass' }, rollup: 'pass' },
    { requirement_id: 'r2', title: 'B', verification_method: 'test', verification_status: '', test_case_ids: ['t2'], latest_results: { t2: 'fail' }, rollup: 'fail' },
    { requirement_id: 'r3', title: 'C', verification_method: 'test', verification_status: '', test_case_ids: ['t3'], latest_results: { t3: 'not-run' }, rollup: 'unrun' },
    { requirement_id: 'r4', title: 'D', verification_method: 'inspection', verification_status: 'verified', test_case_ids: [], latest_results: {}, rollup: 'verified-manually' },
    { requirement_id: 'r5', title: 'E', verification_method: 'test', verification_status: '', test_case_ids: [], latest_results: {}, rollup: 'uncovered' },
    { requirement_id: 'r6', title: 'F', verification_method: '', verification_status: '', test_case_ids: [], latest_results: {}, rollup: 'method-missing' },
    { requirement_id: 'r7', title: 'G', verification_method: 'test', verification_status: '', test_case_ids: ['t4'], latest_results: { t4: 'blocked' }, rollup: 'blocked' },
    { requirement_id: 'r8', title: 'H', verification_method: 'test', verification_status: '', test_case_ids: ['t1'], latest_results: { t1: 'pass' }, rollup: 'pass' },
  ],
  summary: { pass: 2, fail: 1, unrun: 1, 'verified-manually': 1, uncovered: 1, 'method-missing': 1, blocked: 1 },
};

describe('coverageFigures', () => {
  it('reads the real summary, rollup by rollup', () => {
    const f = coverageFigures(REAL)!;
    expect(f).toEqual({ total: 8, traced: 6, verified: 3, failing: 1 });
    expect(percent(f.traced, f.total)).toBe(75);
    expect(percent(f.verified, f.total)).toBe(38);
  });

  it('counts the entries when a summary is missing', () => {
    expect(coverageFigures({ entries: REAL.entries, summary: {} })).toEqual({ total: 8, traced: 6, verified: 3, failing: 1 });
  });

  it('says a project without requirements has none, so the meters say so', () => {
    const f = coverageFigures({ entries: [], summary: {} })!;
    expect(f.total).toBe(0);
    expect(percent(f.traced, f.total)).toBeNull();
    expect(coverageFigures(null)).toBeNull();
  });
});

describe('todoNeeds', () => {
  const base = {
    org_id: 'o', done_at: null, done_note: '', assignee_id: 'me', source_artifact_id: null,
    source_chatter_id: null, promoted_work_item_id: null, created_by: 'me',
    created_at: '2026-11-01T00:00:00Z', updated_at: '2026-11-01T00:00:00Z',
  };
  it('lists my open to-dos: a project one opens its list, a personal one My to-dos', () => {
    const rows = todoNeeds(
      [
        { ...base, id: 'a', project_id: 'p', text: 'Ring Sam', done: false, due_date: '2026-01-01T00:00:00Z' },
        { ...base, id: 'b', project_id: null, text: 'Book the rig', done: false, due_date: null },
        { ...base, id: 'c', project_id: 'p', text: 'Done', done: true, due_date: null },
        { ...base, id: 'd', project_id: 'gone', text: 'Elsewhere', done: false, due_date: null },
      ],
      new Map([['p', 'Seal rig']]),
      new Date('2026-06-01T00:00:00Z'),
    );
    expect(rows.map((r) => [r.kind, r.title, r.project, r.to, r.meta])).toEqual([
      ['to-do', 'Ring Sam', 'Seal rig', '/projects/p/todos?todo=a', 'Overdue · due 2026-01-01'],
      ['to-do', 'Book the rig', 'Personal', '/org/todos?todo=b', 'To-do'],
    ]);
  });
});

describe('buildTodo comments', () => {
  const event = {
    id: 'e1',
    event_type: 'chatter.created',
    actor: 'user:u2',
    actor_name: 'Priya',
    payload: { artifact_id: 'a1', entry_type: 'comment', message: 'Which threshold?' },
    entity_name: 'REQ-1 Answer in time',
    created_at: '2026-11-09T10:00:00Z',
  };
  const sources = { projectId: 'p1', review: null, runs: [], testCases: 0, coverage: null, comments: [event], byId: new Map() };

  it('gives a comment row what Mark read needs, with read state on', () => {
    const [row] = buildTodo({ ...sources, readState: true });
    expect(row.comment).toEqual({ artifactId: 'a1', at: '2026-11-09T10:00:00Z' });
  });

  it('leaves it out without read state', () => {
    expect(buildTodo(sources)[0].comment).toBeUndefined();
  });

  it('drops a comment whose artifact has been deleted: nothing to open or read', () => {
    const gone = { ...event, id: 'e2', entity_name: '', payload: { ...event.payload, artifact_id: 'deleted' } };
    const known = { ...event, id: 'e3', entity_name: '', payload: { ...event.payload, artifact_id: 'a7' } };
    const byId = new Map([['a7', { id: 'a7', ref: 'REQ-7', title: 'Seven' } as any]]);
    const rows = buildTodo({ ...sources, comments: [event, gone, known], byId });
    expect(rows.map((r) => r.key)).toEqual(['comment-e1', 'comment-e3']);
  });
});
