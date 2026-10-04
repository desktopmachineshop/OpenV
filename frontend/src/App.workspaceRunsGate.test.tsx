import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { mockApi } from './test/mockApi';
import App from './App';
import { useAppStore } from './state/store';
import { orgsAPI, type OrgFeatures } from './api/client';

// The Workspace runs page waits for the workspace's feature gates, which App
// loads, and without workspace-runs sends a member to the projects list. A
// gate load that fails counts as the feature off, as it hides gated UI
// elsewhere: the page leaves for the projects list instead of saying
// "Loading…" for good, while a load still on its way keeps it waiting
// (#379 bug 174).

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
      list: () => ok({ orgs: [{ id: 'acme', name: 'Acme', type: 'company', role: 'member' }], active_org: 'acme' }),
      features: () => gatesLoad.promise,
    },
  })
);

// The page's title says it is the one on screen, and not the route's
// Suspense fallback, which says "Loading…" too.
vi.mock('./components/Navbar', () => ({ Navbar: ({ title }: { title?: string }) => <h1>{title}</h1> }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const initialStore = useAppStore.getState();
let container: HTMLDivElement;
let root: Root;

const Where: React.FC = () => {
  const location = useLocation();
  return <output data-testid="where">{location.pathname}</output>;
};
const where = () => container.querySelector('[data-testid="where"]')?.textContent;
const heading = () => container.querySelector('h1')?.textContent;

const settle = async () => {
  await act(async () => {
    for (let i = 0; i < 20; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const boot = async () => {
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={['/org/runs']}>
        <App />
        <Where />
      </MemoryRouter>
    );
  });
  await settle();
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
  act(() => root.unmount());
  container.remove();
  useAppStore.setState(initialStore, true);
  sessionStorage.clear();
  localStorage.clear();
});

describe('the Workspace runs page when the gates fail to load', () => {
  it('waits while the gates are on their way, and leaves for the projects list once they fail', async () => {
    await boot();
    expect(vi.mocked(orgsAPI.features)).toHaveBeenCalledWith('acme');
    expect(where()).toBe('/org/runs');
    expect(heading()).toBe('Workspace runs');
    expect(container.textContent).toContain('Loading…');

    await act(async () => {
      gatesLoad.reject(new Error('Network Error'));
    });
    await settle();
    expect(where()).toBe('/projects');
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
