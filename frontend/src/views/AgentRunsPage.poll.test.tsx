import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes, useLocation, useNavigate, type NavigateFunction } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { AgentRunsPage } from './AgentRunsPage';
import { useAppStore } from '../state/store';
import type { AgentRun, WorkerStatus } from '../api/client';

// A project's Runs page polls its runs every 5 s, and anew on a switch of
// project or workspace or a new status filter. An answer is shown only while
// it is current (#379 bug 175): a late one for the project, workspace or
// filter the page has left is dropped, the runner banner's too, and so is a
// poll's answer that lands after a later poll's. One slower than the poll
// still counts while no later one has landed.
//
// A switch of project empties the list, which says it is loading until the
// new project's answer, and closes the old project's run (#379 bug 177); a
// run the switch comes with stays open. Another status filter keeps the
// list while its answer loads, and so does a workspace switch that keeps the
// project, as ProjectLayout makes to follow a link into another workspace.
//
// An error belongs to the project it was raised for: a switch of project
// hides the old project's (#379 bug 180), while another status filter or a
// workspace switch that keeps the project keeps it until an answer. The
// runner status belongs to the workspace it was read for: a workspace switch
// hides the old workspace's "no runner is online" warning until the new
// one's status arrives, and a read that fails keeps the workspace's last
// known status (#379 bug 181).

interface Deferred<T> {
  promise: Promise<T>;
  resolve: (value: T) => void;
  reject: (reason: unknown) => void;
}

const deferred = <T,>(): Deferred<T> => {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
};

// Each request the page makes, in order, with the answer the test gives it.
const lists: Array<{ query: Record<string, unknown>; answer: Deferred<{ data: AgentRun[] }> }> = [];
const statuses: Array<{ orgId: string; answer: Deferred<{ data: WorkerStatus }> }> = [];

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    agentRunsAPI: {
      list: (query) => {
        const answer = deferred<{ data: AgentRun[] }>();
        lists.push({ query: { ...query }, answer });
        return answer.promise;
      },
    },
    workerStatusAPI: {
      get: (orgId) => {
        const answer = deferred<{ data: WorkerStatus }>();
        statuses.push({ orgId, answer });
        return answer.promise;
      },
    },
  })
);

// Beside the runs, and not what this test is about.
vi.mock('../components/agents/ProposalReviewPanel', () => ({ ProposalReviewPanel: () => null }));
vi.mock('../components/RunnerConnectPrompt', () => ({ RunnerConnectPrompt: () => null }));
vi.mock('../hooks/useViewport', () => ({ useViewport: () => ({ isPhone: false, isCompact: false }) }));
// The detail panel is RunDetailPanel's own; here it shows which run is open.
vi.mock('../components/agents/RunDetailPanel', async (orig) => ({
  ...(await orig<typeof import('../components/agents/RunDetailPanel')>()),
  RunDetailPanel: ({ runId }: { runId: string }) => <aside aria-label="Run detail">{runId}</aside>,
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const run = (id: string, agentName: string, projectId = 'p1'): AgentRun => ({
  id,
  org_id: 'o1',
  agent_id: 'agent-1',
  agent_name: agentName,
  project_id: projectId,
  status: 'queued',
  prompt: 'Do it.',
  final_text: '',
  error: '',
  tokens_in: 0,
  tokens_out: 0,
  created_at: '2026-10-04T09:00:00Z',
});

const status = (queued: number): WorkerStatus => ({
  workers: [],
  queue: { queued, oldest_queued_seconds: 0, queued_repo_access: 0 },
});

const initialStore = useAppStore.getState();
let container: HTMLDivElement;
let root: Root;
let navigate: NavigateFunction;

const Navigator: React.FC = () => {
  navigate = useNavigate();
  const location = useLocation();
  return <output data-testid="where">{location.pathname + location.search}</output>;
};

const settle = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await Promise.resolve();
  });
};

const answer = async <T,>(request: { answer: Deferred<{ data: T }> }, data: T) => {
  await act(async () => {
    request.answer.resolve({ data });
  });
  await settle();
};

const refuse = async (request: { answer: { reject: (reason: unknown) => void } }, message: string) => {
  await act(async () => {
    request.answer.reject(new Error(message));
  });
  await settle();
};

const mount = async (at = '/projects/p1/agent-runs') => {
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={[at]}>
        <Routes>
          <Route path="/projects/:projectId/agent-runs" element={<AgentRunsPage />} />
        </Routes>
        <Navigator />
      </MemoryRouter>
    );
  });
  await settle();
};

const rows = () => Array.from(container.querySelectorAll('tbody tr')).map((tr) => tr.querySelector('td')?.textContent);
const banner = () => container.textContent?.includes('queued but') ?? false;
const detail = () => container.querySelector('[aria-label="Run detail"]')?.textContent ?? null;
const error = () => container.querySelector('[role="alert"] span')?.textContent ?? null;
const where = () => container.querySelector('[data-testid="where"]')?.textContent;
const go = async (to: string) => {
  await act(async () => {
    navigate(to);
  });
  await settle();
};
const filter = async (status: string) => {
  const select = container.querySelector('select') as HTMLSelectElement;
  const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!;
  await act(async () => {
    setter.call(select, status);
    select.dispatchEvent(new Event('change', { bubbles: true }));
  });
  await settle();
};

beforeEach(() => {
  vi.useFakeTimers();
  lists.length = 0;
  statuses.length = 0;
  useAppStore.setState({ ...initialStore, activeOrgId: 'o1' }, true);
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
  vi.useRealTimers();
});

describe("a project's Runs page drops an answer that is no longer current", () => {
  it('drops a late answer for the project it left', async () => {
    await mount();
    expect(lists.map((l) => l.query.project_id)).toEqual(['p1']);
    await act(async () => {
      navigate('/projects/p2/agent-runs');
    });
    await settle();
    expect(lists.map((l) => l.query.project_id)).toEqual(['p1', 'p2']);

    await answer(lists[1], [run('run-p2', 'Bravo', 'p2')]);
    expect(rows()).toEqual(['🤖 Bravo']);
    await answer(lists[0], [run('run-p1', 'Alpha')]);
    expect(rows()).toEqual(['🤖 Bravo']);
  });

  it('drops a late answer for the status filter it left', async () => {
    await mount();
    const select = container.querySelector('select') as HTMLSelectElement;
    const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!;
    await act(async () => {
      setter.call(select, 'failed');
      select.dispatchEvent(new Event('change', { bubbles: true }));
    });
    await settle();
    expect(lists.map((l) => l.query.status)).toEqual([undefined, 'failed']);

    await answer(lists[1], [{ ...run('run-f', 'Failer'), status: 'failed' }]);
    await answer(lists[0], [run('run-q', 'Queuer'), { ...run('run-f', 'Failer'), status: 'failed' }]);
    expect(rows()).toEqual(['🤖 Failer']);
  });

  it("drops a late answer for the workspace it left, the runner banner's too", async () => {
    await mount();
    await act(async () => {
      useAppStore.setState({ activeOrgId: 'o2' });
    });
    await settle();
    expect(statuses.map((s) => s.orgId)).toEqual(['o1', 'o2']);
    expect(lists).toHaveLength(2);

    await answer(lists[1], [run('run-2', 'Bravo')]);
    await answer(statuses[1], status(0));
    // The workspace left had a run waiting for a runner; the new one has none.
    await answer(lists[0], [run('run-1', 'Alpha')]);
    await answer(statuses[0], status(3));
    expect(rows()).toEqual(['🤖 Bravo']);
    expect(banner()).toBe(false);
  });

  it("drops a poll's answer that lands after a later poll's", async () => {
    await mount();
    await act(async () => {
      vi.advanceTimersByTime(5000);
    });
    expect(lists).toHaveLength(2);

    await answer(lists[1], [run('run-new', 'Newer')]);
    await answer(lists[0], [run('run-old', 'Older')]);
    expect(rows()).toEqual(['🤖 Newer']);
  });

  it('shows an answer slower than the poll while no later one has landed', async () => {
    await mount();
    await act(async () => {
      vi.advanceTimersByTime(5000);
    });
    expect(lists).toHaveLength(2);

    await answer(lists[0], [run('run-old', 'Older')]);
    expect(rows()).toEqual(['🤖 Older']);
    await answer(lists[1], [run('run-new', 'Newer')]);
    expect(rows()).toEqual(['🤖 Newer']);
  });
});

describe("a project's Runs page on a switch (#379 bug 177)", () => {
  it('says it is loading until its first answer', async () => {
    await mount();
    expect(rows()).toEqual(['Loading runs…']);
    await answer(lists[0], [run('run-p1', 'Alpha')]);
    expect(rows()).toEqual(['🤖 Alpha']);
  });

  it("empties the list on a project switch until the new project's answer", async () => {
    await mount();
    await answer(lists[0], [run('run-p1', 'Alpha')]);
    expect(rows()).toEqual(['🤖 Alpha']);

    await go('/projects/p2/agent-runs');
    expect(lists.map((l) => l.query.project_id)).toEqual(['p1', 'p2']);
    expect(rows()).toEqual(['Loading runs…']);
    await answer(lists[1], []);
    expect(rows()).toEqual(['No runs yet. Launch an agent from the Agents page or the board.']);
  });

  it("closes the old project's run on a project switch", async () => {
    await mount('/projects/p1/agent-runs?run=run-p1');
    await answer(lists[0], [run('run-p1', 'Alpha')]);
    expect(detail()).toBe('run-p1');

    // A switch that keeps the address's query, ?run= with it.
    await go('/projects/p2/agent-runs?run=run-p1');
    expect(detail()).toBeNull();
    expect(where()).toBe('/projects/p2/agent-runs');
    expect(rows()).toEqual(['Loading runs…']);

    // The close replaced the switch's entry: back is the old project, its run open.
    await act(async () => {
      navigate(-1);
    });
    await settle();
    expect(where()).toBe('/projects/p1/agent-runs?run=run-p1');
    expect(detail()).toBe('run-p1');
  });

  it('keeps open the run a project switch comes with, as a link to it does', async () => {
    await mount('/projects/p1/agent-runs?run=run-p1');
    await answer(lists[0], [run('run-p1', 'Alpha')]);

    await go('/projects/p2/agent-runs?run=run-p2');
    expect(detail()).toBe('run-p2');
    expect(where()).toBe('/projects/p2/agent-runs?run=run-p2');
  });

  it('keeps the list and the run open while another status filter loads', async () => {
    await mount('/projects/p1/agent-runs?run=run-f');
    await answer(lists[0], [run('run-q', 'Queuer'), { ...run('run-f', 'Failer'), status: 'failed' }]);

    await filter('failed');
    expect(lists.map((l) => l.query.status)).toEqual([undefined, 'failed']);
    expect(rows()).toEqual(['🤖 Queuer', '🤖 Failer']);
    expect(detail()).toBe('run-f');
    await answer(lists[1], [{ ...run('run-f', 'Failer'), status: 'failed' }]);
    expect(rows()).toEqual(['🤖 Failer']);
    expect(detail()).toBe('run-f');
  });

  it('keeps the list and the run open on a workspace switch that keeps the project', async () => {
    await mount('/projects/p1/agent-runs?run=run-p1');
    await answer(lists[0], [run('run-p1', 'Alpha')]);

    // ProjectLayout following a link into another workspace's project.
    await act(async () => {
      useAppStore.setState({ activeOrgId: 'o2' });
    });
    await settle();
    expect(lists).toHaveLength(2);
    expect(rows()).toEqual(['🤖 Alpha']);
    expect(detail()).toBe('run-p1');
    expect(where()).toBe('/projects/p1/agent-runs?run=run-p1');
  });
});

describe("a project's Runs page's error (#379 bug 180)", () => {
  it("hides the old project's error on a project switch", async () => {
    await mount();
    await refuse(lists[0], 'p1 is down');
    expect(error()).toBe('p1 is down');

    await go('/projects/p2/agent-runs');
    expect(error()).toBeNull();
    await answer(lists[1], [run('run-p2', 'Bravo', 'p2')]);
    expect(error()).toBeNull();
    expect(rows()).toEqual(['🤖 Bravo']);
  });

  it("shows the new project's own error", async () => {
    await mount();
    await refuse(lists[0], 'p1 is down');
    await go('/projects/p2/agent-runs');
    await refuse(lists[1], 'p2 is down');
    expect(error()).toBe('p2 is down');
  });

  it('keeps the error through another status filter and a workspace switch that keeps the project, until an answer', async () => {
    await mount();
    await refuse(lists[0], 'p1 is down');
    await filter('failed');
    expect(error()).toBe('p1 is down');
    await act(async () => {
      useAppStore.setState({ activeOrgId: 'o2' });
    });
    await settle();
    expect(lists).toHaveLength(3);
    expect(error()).toBe('p1 is down');
    await answer(lists[2], []);
    expect(error()).toBeNull();
  });
});

describe("a project's Runs page's runner warning (#379 bug 181)", () => {
  const switchWorkspace = async () => {
    // ProjectLayout following a link into another workspace's project.
    await act(async () => {
      useAppStore.setState({ activeOrgId: 'o2' });
    });
    await settle();
    expect(statuses.map((s) => s.orgId)).toEqual(['o1', 'o2']);
  };

  it("hides the old workspace's warning on a workspace switch until the new workspace's status", async () => {
    await mount();
    await answer(lists[0], [run('run-p1', 'Alpha')]);
    await answer(statuses[0], status(3));
    expect(banner()).toBe(true);

    await switchWorkspace();
    expect(banner()).toBe(false);
    expect(rows()).toEqual(['🤖 Alpha']);
    await answer(statuses[1], status(2));
    expect(banner()).toBe(true);
    expect(container.textContent).toContain('2 runs queued but');
  });

  it("does not bring the old workspace's warning back when the new workspace's status fails", async () => {
    await mount();
    await answer(statuses[0], status(3));
    await switchWorkspace();
    await refuse(statuses[1], 'Network Error');
    expect(banner()).toBe(false);
  });

  it('keeps the last known warning when a later read in the same workspace fails', async () => {
    await mount();
    await answer(statuses[0], status(3));
    await act(async () => {
      vi.advanceTimersByTime(5000);
    });
    await settle();
    expect(statuses.map((s) => s.orgId)).toEqual(['o1', 'o1']);
    await refuse(statuses[1], 'Network Error');
    expect(banner()).toBe(true);
  });
});
