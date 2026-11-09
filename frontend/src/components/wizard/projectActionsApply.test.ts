import { mockApi } from '../../test/mockApi';
import {
  artifactAPI,
  baselineAPI,
  chatterAPI,
  linkAPI,
  membersAPI,
  releasesAPI,
  reviewAPI,
  todosAPI,
  vvAPI,
  workItemsAPI,
} from '../../api/client';
import { applySuggestionsToProject } from './applySuggestion';

vi.mock('../../api/client', async (orig) => mockApi(await orig()));

const art = (id: string, ref: string, attributes: Record<string, any> = {}) =>
  ({ id, ref, project_id: 'p1', type: 'requirement', title: ref, body: '', attributes } as any);

const releases = [
  { id: 'rel-mvp', name: 'MVP' },
  { id: 'rel-v2', name: 'V2.0' },
];

const apply = (suggestions: Record<string, any>[], artifacts = [art('a1', 'REQ-1', { priority: 'must' }), art('a2', 'REQ-2'), art('t1', 'TC-1')]) =>
  applySuggestionsToProject(
    { projectId: 'p1', artifacts },
    suggestions.map((s, i) => ({ suggestion: s as any, key: `k${i}` }))
  );

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(releasesAPI.list).mockResolvedValue({ data: releases } as any);
  vi.mocked(artifactAPI.update).mockImplementation(async (id: string, p: any) => ({ data: { ...art(id, id), ...p } }) as any);
  vi.mocked(membersAPI.list).mockResolvedValue({ data: [{ user_id: 'u-priya', user_name: 'Priya', user_email: 'priya@example.com' }] } as any);
  vi.mocked(vvAPI.createRun).mockResolvedValue({ data: { id: 'run-1' } } as any);
  // Every other write answers at once; the stubs otherwise never settle.
  const done = { data: {} } as any;
  for (const fn of [
    releasesAPI.create, releasesAPI.update, linkAPI.create, linkAPI.delete, linkAPI.confirm,
    artifactAPI.changeStatus, artifactAPI.delete, chatterAPI.create, todosAPI.createProject,
    workItemsAPI.create, baselineAPI.create, reviewAPI.startRound, vvAPI.upsertResult, vvAPI.updateRun,
  ]) {
    vi.mocked(fn as any).mockResolvedValue(done);
  }
});

describe('applying project-action cards', () => {
  it('assigns artifacts to releases by name, keeping their other attributes', async () => {
    expect(await apply([{ kind: 'target', refs: ['REQ-1', 'REQ-2'], release: 'mvp', until: 'V2.0' }])).toEqual([null]);
    expect(artifactAPI.update).toHaveBeenCalledWith('a1', {
      attributes: { priority: 'must', target_release: 'rel-mvp', target_release_until: 'rel-v2' },
    });
    expect(artifactAPI.update).toHaveBeenCalledWith('a2', { attributes: { target_release: 'rel-mvp', target_release_until: 'rel-v2' } });
  });

  it('clears targeting, and refuses an unknown release before writing anything', async () => {
    await apply([{ kind: 'target', ref: 'REQ-1', clear: true }], [art('a1', 'REQ-1', { target_release: 'rel-mvp', x: 1 })]);
    expect(artifactAPI.update).toHaveBeenCalledWith('a1', { attributes: { x: 1 } });
    vi.mocked(artifactAPI.update).mockClear();
    const [reason] = await apply([{ kind: 'target', refs: ['REQ-1'], release: 'V9' }]);
    expect(reason).toMatch(/no release V9.*MVP, V2.0/);
    expect(artifactAPI.update).not.toHaveBeenCalled();
  });

  it('adds a release, then targets it in the same batch', async () => {
    vi.mocked(releasesAPI.create).mockImplementation(async () => {
      vi.mocked(releasesAPI.list).mockResolvedValue({ data: [...releases, { id: 'rel-v3', name: 'V3' }] } as any);
      return { data: { id: 'rel-v3' } } as any;
    });
    expect(await apply([{ kind: 'release', name: 'V3', target_date: '2027-06-30' }, { kind: 'target', refs: ['REQ-2'], release: 'V3' }])).toEqual([null, null]);
    expect(releasesAPI.create).toHaveBeenCalledWith('p1', { name: 'V3', target_date: '2027-06-30' });
    expect(artifactAPI.update).toHaveBeenCalledWith('a2', { attributes: { target_release: 'rel-v3' } });
  });

  it('changes a release found by name', async () => {
    await apply([{ kind: 'release', release: 'MVP', status: 'released' }]);
    expect(releasesAPI.update).toHaveBeenCalledWith('p1', 'rel-mvp', { status: 'released' });
  });

  it('adds, removes and confirms links between artifacts named by reference', async () => {
    vi.mocked(linkAPI.listForArtifact).mockResolvedValue({
      data: [{ id: 'l1', from_id: 't1', to_id: 'a1', type: 'verifies' }, { id: 'l2', from_id: 't1', to_id: 'a2', type: 'verifies' }],
    } as any);
    expect(
      await apply([
        { kind: 'link', from: 'TC-1', to: 'REQ-2', type: 'verifies' },
        { kind: 'link', action: 'remove', from: 'tc-1', to: 'REQ-1' },
        { kind: 'link', action: 'confirm', from: 'TC-1', to: 'REQ-2', type: 'verifies' },
        { kind: 'link', action: 'remove', from: 'REQ-1', to: 'REQ-2' },
      ])
    ).toEqual([null, null, null, 'There is no link from REQ-1 to REQ-2.']);
    expect(linkAPI.create).toHaveBeenCalledWith({ from_id: 't1', to_id: 'a2', type: 'verifies' });
    expect(linkAPI.delete).toHaveBeenCalledWith('l1');
    expect(linkAPI.confirm).toHaveBeenCalledWith('l2');
  });

  it('changes status and deletes per artifact, naming the one that failed', async () => {
    vi.mocked(artifactAPI.changeStatus).mockImplementation(async (id: string) => {
      if (id === 'a2') throw new Error('illegal transition');
      return { data: {} } as any;
    });
    const [reason] = await apply([{ kind: 'status', refs: ['REQ-1', 'REQ-2'], status: 'approved' }]);
    expect(reason).toMatch(/Applied to 1 of 2\. REQ-2:/);
    expect(await apply([{ kind: 'delete', refs: ['REQ-1'] }, { kind: 'delete', refs: ['REQ-404'] }])).toEqual([
      null,
      'There is no artifact REQ-404 in this project.',
    ]);
    expect(artifactAPI.delete).toHaveBeenCalledWith('a1');
  });

  it('posts a comment, raises a to-do for a member and adds a board task', async () => {
    await apply([
      { kind: 'comment', ref: 'REQ-1', message: 'Which threshold?' },
      { kind: 'todo', text: 'Check it', assignee: 'priya@example.com', due: '2027-02-01', ref: 'REQ-1' },
      { kind: 'task', title: 'Build it', refs: ['REQ-1', 'REQ-2'], assignee: 'Priya' },
    ]);
    expect(chatterAPI.create).toHaveBeenCalledWith({ artifact_id: 'a1', message: 'Which threshold?' });
    expect(todosAPI.createProject).toHaveBeenCalledWith('p1', {
      text: 'Check it',
      assignee_id: 'u-priya',
      due_date: '2027-02-01T00:00:00Z',
      source_artifact_id: 'a1',
    });
    expect(workItemsAPI.create).toHaveBeenCalledWith('p1', {
      title: 'Build it',
      description: '',
      artifact_ids: ['a1', 'a2'],
      assignee_type: 'user',
      assignee_id: 'u-priya',
    });
    const [reason] = await apply([{ kind: 'todo', text: 'x', assignee: 'Nobody' }]);
    expect(reason).toMatch(/Nobody is not a member/);
  });

  it('captures a baseline, starts a review round and records a completed test run', async () => {
    await apply([
      { kind: 'baseline', name: 'Before review' },
      { kind: 'review', types: ['requirement'] },
      { kind: 'test_run', name: 'Bench', results: [{ ref: 'TC-1', status: 'fail', notes: 'Slow' }], complete: true },
    ]);
    expect(baselineAPI.create).toHaveBeenCalledWith('p1', 'Before review');
    expect(reviewAPI.startRound).toHaveBeenCalledWith('p1', ['requirement']);
    expect(vvAPI.createRun).toHaveBeenCalledWith('p1', { name: 'Bench' });
    expect(vvAPI.upsertResult).toHaveBeenCalledWith('run-1', { test_case_id: 't1', status: 'fail', notes: 'Slow' });
    expect(vvAPI.updateRun).toHaveBeenCalledWith('run-1', 'completed');
  });

  it('creates no test run when a result names an unknown test case', async () => {
    const [reason] = await apply([{ kind: 'test_run', name: 'Bench', results: [{ ref: 'TC-9', status: 'pass' }] }]);
    expect(reason).toMatch(/no artifact TC-9/);
    expect(vvAPI.createRun).not.toHaveBeenCalled();
  });
});
