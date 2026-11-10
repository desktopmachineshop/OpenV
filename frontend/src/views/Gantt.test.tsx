import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { Gantt } from './Gantt';
import { DialogProvider } from '../components/ui';
import { planAPI, todosAPI, type PlanMilestone, type PlanTask, type ProjectPlan } from '../api/client';
import { useAppStore } from '../state/store';

// The Plan page: the chart's rows, bars and milestones from the plan the API
// answers, a task's panel with its schedule and to-dos, a milestone's
// criteria, the phone's schedule list, and the feature gate.

const ok = (data: unknown) => Promise.resolve({ data });

const task = (id: string, title: string, extra: Partial<PlanTask> = {}): PlanTask => ({
  id,
  title,
  column: 'todo',
  assignee_type: 'user',
  assignee_id: null,
  parent_id: null,
  depth: 0,
  wbs: '1',
  start: null,
  end: null,
  span_start: null,
  span_end: null,
  progress: 0,
  done: false,
  late: false,
  todos_done: 0,
  todos_total: 0,
  subtasks_done: 0,
  subtasks_total: 0,
  todos: [],
  depends_on: [],
  conflicts: [],
  ...extra,
});

const definition: PlanMilestone = {
  key: 'm1',
  kind: 'custom',
  id: 'm1',
  release_id: null,
  name: 'Definition complete',
  description: '',
  date: '2027-03-05',
  default_criteria: false,
  criteria: [{ metric: 'needs_addressed', label: 'User needs addressed', target: 100, actual: 50, count: 1, total: 2, met: false, percent: 50 }],
  progress: 50,
  met: false,
  late: true,
};

const mvp: PlanMilestone = {
  key: 'release:r1',
  kind: 'release',
  id: '',
  release_id: 'r1',
  name: 'MVP',
  description: '',
  date: '2027-04-01',
  release_status: 'planned',
  default_criteria: true,
  criteria: [],
  progress: 0,
  met: false,
  late: false,
};

const plan: ProjectPlan = {
  project_id: 'p1',
  today: '2027-03-10',
  start: '2027-03-01',
  end: '2027-04-01',
  tasks: [
    task('build', 'Build the rig', { wbs: '1', span_start: '2027-03-01', span_end: '2027-03-20', subtasks_total: 2, subtasks_done: 1, progress: 75 }),
    task('frame', 'Weld the frame', {
      wbs: '1.1',
      depth: 1,
      parent_id: 'build',
      column: 'done',
      done: true,
      progress: 100,
      start: '2027-03-01',
      end: '2027-03-09',
      span_start: '2027-03-01',
      span_end: '2027-03-09',
    }),
    task('wiring', 'Wire the rig', {
      wbs: '1.2',
      depth: 1,
      parent_id: 'build',
      start: '2027-03-08',
      end: '2027-03-20',
      span_start: '2027-03-08',
      span_end: '2027-03-20',
      progress: 50,
      todos_done: 1,
      todos_total: 2,
      todos: [
        { id: 't2', text: 'Label the loom', done: false },
        { id: 't1', text: 'Crimp the connectors', done: true },
      ],
      depends_on: ['frame'],
      conflicts: ['frame'],
    }),
    task('loose', 'Packaging concept', { wbs: '2' }),
  ],
  milestones: [definition, mvp],
  dependencies: [{ work_item_id: 'wiring', depends_on_id: 'frame' }],
  metrics: [{ metric: 'needs_addressed', label: 'User needs addressed', count: 1, total: 2, percent: 50 }],
  metric_catalogue: [
    { key: 'needs_addressed', label: 'User needs addressed', description: 'User needs that at least one requirement derives from.' },
    { key: 'requirements_verified', label: 'Requirements verified', description: 'Requirements whose verification passes.' },
  ],
  summary: { tasks: 4, done: 1, late: 0, unscheduled: 1, progress: 38 },
};

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    planAPI: {
      get: () => ok(plan),
      schedule: () => ok(plan.tasks[2]),
      setReleaseCriteria: () => ok({}),
    },
    todosAPI: { setDone: () => ok({}) },
  })
);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const features = (on: boolean) =>
  useAppStore.setState({ features: { channel: 'nightly', stable_release: '', preview: false, features: { 'gantt-plan': on } } });

const mount = async (width = 1280) => {
  Object.defineProperty(window, 'innerWidth', { value: width, configurable: true });
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(
      <DialogProvider>
        <MemoryRouter initialEntries={['/projects/p1/gantt']}>
          <Routes>
            <Route path="/projects/:projectId/gantt" element={<Gantt />} />
          </Routes>
        </MemoryRouter>
      </DialogProvider>
    );
  });
  await flush();
};

const click = async (el: Element | null | undefined) => {
  expect(el).toBeTruthy();
  await act(async () => {
    (el as HTMLElement).click();
  });
  await flush();
};

const button = (label: string) => Array.from(container.querySelectorAll('button')).find((b) => b.textContent === label);

beforeEach(() => {
  vi.clearAllMocks();
  features(true);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

describe('Plan page', () => {
  it('draws the outline, a bar per scheduled task and a diamond per milestone', async () => {
    await mount();
    expect(vi.mocked(planAPI.get)).toHaveBeenCalledWith('p1');
    const rows = Array.from(container.querySelectorAll('[role="rowheader"]')).map((r) => r.textContent);
    expect(rows).toEqual(['▼1Build the rig75%', '1.1Weld the frame100%', '1.2Wire the rig⚠ 50%', '2Packaging concept0%']);
    const bars = Array.from(container.querySelectorAll('[data-task-bar]')).map((b) => b.getAttribute('aria-label'));
    expect(bars).toEqual([
      'Build the rig: 1 Mar – 20 Mar, 75% complete',
      'Weld the frame: 1 Mar – 9 Mar, 100% complete',
      'Wire the rig: 8 Mar – 20 Mar, 50% complete',
    ]);
    expect(button('+ Schedule')?.getAttribute('aria-label')).toBe('Schedule Packaging concept for a week from today');
    const diamonds = Array.from(container.querySelectorAll('[data-milestone]')).map((b) => b.getAttribute('aria-label'));
    expect(diamonds).toEqual(['Milestone Definition complete, 5 Mar, 50%, late', 'Release milestone MVP, 1 Apr, 0%']);
    expect(container.querySelector('[data-help-key="gantt.summary"]')?.textContent).toBe(
      '38%complete4tasks1done0late1not scheduled0/2milestones met'
    );
    // The arrow from Frame to Wiring is a conflict: Wiring starts before Frame ends.
    expect(container.querySelector('svg path[stroke="var(--danger)"]')).toBeTruthy();
  });

  it('folds a task with sub-tasks', async () => {
    await mount();
    await click(container.querySelector('button[aria-label="Fold Build the rig"]'));
    expect(container.querySelectorAll('[role="rowheader"]')).toHaveLength(2);
    expect(container.querySelector('button[aria-label="Unfold Build the rig"]')?.getAttribute('aria-expanded')).toBe('false');
  });

  it('opens a task with its schedule, what it depends on and its to-dos, and ticks one off', async () => {
    await mount();
    await click(container.querySelector('[data-task-bar="wiring"]'));
    const panel = container.querySelector('aside[aria-label="Task Wire the rig"]')!;
    expect(panel.textContent).toContain('1 of 2 to-dos ticked off');
    expect(panel.textContent).toContain('1.1 Weld the frame');
    expect(panel.textContent).toContain('Overlaps');
    expect((panel.querySelector('input[type="date"]') as HTMLInputElement).value).toBe('2027-03-08');
    // A task may not go under itself, and may not wait on one that waits on it.
    const selects = Array.from(panel.querySelectorAll('select'));
    expect(Array.from(selects[0].options).map((o) => o.value)).toEqual(['', 'build', 'frame', 'loose']);
    expect(Array.from(selects[1].options).map((o) => o.value)).toEqual(['', 'build', 'loose']);

    const boxes = panel.querySelectorAll('input[type="checkbox"]');
    expect(Array.from(boxes).map((b) => (b as HTMLInputElement).checked)).toEqual([false, true]);
    await click(boxes[0]);
    expect(vi.mocked(todosAPI.setDone)).toHaveBeenCalledWith('t2', true);
    expect(vi.mocked(planAPI.get)).toHaveBeenCalledTimes(2);
  });

  it("shows a milestone's criteria, and saves a release milestone's through the release", async () => {
    await mount();
    await click(container.querySelector('[data-milestone="m1"]'));
    const panel = container.querySelector('aside[aria-label="Milestone Definition complete"]')!;
    expect(panel.textContent).toContain('Late');
    expect(panel.textContent).toContain('User needs addressed');
    expect(panel.textContent).toContain('1 of 2');
    expect(panel.querySelector('[role="img"]')?.getAttribute('aria-label')).toBe(
      'User needs addressed: 50% against a target of 100%, 1 of 2'
    );

    await click(container.querySelector('[data-milestone="release:r1"]'));
    const release = container.querySelector('aside[aria-label="Milestone MVP"]')!;
    expect(release.textContent).toContain("Its name and date are the release's.");
    expect(release.querySelector('a')?.getAttribute('href')).toBe('/projects/p1/releases?release=r1');
    await click(button('Edit criteria'));
    await click(button('+ Add criterion'));
    await click(button('Save'));
    expect(vi.mocked(planAPI.setReleaseCriteria)).toHaveBeenCalledWith('p1', 'r1', [{ metric: 'needs_addressed', target: 100 }]);
  });

  it('is a schedule list on a phone, grouped by milestone, with no timeline', async () => {
    await mount(390);
    expect(container.querySelector('[role="table"]')).toBeNull();
    const sections = Array.from(container.querySelectorAll('section')).map((s) => s.getAttribute('aria-label'));
    expect(sections).toEqual(['Definition complete', 'MVP', 'Not scheduled']);
    expect(container.querySelector('section[aria-label="MVP"]')?.textContent).toContain('Build the rig');
    expect(container.querySelector('section[aria-label="MVP"]')?.textContent).toContain('Wire the rig');
    await click(button('Milestones 2'));
    expect(container.querySelector('section[aria-label="Milestones"]')?.textContent).toContain('0/1 criteria met');
  });

  it('waits for the gantt-plan feature', async () => {
    features(false);
    await mount();
    expect(container.textContent).toContain("The Gantt page is not in your workspace's release yet.");
    expect(vi.mocked(planAPI.get)).not.toHaveBeenCalled();
  });
});
