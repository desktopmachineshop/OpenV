import { mockApi, unstubbedCalls } from './mockApi';
import { PLANS, projectAPI, releaseAPI, resolveAvatarUrl } from '../api/client';
import * as client from '../api/client';

// The helper as a test file uses it, on the real client: one override, every
// other method stubbed.
vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    releaseAPI: { current: () => Promise.resolve({ data: { version: '9.9.9' } }) },
  })
);

// A stand-in client module, so a test can add a method the real client does
// not have.
const fakeClient = () => ({
  default: { get: () => 'the axios instance' },
  LIMIT: 50,
  helper: (x: string) => `real ${x}`,
  SETTINGS: { mode: 'real' },
  widgetAPI: {
    list: (projectId: string) => Promise.resolve({ data: [projectId] }),
    get: (id: string) => Promise.resolve({ data: id }),
    baseUrl: '/widgets',
  },
  gadgetAPI: {
    remove: (id: string) => Promise.resolve({ data: id }),
    // A group of methods nested in an *API object, as orgsAPI.members is.
    parts: {
      list: (gadgetId: string) => Promise.resolve({ data: [gadgetId] }),
      add: (gadgetId: string, part: string) => Promise.resolve({ data: part }),
    },
  },
});

const settles = async (value: unknown): Promise<boolean> => {
  let settled = false;
  Promise.resolve(value).then(
    () => (settled = true),
    () => (settled = true)
  );
  await new Promise((resolve) => setTimeout(resolve, 0));
  return settled;
};

beforeEach(() => {
  vi.clearAllMocks();
});

describe('mockApi', () => {
  it('stubs a method added to an *API object without any test naming it (d99fc18)', async () => {
    const real = fakeClient();
    const withNewMethod = { ...real, widgetAPI: { ...real.widgetAPI, archive: (id: string) => Promise.resolve({ data: id }) } };
    const mocked = mockApi<typeof withNewMethod>(withNewMethod);

    expect(vi.isMockFunction(mocked.widgetAPI.archive)).toBe(true);
    const answer = mocked.widgetAPI.archive('w-1');
    expect(await settles(answer)).toBe(false);
    expect(mocked.widgetAPI.archive).toHaveBeenCalledWith('w-1');
    expect(unstubbedCalls()).toEqual([{ name: 'widgetAPI.archive', args: ['w-1'] }]);
  });

  it('stubs every method of every *API object, each returning a promise that never settles', async () => {
    const mocked = mockApi<ReturnType<typeof fakeClient>>(fakeClient());
    for (const fn of [mocked.widgetAPI.list, mocked.widgetAPI.get, mocked.gadgetAPI.remove, mocked.gadgetAPI.parts.list]) {
      expect(vi.isMockFunction(fn)).toBe(true);
    }
    expect(await settles(mocked.widgetAPI.list('p-1'))).toBe(false);
    expect(await settles(mocked.gadgetAPI.remove('g-1'))).toBe(false);
  });

  it('stubs and overrides the methods of a group nested in an *API object', async () => {
    const mocked = mockApi<ReturnType<typeof fakeClient>>(fakeClient(), {
      gadgetAPI: { parts: { add: (gadgetId, part) => Promise.resolve({ data: `${gadgetId}/${part}` }) } },
    });
    expect(await settles(mocked.gadgetAPI.parts.list('g-1'))).toBe(false);
    await expect(mocked.gadgetAPI.parts.add('g-1', 'lid')).resolves.toEqual({ data: 'g-1/lid' });
    expect(mocked.gadgetAPI.parts.add).toHaveBeenCalledWith('g-1', 'lid');
    expect(vi.isMockFunction(mocked.gadgetAPI.remove)).toBe(true);
    expect(unstubbedCalls()).toEqual([{ name: 'gadgetAPI.parts.list', args: ['g-1'] }]);
  });

  it('runs an override instead of the stub, readable as a mock', async () => {
    const existing = vi.fn(() => Promise.resolve({ data: 'from a vi.fn' }));
    const mocked = mockApi<ReturnType<typeof fakeClient>>(fakeClient(), {
      widgetAPI: { list: (projectId) => Promise.resolve({ data: [`override ${projectId}`] }), get: existing },
    });

    await expect(mocked.widgetAPI.list('p-1')).resolves.toEqual({ data: ['override p-1'] });
    expect(mocked.widgetAPI.list).toHaveBeenCalledWith('p-1');
    expect(mocked.widgetAPI.get).toBe(existing);
    await expect(mocked.widgetAPI.get('w-1')).resolves.toEqual({ data: 'from a vi.fn' });
    expect(vi.isMockFunction(mocked.gadgetAPI.remove)).toBe(true);
    expect(unstubbedCalls()).toEqual([]);
  });

  it('keeps an override implementation through vi.clearAllMocks and vi.resetAllMocks', async () => {
    const mocked = mockApi<ReturnType<typeof fakeClient>>(fakeClient(), {
      widgetAPI: { get: (id) => Promise.resolve({ data: `kept ${id}` }) },
    });
    vi.clearAllMocks();
    await expect(mocked.widgetAPI.get('a')).resolves.toEqual({ data: 'kept a' });
    vi.resetAllMocks();
    await expect(mocked.widgetAPI.get('b')).resolves.toEqual({ data: 'kept b' });
  });

  it('leaves non-function exports, non-*API exports and non-function members real', () => {
    const real = fakeClient();
    const mocked = mockApi<typeof real>(real);
    expect(mocked.default).toBe(real.default);
    expect(mocked.LIMIT).toBe(50);
    expect(mocked.helper).toBe(real.helper);
    expect(mocked.SETTINGS).toBe(real.SETTINGS);
    expect(mocked.widgetAPI.baseUrl).toBe('/widgets');
    expect(real.widgetAPI.list).not.toBe(mocked.widgetAPI.list);
  });

  it('lets an override replace a non-*API export', () => {
    const mocked = mockApi<ReturnType<typeof fakeClient>>(fakeClient(), {
      LIMIT: 5,
      helper: (x) => `fake ${x}`,
    });
    expect(mocked.LIMIT).toBe(5);
    expect(mocked.helper('a')).toBe('fake a');
    expect(mocked.helper).toHaveBeenCalledWith('a');
  });

  it('refuses an override naming an export or a method the client does not have', () => {
    // Untyped, as a stale override in a test the type checker skips would be.
    const loose = (overrides: Record<string, unknown>) => () => mockApi<Record<string, unknown>>(fakeClient(), overrides);
    expect(loose({ missingAPI: { list: () => null } })).toThrow('mockApi: the client has no export named missingAPI');
    expect(loose({ widgetAPI: { gone: () => null } })).toThrow('mockApi: widgetAPI has no method named gone');
    expect(loose({ widgetAPI: () => null })).toThrow('mockApi: the override of widgetAPI must be an object of its methods');
    expect(loose({ gadgetAPI: { parts: { gone: () => null } } })).toThrow('mockApi: gadgetAPI.parts has no method named gone');
    expect(loose({ gadgetAPI: { parts: () => null } })).toThrow(
      'mockApi: the override of gadgetAPI.parts must be an object of its methods'
    );
  });
});

describe('unstubbedCalls', () => {
  it('reports the calls that reached a default stub, in call order', () => {
    const mocked = mockApi<ReturnType<typeof fakeClient>>(fakeClient(), {
      widgetAPI: { get: () => Promise.resolve({ data: 'overridden' }) },
    });
    mocked.gadgetAPI.remove('g-1');
    mocked.widgetAPI.get('w-1');
    mocked.widgetAPI.list('p-1');
    mocked.gadgetAPI.remove('g-2');
    expect(unstubbedCalls()).toEqual([
      { name: 'gadgetAPI.remove', args: ['g-1'] },
      { name: 'widgetAPI.list', args: ['p-1'] },
      { name: 'gadgetAPI.remove', args: ['g-2'] },
    ]);
  });

  it('leaves out a call answered by an implementation the test installed on a stub', async () => {
    const mocked = mockApi<ReturnType<typeof fakeClient>>(fakeClient());
    mocked.widgetAPI.list('before');
    vi.mocked(mocked.widgetAPI.list).mockResolvedValue({ data: ['installed'] });
    await expect(mocked.widgetAPI.list('after')).resolves.toEqual({ data: ['installed'] });
    expect(unstubbedCalls()).toEqual([{ name: 'widgetAPI.list', args: ['before'] }]);
  });

  it('resets with the stubs: vi.clearAllMocks, mockClear, and mockReset back to pending', async () => {
    const mocked = mockApi<ReturnType<typeof fakeClient>>(fakeClient());
    mocked.widgetAPI.list('p-1');
    mocked.gadgetAPI.remove('g-1');
    vi.mocked(mocked.gadgetAPI.remove).mockClear();
    expect(unstubbedCalls()).toEqual([{ name: 'widgetAPI.list', args: ['p-1'] }]);
    vi.clearAllMocks();
    expect(unstubbedCalls()).toEqual([]);

    vi.mocked(mocked.widgetAPI.get).mockResolvedValue({ data: 'installed' });
    vi.mocked(mocked.widgetAPI.get).mockReset();
    const answer = mocked.widgetAPI.get('w-1');
    expect(await settles(answer)).toBe(false);
    expect(unstubbedCalls()).toEqual([{ name: 'widgetAPI.get', args: ['w-1'] }]);
  });
});

describe('mockApi on the real client, through vi.mock', () => {
  it('stubs every method of every *API export and keeps every other export real', async () => {
    const actual = await vi.importActual<Record<string, unknown>>('../api/client');
    const mocked = client as unknown as Record<string, unknown>;
    const apis = Object.keys(actual).filter((k) => k.endsWith('API'));
    expect(apis.length).toBeGreaterThan(50);
    let methods = 0;
    const walk = (real: object, stubbed: Record<string, unknown>) => {
      for (const [name, member] of Object.entries(real)) {
        if (typeof member === 'object' && member !== null) walk(member, stubbed[name] as Record<string, unknown>);
        else if (typeof member === 'function') {
          expect(vi.isMockFunction(stubbed[name])).toBe(true);
          methods++;
        } else expect(stubbed[name]).toBe(member);
      }
    };
    for (const key of Object.keys(actual)) {
      if (apis.includes(key)) walk(actual[key] as object, mocked[key] as Record<string, unknown>);
      else expect(mocked[key]).toBe(actual[key]);
    }
    expect(methods).toBeGreaterThan(290);
    expect(vi.isMockFunction(client.orgsAPI.members.list)).toBe(true);
  });

  it('answers through the override and pends elsewhere', async () => {
    await expect(releaseAPI.current()).resolves.toEqual({ data: { version: '9.9.9' } });
    expect(await settles(projectAPI.get('p-1'))).toBe(false);
    expect(unstubbedCalls()).toEqual([{ name: 'projectAPI.get', args: ['p-1'] }]);
    expect(PLANS.length).toBeGreaterThan(0);
    expect(vi.isMockFunction(resolveAvatarUrl)).toBe(false);
  });

  // A factory as vi.mock(path, factory) types it: one that may return
  // anything, so nothing but mockApi's default ties the overrides to the
  // client (#379, bug 111). Each @ts-expect-error is checked by tsc, which
  // fails on one that no longer marks an error.
  type Factory = Extract<NonNullable<Parameters<typeof vi.mock>[1]>, (...args: any[]) => unknown>;
  const original = <T,>() => vi.importActual<T>('../api/client');

  it('type-checks the overrides against the client inside a vi.mock factory', async () => {
    const typed: Factory = async (orig) =>
      mockApi(await orig(), { projectAPI: { get: (id) => Promise.resolve({ data: id.toUpperCase() }) } });
    const staleExport: Factory = async (orig) =>
      mockApi(await orig(), {
        // @ts-expect-error -- the client has no export named missingAPI
        missingAPI: { list: () => Promise.resolve({ data: [] }) },
      });
    const staleMethod: Factory = async (orig) =>
      mockApi(await orig(), {
        // @ts-expect-error -- releaseAPI has no method named latest
        releaseAPI: { latest: () => Promise.resolve({ data: {} }) },
      });
    const wrongArguments: Factory = async (orig) =>
      mockApi(await orig(), {
        // @ts-expect-error -- projectAPI.get takes a string id
        projectAPI: { get: (id: number) => Promise.resolve({ data: id }) },
      });

    const mocked = (await typed(original)) as typeof client;
    await expect(mocked.projectAPI.get('p-1')).resolves.toEqual({ data: 'P-1' });
    await expect(staleExport(original)).rejects.toThrow('mockApi: the client has no export named missingAPI');
    await expect(staleMethod(original)).rejects.toThrow('mockApi: releaseAPI has no method named latest');
    expect(vi.isMockFunction(((await wrongArguments(original)) as typeof client).projectAPI.get)).toBe(true);
  });
});
