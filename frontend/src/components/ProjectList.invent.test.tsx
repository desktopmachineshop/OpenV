import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { ProjectList } from './ProjectList';
import { useAppStore } from '../state/store';
import { agentRunsAPI, type Org } from '../api/client';
import { DialogProvider } from './ui';

// An invention that outlasts its two-minute wait (#379 bug 168). Its run has
// no project, so "check Runs for progress" pointed at no page that listed
// it. Where the workspace has the workspace Runs page, the message links to
// the run there; without it, the message is as it was.

const ok = (data: unknown) => Promise.resolve({ data });

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    projectAPI: { list: () => ok([]) },
    templateAPI: { list: () => ok([]) },
    workerStatusAPI: { get: () => ok({ workers: [{ id: 'w1', online: true, revoked: false }], queue: { queued: 0 } }) },
    sharedProductsAPI: {
      list: () =>
        ok([
          {
            id: 'p1',
            category: 'kitchen appliance',
            name: 'Kevinproof',
            description: 'Kevinproof recognises Kevin and locks.',
            vision: 'Kevinproof becomes the reason the bean jar survives a Tuesday.',
            problem: 'Beans vanish overnight.',
            target_users: 'office workers whose beans keep leaving with Kevin',
            votes: 1,
            votes_week: 0,
            voted: false,
          },
        ]),
    },
    agentsAPI: {
      list: () => ok([{ id: 'agent-1', slug: 'requirements-copilot', name: 'Copilot' }]),
      launchRun: () => ok({ id: 'run-slow' }),
    },
    // The run never leaves the queue.
    agentRunsAPI: { get: (id: string) => ok({ id, status: 'queued', error: '' }) },
  })
);

// The chrome around the form is not what this test is about.
vi.mock('./Navbar', () => ({ Navbar: () => null }));
vi.mock('./HelpSidebar', () => ({ HelpSidebar: () => null }));
vi.mock('./DownloadWizard', () => ({ DownloadWizard: () => null }));
vi.mock('./CreateOrgModal', () => ({ CreateOrgModal: () => null }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const initialStore = useAppStore.getState();

let container: HTMLDivElement;
let root: Root;

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await Promise.resolve();
  });
};

const button = (label: string): HTMLButtonElement => {
  const found = Array.from(container.querySelectorAll('button')).find((b) => (b.textContent || '').trim() === label);
  if (!found) throw new Error(`no button ${label}`);
  return found as HTMLButtonElement;
};

const click = async (el: HTMLElement) => {
  await act(async () => {
    el.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });
  await flush();
};

// Open the new-project form in random-product mode and invent until the wait
// runs out.
const inventUntilTheWaitRunsOut = async (workspaceRuns: boolean) => {
  useAppStore.setState(
    {
      ...initialStore,
      activeOrgId: 'o1',
      orgs: [{ id: 'o1', name: 'Shop', slug: 'shop', type: 'company', plan: 'business', role: 'member', created_at: '' } as Org],
      features: {
        channel: workspaceRuns ? 'nightly' : 'stable',
        stable_release: '',
        preview: false,
        features: { 'workspace-runs': workspaceRuns },
      },
    },
    true
  );
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={['/projects']}>
        <DialogProvider>
          <ProjectList />
        </DialogProvider>
      </MemoryRouter>
    );
  });
  await flush();
  await click(button('+ New Project'));
  const select = container.querySelector('#mode') as HTMLSelectElement;
  const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!;
  await act(async () => {
    setter.call(select, 'random');
    select.dispatchEvent(new Event('change', { bubbles: true }));
  });
  await flush();
  await click(button('✨ Invent with agent'));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(125_000);
  });
  await flush();
  expect(vi.mocked(agentRunsAPI.get)).toHaveBeenCalledWith('run-slow');
};

beforeEach(() => {
  vi.useFakeTimers();
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  vi.useRealTimers();
  useAppStore.setState(initialStore, true);
});

describe('an invention that outlasts its wait', () => {
  it('links to its run on the workspace Runs page', async () => {
    await inventUntilTheWaitRunsOut(true);
    expect(container.textContent).toContain('The invention run is taking unusually long. Follow it in Workspace runs');
    const link = Array.from(container.querySelectorAll('a')).find((a) => a.textContent === 'Follow it in Workspace runs');
    expect(link?.getAttribute('href')).toBe('/org/runs?run=run-slow');
    expect(container.textContent).not.toContain('check Runs for progress');
  });

  it('says to check Runs, as before, where the workspace has no workspace Runs page', async () => {
    await inventUntilTheWaitRunsOut(false);
    expect(container.textContent).toContain('The invention run is taking unusually long — check Runs for progress.');
    expect(container.querySelector('a[href^="/org/runs"]')).toBeNull();
  });
});
