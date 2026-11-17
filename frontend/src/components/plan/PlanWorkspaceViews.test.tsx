import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { mockApi } from '../../test/mockApi';
import type { PlanTask, ProjectPlan } from '../../api/client';
import { mergePlans } from './mergePlans';
import { PlanWorkspaceViews, type WorkspaceView } from './PlanWorkspaceViews';

// The workspace's Plan page draws every project with the views of a
// project's Gantt: the same components, given the plans merged into one.

const ok = (data: unknown) => Promise.resolve({ data });
const base = { project_id: 'x', working_days: [1, 2, 3, 4, 5, 6, 7], holidays: [], scheduling_mode: 'manual', critical_slack_days: 0, resources: [], people: [], today: '2027-03-10', start: '2027-03-01', end: '2027-03-20', milestones: [], dependencies: [], metrics: [], metric_catalogue: [], baselines: [], summary: { tasks: 1, done: 0, late: 0, unscheduled: 0, progress: 0, critical: 0, effort_days: 0, cost: 0, overallocated: 0 } };
const whole = mergePlans([{ id: 't', name: 't', plan: { ...base, tasks: [] } as unknown as ProjectPlan }])!.plan.tasks[0];
const task = (id: string, title: string): PlanTask => ({ ...whole, id, title, subtasks_total: 0, wbs: '1', start: '2027-03-01', end: '2027-03-12', span_start: '2027-03-01', span_end: '2027-03-12', assignments: [{ type: 'user', id: 'u1', units: 100 }] as PlanTask['assignments'] });
const plans: Record<string, unknown> = {
  p1: { ...base, tasks: [task('weld', 'Weld the frame')], resources: [{ type: 'user', id: 'u1', name: 'Priya', daily_rate: 0, capacity: 100, tasks: 1, work_days: 5, cost: 0, peak: 100, overallocations: [], time_off: [] }] },
  p2: { ...base, tasks: [task('spec', 'Write the spec')] },
};

vi.mock('../../api/client', async (orig) =>
  mockApi(await orig(), {
    planAPI: {
      master: () => ok({ today: '2027-03-10', start: '2027-03-01', end: '2027-03-20', links: [], projects: [{ project_id: 'p1', name: 'Brake ECU' }, { project_id: 'p2', name: 'Wheel sensor' }] }),
      get: (id: string) => ok(plans[id]),
    },
  })
);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;
const Where: React.FC = () => <output data-where>{useLocation().pathname}</output>;
const mount = async (view: Exclude<WorkspaceView, 'lines'>) => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={['/org/plan']}>
        <PlanWorkspaceViews view={view} zoom="weeks" showCritical todaySignal={0} />
        <Where />
      </MemoryRouter>
    );
  });
  await act(async () => {
    for (let i = 0; i < 6; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};
afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

describe('PlanWorkspaceViews', () => {
  it('draws the Gantt of every project: a row per project, its tasks beneath, each with its bar', async () => {
    await mount('gantt');
    const titles = Array.from(container.querySelectorAll('[role="rowheader"] button[title]')).map((b) => b.getAttribute('title'));
    expect(titles).toEqual(expect.arrayContaining(['Brake ECU', 'Weld the frame', 'Wheel sensor', 'Write the spec']));
    expect(container.querySelector('[data-task-bar="weld"]')).toBeTruthy();
    expect(container.querySelector('[data-task-bar="project:p2"]')?.hasAttribute('data-task-summary')).toBe(true);
    // Nothing here edits: a task's open mark leads to its own project's Gantt.
    expect(container.querySelector('input[placeholder]')).toBeNull();
    await act(async () => (container.querySelector('[data-open-task="spec"]') as HTMLElement).click());
    expect(container.querySelector('[data-where]')?.textContent).toBe('/projects/p2/gantt');
  });

  it('draws who is loaded when across the projects', async () => {
    await mount('usage');
    expect(container.querySelector('[data-workspace-view="usage"]')?.textContent).toContain('Priya');
  });

  it('draws the network and the calendar from the same merged plan', async () => {
    await mount('network');
    expect(container.textContent).toContain('Weld the frame');
    act(() => root.unmount());
    container.remove();
    await mount('calendar');
    expect(container.textContent).toContain('Write the spec');
  });
});
