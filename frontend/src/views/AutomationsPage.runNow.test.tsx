import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { AutomationsPage } from './AutomationsPage';
import { DialogProvider } from '../components/ui';
import { useAppStore } from '../state/store';
import { agentRunsAPI, type Automation, type Org } from '../api/client';

// Where Run now takes a workspace admin (#379 bug 141). A project
// automation's run is the project's: Run now opens it on the project's Runs
// page, listed there and shown. A whole-workspace automation's run has no
// project, so no project's Runs page lists it, and Run now opened it on
// the project's Runs page all the same, shown in the detail panel beside a
// list without it. It now opens beside the automations, where the run is
// shown in full, and the address names it (?run=) so a reload keeps it.

const ok = (data: unknown) => Promise.resolve({ data });

const automation = (id: string, name: string, extra: Partial<Automation>): Automation => ({
  id,
  name,
  agent_id: 'agent-1',
  kind: 'manual',
  enabled: true,
  prompt_template: '',
  cron_expr: '',
  catch_up: false,
  event_type: '',
  event_filter: {},
  cooldown_seconds: 60,
  max_runs_per_hour: 10,
  ...extra,
});

const run = (id: string, projectId: string | null) => ({
  id,
  agent_id: 'agent-1',
  agent_name: 'Greeter',
  project_id: projectId,
  status: 'queued',
  prompt: 'Welcome them.',
  tokens_in: 0,
  tokens_out: 0,
});

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    automationsAPI: {
      list: () =>
        ok([
          automation('au-p1', 'Review new requirements', { project_id: 'p1' }),
          automation('au-ws', 'Welcome the newcomer', { project_id: null }),
        ]),
      runNow: (id: string) => ok(id === 'au-ws' ? run('run-ws', null) : run('run-p1', 'p1')),
    },
    agentsAPI: { list: () => ok([{ id: 'agent-1', name: 'Greeter' }]) },
    crewsAPI: { list: () => ok([]) },
    agentRunsAPI: {
      get: (id: string) => ok(run(id, id === 'run-ws' ? null : 'p1')),
      tree: () => ok([]),
      logs: () => ok([]),
      streamUrl: (id: string) => `/stream/${id}`,
    },
  })
);

// The panel opens a stream for the run's log; the test needs none.
class StubEventSource {
  constructor(public url: string) {}
  addEventListener() {}
  close() {}
  onerror: unknown = null;
}

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
(globalThis as any).EventSource = StubEventSource as any;

const initialStore = useAppStore.getState();

let container: HTMLDivElement;
let root: Root;

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const rowOf = (name: string): HTMLTableRowElement => {
  const row = Array.from(document.body.querySelectorAll('tbody tr')).find((tr) =>
    (tr.querySelector('td')?.textContent ?? '').startsWith(name)
  );
  expect(row, `the row of ${name}`).toBeTruthy();
  return row as HTMLTableRowElement;
};

const runNow = async (name: string) => {
  const button = Array.from(rowOf(name).querySelectorAll('button')).find((b) => b.textContent === 'Run now');
  expect(button, `Run now on ${name}`).toBeTruthy();
  await act(async () => {
    button!.click();
  });
  await flush();
};

// Where shows the address the app is at.
const Where: React.FC<{ page: string }> = ({ page }) => {
  const location = useLocation();
  return <output data-testid="where">{page + ' ' + location.pathname + location.search}</output>;
};

const where = () => document.body.querySelector('[data-testid="where"]')?.textContent;

const runDetail = () => document.body.querySelector('[aria-label="Run detail"]');

const mount = async (at = '/projects/p1/automations') => {
  useAppStore.setState(
    {
      ...initialStore,
      projectId: 'p1',
      activeOrgId: 'o1',
      orgs: [{ id: 'o1', name: 'Shop', slug: 'shop', type: 'company', plan: 'business', role: 'admin', created_at: '' } as Org],
      features: { channel: 'nightly', stable_release: '', preview: false, features: { 'workspace-automations': true } },
    },
    true
  );
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={[at]}>
        <DialogProvider>
          <Routes>
            <Route
              path="/projects/:projectId/automations"
              element={
                <>
                  <AutomationsPage />
                  <Where page="automations" />
                </>
              }
            />
            <Route path="/projects/:projectId/agent-runs" element={<Where page="runs" />} />
          </Routes>
        </DialogProvider>
      </MemoryRouter>
    );
  });
  await flush();
};

beforeEach(() => {
  vi.mocked(agentRunsAPI.get).mockClear();
  (Element.prototype as any).scrollIntoView = vi.fn();
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

describe('AutomationsPage Run now', () => {
  it("opens a project automation's run on the project's Runs page", async () => {
    await mount();
    await runNow('Review new requirements');
    expect(where()).toBe('runs /projects/p1/agent-runs?run=run-p1');
  });

  it("opens a whole-workspace automation's run beside the automations, not on a project's Runs page", async () => {
    await mount();
    expect(runDetail()).toBeNull();
    await runNow('Welcome the newcomer');
    expect(where()).toBe('automations /projects/p1/automations?run=run-ws');
    expect(runDetail()).not.toBeNull();
    expect(vi.mocked(agentRunsAPI.get)).toHaveBeenCalledWith('run-ws');
    expect(runDetail()!.textContent).toContain('Greeter');
    // The automations stay listed beside it.
    expect(rowOf('Welcome the newcomer')).toBeTruthy();
  });

  it('keeps the run open on a reload, and closes it', async () => {
    await mount('/projects/p1/automations?run=run-ws');
    expect(runDetail()).not.toBeNull();
    const close = runDetail()!.querySelector('button[aria-label="Close run detail"]') as HTMLButtonElement | null;
    expect(close, 'the panel close button').toBeTruthy();
    await act(async () => {
      close!.click();
    });
    await flush();
    expect(runDetail()).toBeNull();
    expect(where()).toBe('automations /projects/p1/automations');
  });
});
