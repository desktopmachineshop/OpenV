import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { GanttWorkspace } from './GanttWorkspace';
import { useAppStore } from '../state/store';
import { planAPI, type MasterPlan, type OrgFeatures } from '../api/client';

// The workspace's Plan page: every project's plan on one chart, on a page
// that belongs to no project. It waits for the gates, and without
// workspace-plan (or the Gantt plan) sends a member to the projects list.

const ok = (data: unknown) => Promise.resolve({ data });

const sub = { id: 'a1', title: 'Prime', wbs: '1.1', depth: 1, summary: false, start: '2026-11-02', end: '2026-11-06', progress: 0, done: false, late: false, critical: false };

const master = (name: string, levels = '1'): MasterPlan =>
  ({
    today: '2026-11-13',
    start: '2026-11-02',
    end: '2026-12-18',
    links: [],
    projects: [
      {
        project_id: 'p1',
        name,
        start: '2026-11-02',
        end: '2026-12-18',
        summary: { progress: 40, tasks: 2, done: 1, late: 0 },
        milestones: [],
        tasks: [
          ...(levels === '1' ? [] : [sub]),
          { id: 'a', title: 'Paint', wbs: '1', depth: 0, summary: true, start: '2026-11-02', end: '2026-11-13', progress: 0, done: false, late: false, critical: false },
          { id: 'b', title: 'Weld', wbs: '2', depth: 0, summary: false, start: '2026-11-09', end: '2026-12-18', progress: 0, done: false, late: false, critical: true },
        ],
      },
    ],
  }) as unknown as MasterPlan;

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    planAPI: { master: (levels?: string) => ok(master(useAppStore.getState().activeOrgId === 'o2' ? 'Other rig' : 'Brake ECU', levels)) },
  })
);

// The page's chrome is not what this test is about.
vi.mock('../components/Navbar', () => ({ Navbar: () => null }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const initialStore = useAppStore.getState();

const gates = (page: boolean, gantt = true): OrgFeatures => ({ channel: 'nightly', stable_release: '', preview: false, features: { 'workspace-plan': page, 'gantt-plan': gantt } });

let container: HTMLDivElement;
let root: Root;

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const Where: React.FC = () => <output data-testid="where">{useLocation().pathname}</output>;
const where = () => document.body.querySelector('[data-testid="where"]')?.textContent;

const mount = async (features: OrgFeatures | null) => {
  useAppStore.setState({ ...initialStore, activeOrgId: 'o1', orgsLoaded: true, features } as never, true);
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={['/org/plan']}>
        <Routes>
          <Route path="/org/plan" element={<GanttWorkspace />} />
          <Route path="/projects" element={<p>Projects list</p>} />
        </Routes>
        <Where />
      </MemoryRouter>
    );
  });
  await flush();
};

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  localStorage.clear();
  vi.clearAllMocks();
});

const bars = () => Array.from(container.querySelectorAll<HTMLElement>('[data-master-project="p1"] button')).map((b) => b.textContent);

describe('GanttWorkspace', () => {
  it('draws each project on a line of the shared chart, its critical path on the top row', async () => {
    await mount(gates(true));
    expect(planAPI.master).toHaveBeenCalledTimes(1);
    // Level 1, the top-level tasks, to begin with.
    expect(planAPI.master).toHaveBeenLastCalledWith('1');
    expect(container.querySelector('[data-plan-view="master"]')).not.toBeNull();
    expect(container.querySelector('[data-master-project="p1"] a')?.getAttribute('href')).toBe('/projects/p1/gantt');
    // Weld is critical, so it is listed first although Paint starts earlier.
    expect(bars()).toEqual(['Weld', 'Paint']);
    expect(container.querySelectorAll('[data-critical]')).toHaveLength(1);
    // Months to begin with: a workspace's plans are long.
    expect(container.querySelector('[data-plan-view="master"]')?.getAttribute('data-plan-scale')).toBe('months');
  });

  it('turns the critical marking off, changes the scale, and loads the other workspace on a switch', async () => {
    await mount(gates(true));
    const button = (name: string) => Array.from(container.querySelectorAll('button')).find((b) => b.textContent === name)!;
    await act(async () => button('Critical path').click());
    expect(container.querySelectorAll('[data-critical]')).toHaveLength(0);
    await act(async () => button('Weeks').click());
    expect(container.querySelector('[data-plan-view="master"]')?.getAttribute('data-plan-scale')).toBe('weeks');
    await act(async () => useAppStore.setState({ activeOrgId: 'o2' }));
    await flush();
    expect(planAPI.master).toHaveBeenCalledTimes(2);
    expect(container.textContent).toContain('Other rig');
  });

  it('digs a level down when the reader asks for it', async () => {
    await mount(gates(true));
    const level = container.querySelector<HTMLSelectElement>('select[aria-label="Level of detail"]')!;
    expect(Array.from(level.options).map((o) => o.textContent)).toEqual(['Level 1', 'Level 2', 'Level 3', 'Level 4', 'All levels']);
    expect(level.value).toBe('1');
    expect(container.querySelectorAll('[data-summary]')).toHaveLength(0);
    await act(async () => {
      level.value = 'all';
      level.dispatchEvent(new Event('change', { bubbles: true }));
    });
    await flush();
    expect(planAPI.master).toHaveBeenLastCalledWith('all');
    // The parent stays, drawn as a parent, with its sub-task on a row beneath.
    expect(bars()).toEqual(['Weld', 'Paint', 'Prime']);
    expect(container.querySelector('[data-summary]')?.textContent).toBe('Paint');
    expect(container.querySelector<HTMLSelectElement>('select[aria-label="Level of detail"]')!.value).toBe('all');
    // The choice is this browser's: the chart opens on it the next time.
    act(() => root.unmount());
    container.remove();
    await mount(gates(true));
    expect(container.querySelector<HTMLSelectElement>('select[aria-label="Level of detail"]')!.value).toBe('all');
    expect(planAPI.master).toHaveBeenLastCalledWith('all');
  });

  it('waits for the gates, and without the page or the Gantt plan leads to the projects list', async () => {
    await mount(null);
    expect(where()).toBe('/org/plan');
    expect(container.textContent).toContain('Loading…');
    expect(planAPI.master).not.toHaveBeenCalled();
    act(() => root.unmount());
    container.remove();
    await mount(gates(false));
    expect(where()).toBe('/projects');
    act(() => root.unmount());
    container.remove();
    await mount(gates(true, false));
    expect(where()).toBe('/projects');
  });
});
