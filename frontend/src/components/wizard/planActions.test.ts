import { mockApi } from '../../test/mockApi';
import { planAPI, todosAPI, workItemsAPI } from '../../api/client';
import { applySuggestionsToProject } from './applySuggestion';
import { planProjectAction, projectActionLabels, projectActionSummary } from './projectActions';

vi.mock('../../api/client', async (orig) => mockApi(await orig()));

// The V&V Assistant's plan cards: what each means, what it says on its
// face, and the calls applying it makes.

const ok = (s: Record<string, any>) => {
  const plan = planProjectAction(s as any);
  if (plan.outcome !== 'action') throw new Error(`refused: ${(plan as any).reason}`);
  return plan.action;
};
const refused = (s: Record<string, any>) => {
  const plan = planProjectAction(s as any);
  return plan.outcome === 'refused' ? plan.reason : '';
};

const plan = {
  project_id: 'p1',
  tasks: [
    { id: 'w1', wbs: '1', title: 'Design', column: 'todo', todos: [{ id: 'td1', text: 'Draw the bracket', done: false }] },
    { id: 'w2', wbs: '2', title: 'Build', column: 'todo', todos: [] },
    { id: 'w3', wbs: '2.1', title: 'Build', column: 'todo', todos: [] },
  ],
  milestones: [
    { key: 'm1', id: 'm1', kind: 'custom', name: 'Design review', release_id: null },
    { key: 'release:r1', id: '', kind: 'release', name: 'MVP', release_id: 'r1' },
  ],
  metric_catalogue: [{ key: 'tasks_done' }, { key: 'requirements_approved' }],
};

const apply = (suggestions: Record<string, any>[]) =>
  applySuggestionsToProject(
    { projectId: 'p1', artifacts: [] },
    suggestions.map((s, i) => ({ suggestion: s as any, key: `k${i}` })),
  );

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(planAPI.get).mockResolvedValue({ data: plan } as any);
  vi.mocked(todosAPI.listProject).mockResolvedValue({ data: [] } as any);
  vi.mocked(workItemsAPI.create).mockResolvedValue({ data: { id: 'new1' } } as any);
  vi.mocked(workItemsAPI.get).mockResolvedValue({ data: { item: { description: 'd', assignee_type: 'user', assignee_id: null, artifact_ids: [], due_date: null } } } as any);
  for (const fn of [planAPI.schedule, planAPI.createMilestone, planAPI.updateMilestone, planAPI.setReleaseCriteria, planAPI.addTodo, planAPI.recalculate, planAPI.level, planAPI.saveBaseline, planAPI.rescheduleRemaining, workItemsAPI.update, workItemsAPI.move, todosAPI.setDone]) {
    vi.mocked(fn as any).mockResolvedValue({ data: {} } as any);
  }
});

describe('plan cards', () => {
  it('reads a task card with its place on the plan', () => {
    expect(ok({ kind: 'task', title: 'Weld', start: '2027-03-01', duration_days: 5, parent: 'Build', milestone: 'MVP', after: [{ task: 'Design', type: 'ss', lag_days: 2 }, 'Cut'] })).toEqual({
      kind: 'task',
      title: 'Weld',
      description: '',
      refs: [],
      plan: { start: '2027-03-01', duration_days: 5, parent: 'Build', milestone: 'MVP', after: [{ task: 'Design', type: 'SS', lag_days: 2 }, { task: 'Cut', type: 'FS', lag_days: 0 }] },
    });
    expect(refused({ kind: 'task', title: 'Weld', start: 'Monday' })).toMatch(/YYYY-MM-DD/);
    expect(refused({ kind: 'task', title: 'Weld', after: [{ task: 'Design', type: 'XX' }] })).toMatch(/FS, SS, FF or SF/);
  });

  it('reads the changes to a task, a milestone, a to-do and the plan', () => {
    expect(ok({ kind: 'plan_task', task: 'Design', due: '', percent_complete: 40, column: 'in-progress', parent: '' })).toEqual({
      kind: 'plan_task',
      task: 'Design',
      column: 'in-progress',
      schedule: { due: '', percent_complete: 40, parent: '' },
    });
    expect(refused({ kind: 'plan_task', task: 'Design' })).toMatch(/changes nothing/);
    expect(refused({ kind: 'plan_task', task: 'Design', percent_complete: 140 })).toMatch(/percentage complete/);
    expect(refused({ kind: 'plan_task', task: 'Design', column: 'doing' })).toMatch(/board column/);
    expect(ok({ kind: 'milestone', name: 'Gate', date: '2027-04-01', criteria: [{ metric: 'tasks_done' }] })).toEqual({
      kind: 'milestone',
      fields: { name: 'Gate', date: '2027-04-01', criteria: [{ metric: 'tasks_done', target: 100 }] },
    });
    expect(refused({ kind: 'milestone' })).toMatch(/needs a name/);
    expect(refused({ kind: 'milestone', name: 'Gate', criteria: [{ metric: 'tasks_done', target: 0 }] })).toMatch(/1 to 100/);
    expect(ok({ kind: 'task_todo', task: 'Design', text: 'Check clearances', due: '2027-03-05' })).toEqual({ kind: 'task_todo', task: 'Design', text: 'Check clearances', due: '2027-03-05' });
    expect(ok({ kind: 'todo_done', text: 'Draw the bracket' })).toEqual({ kind: 'todo_done', text: 'Draw the bracket', done: true });
    expect(ok({ kind: 'plan', action: 'baseline', name: 'As agreed' })).toEqual({ kind: 'plan', action: 'baseline', name: 'As agreed' });
    expect(refused({ kind: 'plan', action: 'delete' })).toMatch(/recalculate, level, baseline, reschedule_remaining/);
  });

  it('says what each card will do', () => {
    expect(projectActionSummary({ kind: 'plan_task', task: 'Design', start: '2027-03-01', after: [{ task: 'Cut', lag_days: 2 }] })).toEqual({ title: 'Design', detail: 'starts 2027-03-01 · after Cut +2d' });
    expect(projectActionSummary({ kind: 'milestone', name: 'Gate', date: '2027-04-01' })).toEqual({ title: 'New milestone: Gate', detail: 'on 2027-04-01' });
    expect(projectActionLabels({ kind: 'milestone', milestone: 'Gate', date: '' }).button).toBe('Change milestone');
    expect(projectActionLabels({ kind: 'plan', action: 'recalculate' })).toEqual({ button: 'Recalculate', done: '✓ Recalculated' });
    expect(projectActionLabels({ kind: 'todo_done', text: 'x', done: false }).button).toBe('Reopen');
  });

  it('adds a task and places it, naming tasks and milestones as a person does', async () => {
    expect(await apply([{ kind: 'task', title: 'Weld', start: '2027-03-01', duration_days: 5, parent: '1', milestone: 'mvp', after: [{ task: 'design', type: 'SS', lag_days: 2 }] }])).toEqual([null]);
    expect(planAPI.schedule).toHaveBeenCalledWith('new1', {
      start_date: '2027-03-01',
      duration_days: 5,
      parent_id: 'w1',
      milestone: 'release:r1',
      relationships: [{ task_id: 'w1', type: 'SS', lag_days: 2, lag_unit: 'd' }],
    });
  });

  it('changes a task: its title, its schedule and its column', async () => {
    expect(await apply([{ kind: 'plan_task', task: 'Design', title: 'Detailed design', due: '2027-03-12', column: 'in-progress', milestone: '' }])).toEqual([null]);
    expect(workItemsAPI.update).toHaveBeenCalledWith('w1', expect.objectContaining({ title: 'Detailed design', description: 'd' }));
    expect(planAPI.schedule).toHaveBeenCalledWith('w1', { due_date: '2027-03-12', milestone: '' });
    expect(workItemsAPI.move).toHaveBeenCalledWith('w1', 'in-progress', 0);
    const [twice, missing] = await apply([{ kind: 'plan_task', task: 'Build', start: '2027-03-01' }, { kind: 'plan_task', task: 'Paint', start: '2027-03-01' }]);
    expect(twice).toMatch(/2 tasks are called Build.*2 or 2\.1/);
    expect(missing).toMatch(/no task Paint/);
  });

  it('adds and changes milestones, to-dos and the plan', async () => {
    const reasons = await apply([
      { kind: 'milestone', name: 'Gate', date: '2027-04-01', criteria: [{ metric: 'tasks_done', target: 90 }] },
      { kind: 'milestone', milestone: 'design review', date: '' },
      { kind: 'milestone', milestone: 'MVP', criteria: [{ metric: 'requirements_approved', target: 80 }] },
      { kind: 'milestone', milestone: 'MVP', name: 'Launch' },
      { kind: 'milestone', name: 'Bad', criteria: [{ metric: 'vibes' }] },
      { kind: 'task_todo', task: 'Design', text: 'Check clearances', due: '2027-03-05' },
      { kind: 'todo_done', text: 'draw the bracket' },
      { kind: 'todo_done', text: 'Nothing like it' },
      { kind: 'plan', action: 'recalculate' },
      { kind: 'plan', action: 'baseline', name: 'As agreed' },
    ]);
    expect(planAPI.createMilestone).toHaveBeenCalledWith('p1', { name: 'Gate', target_date: '2027-04-01', criteria: [{ metric: 'tasks_done', target: 90 }] });
    expect(planAPI.updateMilestone).toHaveBeenCalledWith('p1', 'm1', { target_date: '' });
    expect(planAPI.setReleaseCriteria).toHaveBeenCalledWith('p1', 'r1', [{ metric: 'requirements_approved', target: 80 }]);
    expect(reasons[3]).toMatch(/release's milestone/);
    expect(reasons[4]).toMatch(/not a metric/);
    expect(planAPI.addTodo).toHaveBeenCalledWith('w1', 'Check clearances', '2027-03-05');
    expect(todosAPI.setDone).toHaveBeenCalledWith('td1', true);
    expect(reasons[7]).toMatch(/no to-do/);
    expect(planAPI.recalculate).toHaveBeenCalledWith('p1');
    expect(planAPI.saveBaseline).toHaveBeenCalledWith('p1', 'As agreed');
    expect(reasons.filter((r) => r === null)).toHaveLength(7);
  });
});
