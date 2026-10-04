import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { mockApi } from './test/mockApi';
import App from './App';
import { useAppStore } from './state/store';
import type { OrgFeatures } from './api/client';

// App loads the active workspace's feature gates, and a load that fails
// hides every gated feature. It is tried again after 2 s, 5 s and 15 s, then
// no more on a timer; and once more whenever the window regains focus or the
// browser comes back online after a failed try. A try that succeeds sets the
// gates, which clears featuresFailed. A workspace switch cancels the old
// workspace's tries, so its answer never lands (#379 bug 176).

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

// Each gate load App starts, in order, with the answer the test gives it.
const loads: Array<{ orgId: string; answer: Deferred<{ data: OrgFeatures }> }> = [];

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
      features: (orgId) => {
        const answer = deferred<{ data: OrgFeatures }>();
        loads.push({ orgId, answer });
        return answer.promise;
      },
    },
  })
);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const gates = (orgId: string): OrgFeatures => ({
  channel: 'nightly',
  stable_release: '',
  preview: false,
  features: { [`${orgId}-feature`]: true },
});

const initialStore = useAppStore.getState();
let container: HTMLDivElement;
let root: Root;

const settle = async () => {
  await act(async () => {
    for (let i = 0; i < 10; i++) await Promise.resolve();
  });
};

const boot = async () => {
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={['/faq']}>
        <App />
      </MemoryRouter>
    );
  });
  await settle();
};

const fail = async (n: number) => {
  await act(async () => {
    loads[n].answer.reject(new Error('Network Error'));
  });
  await settle();
};

const succeed = async (n: number) => {
  await act(async () => {
    loads[n].answer.resolve({ data: gates(loads[n].orgId) });
  });
  await settle();
};

const wait = async (ms: number) => {
  await act(async () => {
    vi.advanceTimersByTime(ms);
  });
  await settle();
};

const fire = async (event: 'focus' | 'online') => {
  await act(async () => {
    window.dispatchEvent(new Event(event));
  });
  await settle();
};

const tried = () => loads.map((l) => l.orgId);
const state = () => {
  const { features, featuresFailed } = useAppStore.getState();
  return { features: features ? Object.keys(features.features) : null, featuresFailed };
};

beforeEach(() => {
  vi.useFakeTimers();
  sessionStorage.clear();
  localStorage.clear();
  loads.length = 0;
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
  vi.useRealTimers();
});

describe('a gate load that fails', () => {
  it('is tried again after 2 s, 5 s and 15 s, and then no more on a timer', async () => {
    await boot();
    expect(tried()).toEqual(['acme']);
    await fail(0);
    expect(state()).toEqual({ features: null, featuresFailed: true });

    await wait(1999);
    expect(loads).toHaveLength(1);
    await wait(1);
    expect(tried()).toEqual(['acme', 'acme']);
    await fail(1);
    await wait(4999);
    expect(loads).toHaveLength(2);
    await wait(1);
    expect(loads).toHaveLength(3);
    await fail(2);
    await wait(14999);
    expect(loads).toHaveLength(3);
    await wait(1);
    expect(loads).toHaveLength(4);
    await fail(3);

    await wait(10 * 60 * 1000);
    expect(loads).toHaveLength(4);
    expect(state()).toEqual({ features: null, featuresFailed: true });
  });

  it('brings the gates back with a try that succeeds, which clears featuresFailed', async () => {
    await boot();
    await fail(0);
    await wait(2000);
    expect(loads).toHaveLength(2);
    // Still failed while the try is on its way.
    expect(state()).toEqual({ features: null, featuresFailed: true });
    await succeed(1);
    expect(state()).toEqual({ features: ['acme-feature'], featuresFailed: false });

    // Nothing more to try.
    await wait(60 * 1000);
    await fire('focus');
    expect(loads).toHaveLength(2);
  });

  it('is tried once more when the window regains focus or the browser comes back online', async () => {
    await boot();
    await fail(0);
    for (const n of [1, 2, 3]) {
      await wait(15000);
      expect(loads).toHaveLength(n + 1);
      await fail(n);
    }
    expect(loads).toHaveLength(4);

    await fire('focus');
    expect(loads).toHaveLength(5);
    // Once: another focus while that try is on its way starts no other.
    await fire('focus');
    expect(loads).toHaveLength(5);
    await fail(4);
    // And no timer after it: the retries are spent.
    await wait(60 * 1000);
    expect(loads).toHaveLength(5);

    await fire('online');
    expect(loads).toHaveLength(6);
    await succeed(5);
    expect(state()).toEqual({ features: ['acme-feature'], featuresFailed: false });
    await fire('online');
    await fire('focus');
    expect(loads).toHaveLength(6);
  });

  it('is tried at once on focus during a wait, and the wait does not try again on top of it', async () => {
    await boot();
    await fail(0);
    await wait(1000);
    await fire('focus');
    expect(loads).toHaveLength(2);
    await fail(1);
    // The 2 s wait was replaced by the try on focus; the next is 5 s after it.
    await wait(4999);
    expect(loads).toHaveLength(2);
    await wait(1);
    expect(loads).toHaveLength(3);
  });

  it("a workspace switch cancels the old workspace's tries, and its late answer never lands", async () => {
    await boot();
    await fail(0);
    await wait(2000);
    expect(tried()).toEqual(['acme', 'acme']);

    // Switched while acme's retry is on its way.
    await act(async () => {
      useAppStore.getState().setActiveOrgId('bigco');
    });
    await settle();
    expect(tried()).toEqual(['acme', 'acme', 'bigco']);
    await succeed(1);
    expect(state()).toEqual({ features: null, featuresFailed: false });

    // Bigco's load fails: only bigco is tried again, on its own timer.
    await fail(2);
    expect(state()).toEqual({ features: null, featuresFailed: true });
    await wait(2000);
    expect(tried()).toEqual(['acme', 'acme', 'bigco', 'bigco']);
    await succeed(3);
    expect(state()).toEqual({ features: ['bigco-feature'], featuresFailed: false });
  });

  it("a workspace switch cancels the old workspace's waiting retry and its focus retry", async () => {
    await boot();
    await fail(0);
    await act(async () => {
      useAppStore.getState().setActiveOrgId('bigco');
    });
    await settle();
    expect(tried()).toEqual(['acme', 'bigco']);
    await succeed(1);

    await wait(60 * 1000);
    await fire('focus');
    await fire('online');
    expect(tried()).toEqual(['acme', 'bigco']);
    expect(state()).toEqual({ features: ['bigco-feature'], featuresFailed: false });
  });

  it("a workspace switch keeps the old workspace's failure from scheduling a retry", async () => {
    await boot();
    await act(async () => {
      useAppStore.getState().setActiveOrgId('bigco');
    });
    await settle();
    expect(tried()).toEqual(['acme', 'bigco']);
    // Acme's first load fails after the switch.
    await fail(0);
    await succeed(1);
    await wait(60 * 1000);
    expect(tried()).toEqual(['acme', 'bigco']);
    expect(state()).toEqual({ features: ['bigco-feature'], featuresFailed: false });
  });
});
