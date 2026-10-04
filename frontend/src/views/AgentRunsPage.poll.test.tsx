import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes, useNavigate, type NavigateFunction } from 'react-router-dom';
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

interface Deferred<T> {
  promise: Promise<T>;
  resolve: (value: T) => void;
}

const deferred = <T,>(): Deferred<T> => {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
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
  return null;
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

const mount = async () => {
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={['/projects/p1/agent-runs']}>
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
