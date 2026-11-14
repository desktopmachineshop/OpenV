import { describe, expect, it } from 'vitest';
import type { PlanTask, ProjectPlan } from '../../api/client';
import { mergePlans, projectRowId } from './mergePlans';

const summary = (over: Partial<ProjectPlan['summary']> = {}): ProjectPlan['summary'] => ({ tasks: 0, done: 0, late: 0, unscheduled: 0, progress: 0, critical: 0, effort_days: 0, cost: 0, overallocated: 0, ...over });

const empty = (over: Partial<ProjectPlan> = {}): ProjectPlan =>
  ({ project_id: 'x', working_days: [1, 2, 3, 4, 5], holidays: [], scheduling_mode: 'manual', resources: [], people: [], today: '2027-03-10', start: null, end: null, tasks: [], milestones: [], dependencies: [], metrics: [], metric_catalogue: [], baselines: [], summary: summary(), ...over }) as unknown as ProjectPlan;

// A whole task, with every field: the row mergePlans makes for a project.
const whole = mergePlans([{ id: 't', name: 't', plan: empty() }])!.plan.tasks[0];
const task = (id: string, over: Partial<PlanTask> = {}): PlanTask => ({ ...whole, id, title: id, subtasks_total: 0, wbs: '1', span_start: '2027-03-01', span_end: '2027-03-05', ...over });
const person = (over: object = {}) => ({ type: 'user', id: 'u1', name: 'Priya', daily_rate: 0, capacity: 100, tasks: 1, work_days: 2, cost: 10, peak: 100, overallocations: [], time_off: [], ...over });

const rig = empty({
  start: '2027-03-01',
  end: '2027-03-20',
  tasks: [task('build', { subtasks_total: 1, done: true }), task('weld', { parent_id: 'build', depth: 1, wbs: '1.1' })],
  milestones: [{ key: 'm1', name: 'Beta', date: '2027-03-20', tasks: [] }] as unknown as ProjectPlan['milestones'],
  dependencies: [{ work_item_id: 'weld', depends_on_id: 'build', type: 'FS' }],
  resources: [person()] as unknown as ProjectPlan['resources'],
  holidays: [{ date: '2027-03-17', name: 'Fair' }] as unknown as ProjectPlan['holidays'],
  summary: summary({ tasks: 2, done: 1, progress: 50, critical: 1, cost: 10 }),
});
const pump = empty({
  start: '2027-02-01',
  end: '2027-03-10',
  tasks: [task('spec', { late: true })],
  milestones: [{ key: 'm2', name: 'Beta', date: null, tasks: [] }] as unknown as ProjectPlan['milestones'],
  resources: [person({ tasks: 2, work_days: 3, cost: 5, peak: 150 })] as unknown as ProjectPlan['resources'],
  summary: summary({ tasks: 2, late: 1, progress: 0, cost: 5 }),
});

describe('mergePlans', () => {
  it('has nothing to make of no plans', () => {
    expect(mergePlans([])).toBeNull();
  });

  it('puts each project on a row of its own, with its tasks a level down beneath it', () => {
    const { plan, projectOf } = mergePlans([{ id: 'p1', name: 'Rig', plan: rig }, { id: 'p2', name: 'Pump', plan: pump }])!;
    expect(plan.tasks.map((t) => [t.id, t.depth, t.wbs, t.parent_id])).toEqual([
      ['project:p1', 0, '1', null],
      ['build', 1, '1.1', 'project:p1'],
      ['weld', 2, '1.1.1', 'build'],
      ['project:p2', 0, '2', null],
      ['spec', 1, '2.1', 'project:p2'],
    ]);
    const row = plan.tasks[0];
    expect([row.title, row.span_start, row.span_end, row.progress, row.subtasks_total, row.subtasks_done, row.critical_subtasks]).toEqual(['Rig', '2027-03-01', '2027-03-20', 50, 1, 1, true]);
    expect(plan.tasks[3].late).toBe(true);
    expect(projectOf.get('weld')).toBe('p1');
    expect(projectOf.get(projectRowId('p2'))).toBe('p2');
  });

  it('spans every plan, adds their figures up, and names a milestone by its project', () => {
    const { plan } = mergePlans([{ id: 'p1', name: 'Rig', plan: rig }, { id: 'p2', name: 'Pump', plan: pump }])!;
    expect([plan.start, plan.end]).toEqual(['2027-02-01', '2027-03-20']);
    expect(plan.summary).toMatchObject({ tasks: 4, done: 1, late: 1, critical: 1, cost: 15, progress: 25 });
    expect(plan.milestones.map((m) => m.name)).toEqual(['Rig: Beta', 'Pump: Beta']);
    expect(plan.dependencies).toHaveLength(1);
    expect(plan.holidays).toHaveLength(1);
    expect(plan.baselines).toEqual([]);
  });

  it('counts one person once, with the work of every project', () => {
    const { plan } = mergePlans([{ id: 'p1', name: 'Rig', plan: rig }, { id: 'p2', name: 'Pump', plan: pump }])!;
    expect(plan.resources).toHaveLength(1);
    expect(plan.resources[0]).toMatchObject({ tasks: 3, work_days: 5, cost: 15, peak: 150 });
    // The plans it was made from are left as they were.
    expect(rig.resources[0].tasks).toBe(1);
  });

  it('keeps a lone project’s milestone names as they are', () => {
    expect(mergePlans([{ id: 'p1', name: 'Rig', plan: rig }])!.plan.milestones[0].name).toBe('Beta');
  });
});
