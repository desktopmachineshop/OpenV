import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { mockApi } from '../../test/mockApi';
import { CardPlan } from './CardPlan';
import { planAPI } from '../../api/client';

// The plan's side of a card opened in full on the board: the Gantt's task
// panel for that card, which writes to the plan and tells the board.

const ok = (data: unknown) => Promise.resolve({ data });

const task = (id: string, title: string, extra: Record<string, unknown> = {}) => ({
  id, title, column: 'todo', assignee_type: 'user', assignee_id: null, parent_id: null, depth: 0, wbs: '1', order: 1,
  start: null, end: null, span_start: null, span_end: null, duration_days: null, effort_days: null, float_days: null,
  critical: false, critical_subtasks: false, float_until: null, elapsed: false, deadline: null, deadline_missed: false,
  mode: 'manual', mode_own: '', constraint_type: 'asap', constraint_date: null, constraint_violated: false,
  early_start: null, early_finish: null, late_start: null, late_finish: null, free_float_days: null, assignments: [],
  fixed_cost: 0, cost: 0, effort_driven: false, overallocated: false, actual_start: null, actual_finish: null,
  percent_complete: null, actual_cost: null, baseline: null, start_variance_days: null, finish_variance_days: null,
  earned_value: null, behind_schedule: false, progress: 0, done: false, late: false, todos_done: 0, todos_total: 0,
  subtasks_done: 0, subtasks_total: 0, milestone: null, milestone_inherited: false, todos: [], depends_on: [],
  relationships: [], drives: [], conflicts: [], external_links: [],
  ...extra,
});

const plan = {
  project_id: 'p1', working_days: [1, 2, 3, 4, 5, 6, 7], holidays: [], critical_slack_days: 0, scheduling_mode: 'manual',
  project_start: null, project_finish: null, schedule_from: 'start', multiple_critical_paths: false,
  effort_driven_default: false, currency: '', budget: null, status_date: null, active_baseline_id: null,
  status_day: '2027-03-10', baseline_id: null, baselines: [],
  earned_value: { bac: 0, pv: 0, ev: 0, ac: 0, sv: 0, cv: 0, spi: 0, cpi: 0, eac: 0 },
  resources: [], people: [], today: '2027-03-10', start: '2027-03-01', end: '2027-04-01',
  tasks: [
    task('build', 'Build the rig', { start: '2027-03-01', end: '2027-03-05', span_start: '2027-03-01', span_end: '2027-03-05', duration_days: 5 }),
    task('loose', 'Packaging concept', { wbs: '2' }),
  ],
  milestones: [], dependencies: [], metrics: [], metric_catalogue: [],
  summary: { tasks: 2, done: 0, late: 0, unscheduled: 1, progress: 0, critical: 0, effort_days: 0, cost: 0, overallocated: 0 },
};

vi.mock('../../api/client', async (orig) =>
  mockApi(await orig(), {
    planAPI: { get: vi.fn(() => ok(plan)), schedule: vi.fn(() => ok({})) },
  })
);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;
const onChanged = vi.fn();

const mount = async (workItemId: string, version = 0) => {
  await act(async () => {
    root.render(
      <MemoryRouter>
        <CardPlan projectId="p1" workItemId={workItemId} version={version} onChanged={onChanged} onSelectTask={() => undefined} onClose={() => undefined} />
      </MemoryRouter>
    );
  });
};

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

describe('CardPlan', () => {
  it("shows the card's task of the plan, with its schedule and a way to the Gantt", async () => {
    await mount('build');
    const panel = container.querySelector('aside[aria-label="Task Build the rig"]')!;
    expect(panel).toBeTruthy();
    expect((panel.querySelector('input[type="date"]') as HTMLInputElement).value).toBe('2027-03-01');
    expect(panel.querySelector('a[href="/projects/p1/gantt"]')?.textContent).toBe('Open the Gantt');
  });

  it('saves a new schedule to the plan and tells the board', async () => {
    await mount('build');
    const start = container.querySelector('input[type="date"]') as HTMLInputElement;
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(start, '2027-03-02');
      start.dispatchEvent(new Event('input', { bubbles: true }));
    });
    const save = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === 'Save schedule')!;
    await act(async () => save.click());
    expect(planAPI.schedule).toHaveBeenCalledWith('build', expect.objectContaining({ start_date: '2027-03-02', due_date: '2027-03-06' }));
    expect(onChanged).toHaveBeenCalled();
  });

  it('reads the plan again when the card changes', async () => {
    await mount('build');
    await mount('build', 1);
    expect(planAPI.get).toHaveBeenCalledTimes(2);
  });
});
