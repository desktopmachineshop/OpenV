import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { ApiResource, useApiResource } from './useApiResource';

// useApiResource (refactor plan X15): no view uses it yet; X16 and X18 adopt
// it in the views they split. It is held to what those views' hand-written
// loads do today (AgentsPage.tsx:48-63, AutomationsPage.tsx:69-89): fetch on
// mount, again when the active workspace or a dependency changes, keep the
// last answer on a failure, and ignore an answer that lands too late.

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

/** A promise the test settles by hand. */
function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

interface ProbeProps {
  fetch: (orgId: string) => Promise<string>;
  orgId: string;
  deps?: unknown[];
}

let seen: ApiResource<string>;
const Probe: React.FC<ProbeProps> = ({ fetch, orgId, deps }) => {
  seen = useApiResource(fetch, orgId, deps);
  return null;
};

let container: HTMLDivElement;
let root: Root;
let mounted = false;

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
  mounted = true;
});

afterEach(() => {
  if (mounted) unmount();
  container.remove();
});

const render = (props: ProbeProps) => {
  act(() => {
    root.render(<Probe {...props} />);
  });
};

const unmount = () => {
  act(() => {
    root.unmount();
  });
  mounted = false;
};

/** A fetch whose every call the test answers by hand, in call order. */
const manualFetch = () => {
  const calls: { orgId: string; answer: ReturnType<typeof deferred<string>> }[] = [];
  const fetch = vi.fn((orgId: string) => {
    const answer = deferred<string>();
    calls.push({ orgId, answer });
    return answer.promise;
  });
  return { fetch, calls };
};

const settle = async (fn: () => void) => {
  await act(async () => {
    fn();
  });
};

describe('useApiResource', () => {
  it('fetches on mount with the active workspace, loading until the answer lands', async () => {
    const { fetch, calls } = manualFetch();
    render({ fetch, orgId: 'org-1' });
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(calls[0].orgId).toBe('org-1');
    expect(seen).toMatchObject({ data: undefined, error: null, loading: true });

    await settle(() => calls[0].answer.resolve('agents of org-1'));
    expect(seen).toMatchObject({ data: 'agents of org-1', error: null, loading: false });
  });

  it('does not fetch again on a render that changes nothing, even with a new fetch function', async () => {
    const { fetch, calls } = manualFetch();
    render({ fetch, orgId: 'org-1', deps: ['p-1'] });
    await settle(() => calls[0].answer.resolve('one'));
    render({ fetch: (orgId) => fetch(orgId), orgId: 'org-1', deps: ['p-1'] });
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(seen.data).toBe('one');
  });

  it('fetches again when the active workspace switches', async () => {
    const { fetch, calls } = manualFetch();
    render({ fetch, orgId: 'org-1' });
    await settle(() => calls[0].answer.resolve('agents of org-1'));

    render({ fetch, orgId: 'org-2' });
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(calls[1].orgId).toBe('org-2');
    // The last answer stays on screen until the next one lands.
    expect(seen).toMatchObject({ data: 'agents of org-1', loading: true });
    await settle(() => calls[1].answer.resolve('agents of org-2'));
    expect(seen).toMatchObject({ data: 'agents of org-2', loading: false });
  });

  it('fetches again when a dependency changes, with the newest fetch function', async () => {
    const first = manualFetch();
    render({ fetch: first.fetch, orgId: 'org-1', deps: ['p-1'] });
    await settle(() => first.calls[0].answer.resolve('p-1'));

    const second = manualFetch();
    render({ fetch: second.fetch, orgId: 'org-1', deps: ['p-2'] });
    expect(first.fetch).toHaveBeenCalledTimes(1);
    expect(second.fetch).toHaveBeenCalledTimes(1);
    await settle(() => second.calls[0].answer.resolve('p-2'));
    expect(seen.data).toBe('p-2');
  });

  it('drops an answer that lands after the next fetch has started (the cancelled flag)', async () => {
    const { fetch, calls } = manualFetch();
    render({ fetch, orgId: 'org-1' });
    render({ fetch, orgId: 'org-2' });
    await settle(() => calls[1].answer.resolve('agents of org-2'));
    await settle(() => calls[0].answer.resolve('agents of org-1'));
    expect(seen).toMatchObject({ data: 'agents of org-2', loading: false });
  });

  it('drops a failure that lands after the next fetch has started', async () => {
    const { fetch, calls } = manualFetch();
    render({ fetch, orgId: 'org-1' });
    render({ fetch, orgId: 'org-2' });
    await settle(() => calls[1].answer.resolve('agents of org-2'));
    await settle(() => calls[0].answer.reject(new Error('late')));
    expect(seen).toMatchObject({ data: 'agents of org-2', error: null });
  });

  it('keeps the last answer when a fetch fails, and clears the error with the next answer', async () => {
    const { fetch, calls } = manualFetch();
    render({ fetch, orgId: 'org-1' });
    await settle(() => calls[0].answer.resolve('one'));

    const failure = new Error('Failed to load agents');
    await settle(() => seen.reload());
    await settle(() => calls[1].answer.reject(failure));
    expect(seen).toMatchObject({ data: 'one', error: failure, loading: false });

    await settle(() => seen.reload());
    await settle(() => calls[2].answer.resolve('two'));
    expect(seen).toMatchObject({ data: 'two', error: null, loading: false });
  });

  it('fetches again on reload', async () => {
    const { fetch, calls } = manualFetch();
    render({ fetch, orgId: 'org-1' });
    await settle(() => calls[0].answer.resolve('one'));
    await settle(() => seen.reload());
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(calls[1].orgId).toBe('org-1');
    expect(seen.loading).toBe(true);
  });

  it('sets nothing once unmounted', async () => {
    const errors = vi.spyOn(console, 'error').mockImplementation(() => undefined);
    const { fetch, calls } = manualFetch();
    render({ fetch, orgId: 'org-1' });
    const before = seen;
    unmount();
    await settle(() => calls[0].answer.resolve('too late'));
    expect(seen).toBe(before);
    expect(errors).not.toHaveBeenCalled();
    errors.mockRestore();
  });

  it('caches nothing: a new mount fetches again', async () => {
    const { fetch, calls } = manualFetch();
    render({ fetch, orgId: 'org-1' });
    await settle(() => calls[0].answer.resolve('one'));
    unmount();
    act(() => {
      root = createRoot(container);
    });
    mounted = true;
    render({ fetch, orgId: 'org-1' });
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(seen).toMatchObject({ data: undefined, loading: true });
  });
});
