import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { GanttWorkspaceTasks, myTaskOrder } from './GanttWorkspaceTasks';
import { useAppStore } from '../state/store';
import { planAPI, type MyPlanTasks, type OrgFeatures } from '../api/client';

// The workspace Plan's My tasks: one person's tasks across every project,
// open ones first, each leading to its project's Gantt.

type Task = MyPlanTasks['tasks'][number];
const t = (id: string, extra: Partial<Task> = {}): Task => ({ project_id: 'p1', project_name: 'Rig', id, title: id, column: 'todo', start: '2026-11-02', end: '2026-11-10', progress: 0, done: false, late: false, critical: false, ...extra });

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    planAPI: {
      mine: () =>
        Promise.resolve({
          data: { today: '2026-11-13', tasks: [t('Paint', { end: '2026-11-20' }), t('Weld', { done: true, progress: 100 }), t('Order steel', { late: true, project_id: 'p2', project_name: 'Supply' }), t('Draft')] },
        }),
    },
  })
);
vi.mock('../components/Navbar', () => ({ Navbar: () => null }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
const initialStore = useAppStore.getState();
const gates = (on: boolean): OrgFeatures => ({ channel: 'nightly', stable_release: '', preview: false, features: { 'workspace-plan': on, 'gantt-plan': true } });

let container: HTMLDivElement;
let root: Root;
const mount = async (features: OrgFeatures) => {
  useAppStore.setState({ ...initialStore, activeOrgId: 'o1', orgsLoaded: true, features } as never, true);
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={['/org/plan/tasks']}>
        <Routes>
          <Route path="/org/plan/tasks" element={<GanttWorkspaceTasks />} />
          <Route path="/projects" element={<p>Projects list</p>} />
        </Routes>
      </MemoryRouter>
    );
  });
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((r) => setTimeout(r, 0));
  });
};
afterEach(() => {
  // The pure test mounts nothing.
  if (!container?.isConnected) return;
  act(() => root.unmount());
  container.remove();
});

describe('My tasks', () => {
  it('orders open tasks before done ones, the late first, then by the day they end', () => {
    const order = [t('b', { end: '2026-11-20' }), t('done', { done: true }), t('late', { late: true, end: '2026-12-01' }), t('a'), t('undated', { end: null })].sort(myTaskOrder).map((x) => x.id);
    expect(order).toEqual(['late', 'a', 'b', 'undated', 'done']);
  });

  it('lists the open tasks with their project, each a link to its Gantt, and the done ones on request', async () => {
    await mount(gates(true));
    const rows = () => Array.from(container.querySelectorAll('[data-my-task]')).map((li) => [li.querySelector('a')?.textContent, li.querySelector('a')?.getAttribute('href')]);
    expect(rows()).toEqual([
      ['Order steel', '/projects/p2/gantt'],
      ['Draft', '/projects/p1/gantt'],
      ['Paint', '/projects/p1/gantt'],
    ]);
    expect(container.textContent).toContain('Supply');
    await act(async () => container.querySelector<HTMLInputElement>('input[type="checkbox"]')!.click());
    expect(rows().map(([name]) => name)).toEqual(['Order steel', 'Draft', 'Paint', 'Weld']);
  });

  it('leads to the projects list without the workspace plan', async () => {
    await mount(gates(false));
    expect(container.textContent).toContain('Projects list');
    expect(planAPI.mine).not.toHaveBeenCalled();
  });
});
