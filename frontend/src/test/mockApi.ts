// An auto-stubbing mock of the API client for vitest (refactor plan F2).
//
//   import { mockApi } from '../test/mockApi';
//   vi.mock('../api/client', async (orig) =>
//     mockApi(await orig(), {
//       projectAPI: { get: () => Promise.resolve({ data: project }) },
//     })
//   );
//
// Import mockApi above the module under test: vi.mock is hoisted, and its
// factory runs when that module first loads the client.
//
// Every method of every exported *API object, a group of methods nested in
// one (orgsAPI.members) included, becomes a vi.fn. A method the overrides
// name runs the override (wrapped in vi.fn, unless it already is a mock, so a
// test can read its calls); any other method records its call and returns a
// promise that never settles, so a request the test does not care about
// leaves the component loading instead of throwing, and a method added to
// the client later is stubbed without touching a test (the d99fc18 failure
// mode). Read a stub through the mocked module, as before:
// vi.mocked(projectAPI.get).mockResolvedValue(...).
//
// Everything else the client exports (constants, helpers, the axios
// instance) stays real unless an override names it. The overrides are typed
// against the client, so tsc refuses an override naming an export or a
// method the client lacks, or taking other arguments than the real method;
// at run time such a name throws as well, for a test the type checker skips.

import { vi, type Mock } from 'vitest';
import type * as Client from '../api/client';

type AnyFn = (...args: any[]) => any;

// What a test may override: any export, and per *API object any of its
// methods. A method override takes the real method's parameters (so its
// arguments are typed) and may return anything, since a test's fixture is
// rarely a whole AxiosResponse.
type Override<T> = T extends (...args: infer A) => any ? (...args: A) => unknown : unknown;
type Methods<T> = {
  [P in keyof T]?: T[P] extends AnyFn ? Override<T[P]> : T[P] extends object ? Methods<T[P]> : unknown;
};
export type ApiOverrides<M = typeof Client> = {
  [K in keyof M]?: K extends `${string}API` ? Methods<M[K]> : Override<M[K]>;
};

export interface UnstubbedCall {
  /** The stubbed method, as `xxxAPI.method`. */
  name: string;
  args: unknown[];
}

// The promises the default stubs hand out, to tell their calls apart from
// those answered by an implementation a test installed later.
const pending = new WeakSet<object>();
const stubs: Array<{ name: string; fn: Mock }> = [];

function autoStub(name: string): Mock {
  const fn = vi.fn((..._args: unknown[]): Promise<never> => {
    const never = new Promise<never>(() => {});
    pending.add(never);
    return never;
  });
  stubs.push({ name, fn });
  return fn;
}

const has = (object: object, key: string) => Object.prototype.hasOwnProperty.call(object, key);

const asMock = (value: unknown): unknown =>
  typeof value === 'function' && !vi.isMockFunction(value) ? vi.fn(value as AnyFn) : value;

const isGroup = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value);

// A copy of one *API object, or a group nested in one, named path, with
// its methods stubbed or overridden; a member that is neither stays real.
function stubMethods(path: string, real: Record<string, unknown>, given: unknown): Record<string, unknown> {
  if (!isGroup(given)) throw new Error(`mockApi: the override of ${path} must be an object of its methods`);
  for (const name of Object.keys(given)) {
    if (!has(real, name)) throw new Error(`mockApi: ${path} has no method named ${name}`);
  }
  const stubbed: Record<string, unknown> = {};
  for (const [name, member] of Object.entries(real)) {
    const at = `${path}.${name}`;
    if (isGroup(member)) stubbed[name] = stubMethods(at, member, has(given, name) ? given[name] : {});
    else if (has(given, name)) stubbed[name] = asMock(given[name]);
    else stubbed[name] = typeof member === 'function' ? autoStub(at) : member;
  }
  return stubbed;
}

/**
 * Returns the client module `actual` with every method of every `*API`
 * object stubbed: `overrides` supplies implementations, every other method
 * records its call and returns a promise that never settles.
 *
 * `M` is the client module unless a caller names another. Neither argument
 * nor the return type infers it: inside `vi.mock`, whose factory may return
 * anything, inferring from the return type made `M` `{}` and left the
 * overrides unchecked (#379, bug 111).
 */
export function mockApi<M = typeof Client>(actual: unknown, overrides: ApiOverrides<NoInfer<M>> = {}): NoInfer<M> {
  const real = actual as Record<string, unknown>;
  const given = overrides as Record<string, unknown>;
  for (const key of Object.keys(given)) {
    if (!has(real, key)) throw new Error(`mockApi: the client has no export named ${key}`);
  }
  const mocked: Record<string, unknown> = { ...real };
  for (const [key, value] of Object.entries(real)) {
    if (key.endsWith('API') && isGroup(value)) mocked[key] = stubMethods(key, value, has(given, key) ? given[key] : {});
    else if (has(given, key)) mocked[key] = asMock(given[key]);
  }
  return mocked as M;
}

/**
 * The calls that reached a default stub (a method with no override and no
 * implementation installed since), in call order. It reads the stubs' own
 * call records, so whatever clears those clears it: `vi.clearAllMocks()`,
 * `vi.resetAllMocks()`, or `mockClear()` / `mockReset()` on one stub.
 *
 *   afterEach(() => expect(unstubbedCalls()).toEqual([]));
 */
export function unstubbedCalls(): UnstubbedCall[] {
  const calls: Array<UnstubbedCall & { order: number }> = [];
  for (const { name, fn } of stubs) {
    fn.mock.results.forEach((result, i) => {
      if (result.type === 'return' && pending.has(result.value)) {
        calls.push({ name, args: fn.mock.calls[i], order: fn.mock.invocationCallOrder[i] });
      }
    });
  }
  return calls.sort((a, b) => a.order - b.order).map(({ name, args }) => ({ name, args }));
}
