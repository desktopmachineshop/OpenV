import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { WorkspaceRunsPage } from './WorkspaceRunsPage';
import { useAppStore } from '../state/store';
import { agentRunsAPI, type AgentRun, type Org, type OrgFeatures } from '../api/client';

// The workspace Runs page (#379 bug 168): the runs with no project, which no
// page listed, with the run detail beside them, selected by ?run=. It lists
// what the server answers for project=none, which is what the member may
// open; it waits for the gates, and without workspace-runs sends a member
// to the projects list, where /org/runs led before.

const ok = (data: unknown) => Promise.resolve({ data });

const run = (id: string, agentName: string, orgId = 'o1'): AgentRun => ({
  id,
  org_id: orgId,
  agent_id: 'agent-1',
  agent_name: agentName,
  project_id: null,
  status: 'queued',
  prompt: 'Do it.',
  final_text: '',
  error: '',
  tokens_in: 0,
  tokens_out: 0,
  created_at: '2026-10-04T09:00:00Z',
});

// Newest first, as the server lists them.
const listed = [run('run-new', 'Welcomer'), run('run-old', 'Inventor')];
const elsewhere = run('run-b', 'Auditor', 'o2');

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    agentRunsAPI: {
      list: () => ok(listed),
      get: (id: string) => ok(id === elsewhere.id ? elsewhere : listed.find((r) => r.id === id) || listed[0]),
    },
    notificationsAPI: { list: () => ok({ notifications: [], unread_count: 0 }) },
  })
);

// The detail panel is RunDetailPanel's own; here it shows which run is open.
vi.mock('../components/agents/RunDetailPanel', async (orig) => ({
  ...(await orig<typeof import('../components/agents/RunDetailPanel')>()),
  RunDetailPanel: ({ runId, onClose }: { runId: string; onClose: () => void }) => (
    <aside aria-label="Run detail">
      {runId}
      <button onClick={onClose}>Close run detail</button>
    </aside>
  ),
}));

// The page's chrome is not what this test is about.
vi.mock('../components/Navbar', () => ({ Navbar: () => null }));

const mockViewport = vi.hoisted(() => ({ isPhone: false, isCompact: false }));
vi.mock('../hooks/useViewport', () => ({ useViewport: () => mockViewport }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const initialStore = useAppStore.getState();
const list = vi.mocked(agentRunsAPI.list);

const gates = (on: boolean): OrgFeatures => ({
  channel: on ? 'nightly' : 'stable',
  stable_release: '',
  preview: false,
  features: { 'workspace-runs': on },
});

const org = (id: string, role: string): Org =>
  ({ id, name: id, slug: id, type: 'company', plan: 'business', role, created_at: '' }) as Org;

let container: HTMLDivElement;
let root: Root;

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

// Where shows the address the app is at.
const Where: React.FC = () => {
  const location = useLocation();
  return <output data-testid="where">{location.pathname + location.search}</output>;
};
const where = () => document.body.querySelector('[data-testid="where"]')?.textContent;

const mount = async (at: string, features: OrgFeatures | null, role = 'member') => {
  useAppStore.setState(
    {
      ...initialStore,
      currentUser: { id: 'u1', email: 'sam@example.test', name: 'Sam' } as any,
      activeOrgId: 'o1',
      orgs: [org('o1', role), org('o2', 'member')],
      features,
    },
    true
  );
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={[at]}>
        <Routes>
          <Route path="/org/runs" element={<WorkspaceRunsPage />} />
          <Route path="/projects" element={<p>Projects</p>} />
        </Routes>
        <Where />
      </MemoryRouter>
    );
  });
  await flush();
};

const rows = () => Array.from(container.querySelectorAll('tbody tr')).map((tr) => tr.querySelector('td')?.textContent);
const detail = () => container.querySelector('[aria-label="Run detail"]');

const click = async (el: Element) => {
  await act(async () => {
    el.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });
  await flush();
};

beforeEach(() => {
  list.mockClear();
  vi.mocked(agentRunsAPI.get).mockClear();
  mockViewport.isPhone = false;
  mockViewport.isCompact = false;
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  useAppStore.setState(initialStore, true);
});

describe('the workspace Runs page', () => {
  it('lists the runs with no project, newest first, as the server answers', async () => {
    await mount('/org/runs', gates(true));
    expect(list).toHaveBeenCalledWith({ project: 'none', limit: 200 });
    expect(rows()).toEqual(['🤖 Welcomer', '🤖 Inventor']);
    expect(detail()).toBeNull();
    expect(container.textContent).toContain('You see the ones you launched');
  });

  it("tells a workspace admin they see everyone's", async () => {
    await mount('/org/runs', gates(true), 'admin');
    expect(container.textContent).toContain('As a workspace admin you see everyone’s.');
  });

  it('opens a run beside the list, names it in the address, and closes it', async () => {
    await mount('/org/runs', gates(true));
    await click(container.querySelectorAll('tbody tr')[1]);
    expect(where()).toBe('/org/runs?run=run-old');
    expect(detail()?.textContent).toContain('run-old');
    // Still listed beside it.
    expect(rows()).toEqual(['🤖 Welcomer', '🤖 Inventor']);

    await click(Array.from(container.querySelectorAll('button')).find((b) => b.textContent === 'Close run detail')!);
    expect(where()).toBe('/org/runs');
    expect(detail()).toBeNull();
  });

  it('keeps the run open on a reload (?run=)', async () => {
    await mount('/org/runs?run=run-new', gates(true));
    expect(detail()?.textContent).toContain('run-new');
  });

  it('opens the run as a sheet over the list on a compact screen', async () => {
    mockViewport.isPhone = true;
    mockViewport.isCompact = true;
    await mount('/org/runs?run=run-new', gates(true));
    expect(document.body.querySelector('[role="dialog"][aria-label="Run detail"]')).not.toBeNull();
  });

  it('filters by status', async () => {
    await mount('/org/runs', gates(true));
    const select = container.querySelector('select') as HTMLSelectElement;
    const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!;
    await act(async () => {
      setter.call(select, 'failed');
      select.dispatchEvent(new Event('change', { bubbles: true }));
    });
    await flush();
    expect(list).toHaveBeenLastCalledWith({ project: 'none', limit: 200, status: 'failed' });
  });

  it("follows a linked run to its workspace, the member's other one", async () => {
    await mount('/org/runs?run=run-b', gates(true));
    expect(vi.mocked(agentRunsAPI.get)).toHaveBeenCalledWith('run-b');
    expect(useAppStore.getState().activeOrgId).toBe('o2');
    expect(where()).toBe('/org/runs?run=run-b');
  });
});

describe('the workspace-runs gate', () => {
  it('sends a member to the projects list without the feature, as /org/runs did before', async () => {
    await mount('/org/runs?run=run-new', gates(false));
    expect(where()).toBe('/projects');
    expect(list).not.toHaveBeenCalled();
  });

  it('sends a member with no workspace, whose gates never load, to the projects list', async () => {
    await mount('/org/runs', null);
    await act(async () => {
      useAppStore.setState({ activeOrgId: '', orgsLoaded: true });
    });
    await flush();
    expect(where()).toBe('/projects');
    expect(list).not.toHaveBeenCalled();
  });

  it('takes gates that fail to load as the feature off (#379 bug 174)', async () => {
    await mount('/org/runs?run=run-new', null);
    expect(container.textContent).toContain('Loading…');
    await act(async () => {
      useAppStore.getState().setFeaturesFailed();
    });
    await flush();
    expect(where()).toBe('/projects');
    expect(list).not.toHaveBeenCalled();
  });

  it('waits again for a gate load that starts after one failed, as a workspace switch starts it', async () => {
    await mount('/org/runs', null);
    await act(async () => {
      useAppStore.getState().setFeaturesFailed();
      // App clears the gates as it sets the next load going.
      useAppStore.getState().setFeatures(null);
    });
    await flush();
    expect(where()).toBe('/org/runs');
    expect(container.textContent).toContain('Loading…');
  });

  it('waits for the gates before listing anything', async () => {
    await mount('/org/runs?run=run-new', null);
    expect(where()).toBe('/org/runs?run=run-new');
    expect(container.textContent).toContain('Loading…');
    expect(list).not.toHaveBeenCalled();

    await act(async () => {
      useAppStore.setState({ features: gates(true) });
    });
    await flush();
    expect(list).toHaveBeenCalledWith({ project: 'none', limit: 200 });
    expect(detail()?.textContent).toContain('run-new');
  });
});
