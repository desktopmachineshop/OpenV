import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { mockApi } from './test/mockApi';
import App from './App';
import { useAppStore } from './state/store';
import { orgsAPI, type AgentRun, type OrgFeatures } from './api/client';

// The Workspace runs page waits for the workspace's feature gates, which App
// loads, and without workspace-runs sends a member to the projects list. A
// gate load that fails counts as the feature off, as it hides gated UI
// elsewhere: the page leaves for the projects list instead of saying
// "Loading…" for good, while a load still on its way keeps it waiting
// (#379 bug 174). So does a timed retry of a load that failed still to come,
// which may bring the gates back: the page leaves only once the last one
// fails (#379 bug 179).
//
// A link to a run of the member's other workspace switches the workspace,
// which loads its gates anew, and the run stays open there (#379 bug 177).

const ok = (data: unknown) => Promise.resolve({ data });

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

let gatesLoad: Deferred<{ data: OrgFeatures }>;

vi.mock('./api/client', async (orig) =>
  mockApi(await orig(), {
    authAPI: {
      me: () => ok({ id: 'u1', email: 'dana@example.com', name: 'Dana', email_verified: true }),
      config: () => ok({ email_verification_required: false }),
    },
    orgsAPI: {
      list: () =>
        ok({
          orgs: [
            { id: 'acme', name: 'Acme', type: 'company', role: 'member' },
            { id: 'bigco', name: 'Bigco', type: 'company', role: 'member' },
          ],
          active_org: 'acme',
        }),
      features: () => gatesLoad.promise,
    },
    agentRunsAPI: {
      list: () => ok([]),
      get: (id) => ok({ id, org_id: 'bigco', project_id: null, status: 'failed' } as Partial<AgentRun>),
    },
  })
);

// The detail panel is RunDetailPanel's own; here it shows which run is open.
vi.mock('./components/agents/RunDetailPanel', async (orig) => ({
  ...(await orig<typeof import('./components/agents/RunDetailPanel')>()),
  RunDetailPanel: ({ runId }: { runId: string }) => <aside aria-label="Run detail">{runId}</aside>,
}));

// The page's title says it is the one on screen, and not the route's
// Suspense fallback, which says "Loading…" too.
vi.mock('./components/Navbar', () => ({ Navbar: ({ title }: { title?: string }) => <h1>{title}</h1> }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const initialStore = useAppStore.getState();
let container: HTMLDivElement;
let root: Root;

const Where: React.FC = () => {
  const location = useLocation();
  return <output data-testid="where">{location.pathname + location.search}</output>;
};
const where = () => container.querySelector('[data-testid="where"]')?.textContent;
const heading = () => container.querySelector('h1')?.textContent;

const settle = async () => {
  await act(async () => {
    for (let i = 0; i < 20; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const boot = async (at = '/org/runs') => {
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={[at]}>
        <App />
        <Where />
      </MemoryRouter>
    );
  });
  await settle();
};

// After boot the gate load's retries wait on fake timers: these flush what
// a failure or an answer sets going, and run the wait before a retry.
const tick = async () => {
  await act(async () => {
    for (let i = 0; i < 20; i++) await Promise.resolve();
  });
};

const failGates = async () => {
  await act(async () => {
    gatesLoad.reject(new Error('Network Error'));
  });
  await tick();
  // The next try gets an answer of its own.
  gatesLoad = deferred();
};

const wait = async (ms: number) => {
  await act(async () => {
    vi.advanceTimersByTime(ms);
  });
  await tick();
};

beforeEach(() => {
  sessionStorage.clear();
  localStorage.clear();
  gatesLoad = deferred();
  useAppStore.setState(initialStore, true);
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
});

afterEach(() => {
  // Real timers first: what boot set going clears with them.
  vi.useRealTimers();
  act(() => root.unmount());
  container.remove();
  useAppStore.setState(initialStore, true);
  sessionStorage.clear();
  localStorage.clear();
});

describe('the Workspace runs page when the gates fail to load', () => {
  it('waits while the gates are on their way and through their timed retries, and leaves for the projects list once the last fails', async () => {
    await boot();
    expect(vi.mocked(orgsAPI.features)).toHaveBeenCalledWith('acme');
    expect(where()).toBe('/org/runs');
    expect(heading()).toBe('Workspace runs');
    expect(container.textContent).toContain('Loading…');

    vi.useFakeTimers();
    for (const delay of [2000, 5000, 15000]) {
      await failGates();
      expect(where()).toBe('/org/runs');
      expect(container.textContent).toContain('Loading…');
      await wait(delay);
    }
    expect(vi.mocked(orgsAPI.features)).toHaveBeenCalledTimes(4);
    await failGates();
    expect(where()).toBe('/projects');
  });

  it('opens when a retry brings the gates back (#379 bug 179)', async () => {
    await boot();
    vi.useFakeTimers();
    await failGates();
    expect(where()).toBe('/org/runs');
    expect(container.textContent).toContain('Loading…');

    await wait(2000);
    expect(vi.mocked(orgsAPI.features)).toHaveBeenCalledTimes(2);
    await act(async () => {
      gatesLoad.resolve({
        data: { channel: 'nightly', stable_release: '', preview: false, features: { 'workspace-runs': true } },
      });
    });
    await tick();
    expect(where()).toBe('/org/runs');
    expect(container.textContent).toContain('The runs that belong to no project');
  });

  it('opens once the gates load with the feature on', async () => {
    await boot();
    await act(async () => {
      gatesLoad.resolve({
        data: { channel: 'nightly', stable_release: '', preview: false, features: { 'workspace-runs': true } },
      });
    });
    await settle();
    expect(where()).toBe('/org/runs');
    expect(container.textContent).toContain('The runs that belong to no project');
  });
});

describe("a link to a run of the member's other workspace", () => {
  it('switches to that workspace, loads its gates, and keeps the run open (#379 bug 177)', async () => {
    await boot('/org/runs?run=run-b');
    await act(async () => {
      gatesLoad.resolve({
        data: { channel: 'nightly', stable_release: '', preview: false, features: { 'workspace-runs': true } },
      });
    });
    await settle();
    expect(useAppStore.getState().activeOrgId).toBe('bigco');
    expect(vi.mocked(orgsAPI.features)).toHaveBeenLastCalledWith('bigco');
    expect(where()).toBe('/org/runs?run=run-b');
    expect(container.querySelector('[aria-label="Run detail"]')?.textContent).toBe('run-b');
  });
});
