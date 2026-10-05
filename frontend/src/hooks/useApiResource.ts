import { DependencyList, useCallback, useEffect, useRef, useState } from 'react';

// One way to load what a view shows (refactor plan X15): fetch on mount and
// whenever the workspace or a dependency changes, and drop an answer that
// lands after either has moved on. A view adopts it when it is split (X16,
// X18), not before.

export interface ApiResource<T> {
  /** The last answer, or undefined until the first one lands. It stays while a later fetch runs or fails. */
  data: T | undefined;
  /** What the last fetch threw, or null; a later answer clears it. */
  error: unknown;
  /** True from the start of each fetch until it settles. */
  loading: boolean;
  /** Fetches again, with the newest fetch function. */
  reload: () => void;
}

/**
 * Calls fetch(activeOrgId) on mount, and again whenever activeOrgId or an
 * entry of deps changes (compared with Object.is, as React compares an
 * effect's dependencies) or reload is called. The workspace is a parameter
 * of its own, not an entry the caller must remember: an org-scoped list is
 * scoped by the X-Org-ID header the API client sends, and must refetch when
 * the active workspace switches under it (ProjectLayout's deep-link sync,
 * issues #99, #111 and #112), even though the fetch never reads the id.
 *
 * Each fetch has a cancelled flag: an answer that lands after the next
 * fetch has started, or after unmount, is dropped. Nothing is cached: every
 * mount fetches. fetch may be a new function on every render; the newest is
 * the one called.
 */
export function useApiResource<T>(
  fetch: (activeOrgId: string) => Promise<T>,
  activeOrgId: string,
  deps: DependencyList = []
): ApiResource<T> {
  const fetchRef = useRef(fetch);
  fetchRef.current = fetch;

  const [state, setState] = useState<{ data: T | undefined; error: unknown; loading: boolean }>({
    data: undefined,
    error: null,
    loading: true,
  });
  const [reloads, setReloads] = useState(0);
  const reload = useCallback(() => setReloads((n) => n + 1), []);
  const depsVersion = useChangeCount(deps);

  useEffect(() => {
    let cancelled = false;
    setState((s) => (s.loading ? s : { ...s, loading: true }));
    fetchRef.current(activeOrgId).then(
      (data) => {
        if (!cancelled) setState({ data, error: null, loading: false });
      },
      (error: unknown) => {
        if (!cancelled) setState((s) => ({ data: s.data, error, loading: false }));
      }
    );
    return () => {
      cancelled = true;
    };
  }, [activeOrgId, depsVersion, reloads]);

  return { ...state, reload };
}

/** How many times deps has changed since mount, entry by entry with Object.is. */
function useChangeCount(deps: DependencyList): number {
  const seen = useRef({ deps, count: 0 });
  const prev = seen.current.deps;
  if (prev.length !== deps.length || prev.some((d, i) => !Object.is(d, deps[i]))) {
    seen.current = { deps, count: seen.current.count + 1 };
  }
  return seen.current.count;
}
