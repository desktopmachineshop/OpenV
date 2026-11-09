import { planProjectAction, projectActionLabels, projectActionSummary } from './projectActions';

const ok = (s: Record<string, any>) => {
  const plan = planProjectAction(s as any);
  if (plan.outcome !== 'action') throw new Error(`refused: ${(plan as any).reason}`);
  return plan.action;
};
const refused = (s: Record<string, any>) => {
  const plan = planProjectAction(s as any);
  return plan.outcome === 'refused' ? plan.reason : '';
};

describe('planProjectAction', () => {
  it('adds a release, or changes one by name with only the fields given', () => {
    expect(ok({ kind: 'release', name: 'MVP', target_date: '2027-01-31' })).toEqual({
      kind: 'release',
      fields: { name: 'MVP', target_date: '2027-01-31' },
    });
    expect(ok({ kind: 'release', release: 'MVP', status: 'in_progress', target_date: '' })).toEqual({
      kind: 'release',
      release: 'MVP',
      fields: { status: 'in_progress', target_date: '' },
    });
    expect(refused({ kind: 'release' })).toMatch(/needs a name/);
    expect(refused({ kind: 'release', name: 'V1', target_date: 'next spring' })).toMatch(/YYYY-MM-DD/);
    expect(refused({ kind: 'release', name: 'V1', status: 'shipped' })).toMatch(/release status/);
    expect(refused({ kind: 'release', release: 'MVP' })).toMatch(/changes nothing/);
  });

  it('sets release targets for several artifacts, or clears them', () => {
    expect(ok({ kind: 'target', refs: ['REQ-1', 'REQ-2'], release: 'MVP', until: 'V2' })).toEqual({
      kind: 'target',
      refs: ['REQ-1', 'REQ-2'],
      clear: false,
      release: 'MVP',
      until: 'V2',
    });
    expect(ok({ kind: 'target', ref: 'REQ-1', clear: true })).toEqual({ kind: 'target', refs: ['REQ-1'], clear: true });
    expect(refused({ kind: 'target', refs: ['REQ-1'] })).toMatch(/names no release/);
    expect(refused({ kind: 'target', release: 'MVP' })).toMatch(/names no artifact/);
  });

  it('adds, removes and confirms links, with a known type', () => {
    expect(ok({ kind: 'link', from: 'TC-1', to: 'REQ-1', type: 'verifies' })).toEqual({
      kind: 'link',
      action: 'add',
      from: 'TC-1',
      to: 'REQ-1',
      type: 'verifies',
    });
    expect(ok({ kind: 'link', action: 'remove', from: 'TC-1', to: 'REQ-1' }).kind).toBe('link');
    expect(refused({ kind: 'link', from: 'TC-1', to: 'REQ-1' })).toMatch(/no type/);
    expect(refused({ kind: 'link', from: 'TC-1', to: 'REQ-1', type: 'blesses' })).toMatch(/not a link type/);
    expect(refused({ kind: 'link', action: 'explode', from: 'A', to: 'B' })).toMatch(/add, remove or confirm/);
  });

  it('checks statuses, results and dates against the vocabularies', () => {
    expect(ok({ kind: 'status', refs: ['REQ-1'], status: 'approved' })).toEqual({ kind: 'status', refs: ['REQ-1'], status: 'approved' });
    expect(refused({ kind: 'status', refs: ['REQ-1'], status: 'done' })).toMatch(/review status/);
    expect(refused({ kind: 'todo', text: 'x', due: 'Friday' })).toMatch(/YYYY-MM-DD/);
    expect(refused({ kind: 'test_run', name: 'R', results: [{ ref: 'TC-1', status: 'green' }] })).toMatch(/pass, fail/);
    const run = ok({ kind: 'test_run', name: 'R', results: [{ ref: 'TC-1', status: 'pass' }], complete: true });
    expect(run).toEqual({ kind: 'test_run', name: 'R', description: '', results: [{ ref: 'TC-1', status: 'pass', notes: '' }], complete: true });
  });

  it('plans the rest: delete, comment, to-do, task, baseline, review', () => {
    expect(ok({ kind: 'delete', refs: ['REQ-9'] })).toEqual({ kind: 'delete', refs: ['REQ-9'] });
    expect(ok({ kind: 'comment', ref: 'REQ-1', message: 'Which threshold?' })).toEqual({ kind: 'comment', ref: 'REQ-1', message: 'Which threshold?' });
    expect(refused({ kind: 'comment', ref: 'REQ-1', message: ' ' })).toMatch(/empty/);
    expect(ok({ kind: 'todo', text: 'Check limit', assignee: 'Priya', due: '2027-02-01', ref: 'REQ-1' })).toEqual({
      kind: 'todo',
      text: 'Check limit',
      assignee: 'Priya',
      due: '2027-02-01',
      ref: 'REQ-1',
    });
    expect(ok({ kind: 'task', title: 'Build it', refs: ['REQ-1'] })).toEqual({ kind: 'task', title: 'Build it', description: '', refs: ['REQ-1'] });
    expect(ok({ kind: 'baseline', name: 'Before review' })).toEqual({ kind: 'baseline', name: 'Before review' });
    expect(ok({ kind: 'review' })).toEqual({ kind: 'review' });
    expect(ok({ kind: 'review', types: ['requirement'] })).toEqual({ kind: 'review', types: ['requirement'] });
  });
});

describe('the card face', () => {
  it('says what the card will do', () => {
    expect(projectActionSummary({ kind: 'target', refs: ['REQ-1'], release: 'MVP' })).toEqual({ title: 'REQ-1', detail: 'In MVP onwards' });
    expect(projectActionSummary({ kind: 'link', from: 'TC-1', to: 'REQ-1', type: 'verifies', action: 'remove' }).detail).toBe('Remove this link');
    expect(projectActionLabels({ kind: 'release', release: 'MVP', status: 'released' }).button).toBe('Change release');
    expect(projectActionLabels({ kind: 'release', name: 'MVP' }).button).toBe('+ Add release');
    expect(projectActionLabels({ kind: 'delete', refs: ['REQ-1'] }).button).toBe('Delete');
  });
});
