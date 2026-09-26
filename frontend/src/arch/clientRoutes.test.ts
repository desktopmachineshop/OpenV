// Regenerate the snapshot (only in a PR that means to change the client):
//   npx vitest run src/arch -u
//
// Every request the API layer can make is a route the server registers
// (invariant I23): each METHOD PATH found in src/api is looked up in
// internal/api/testdata/routes.txt, the server's own route inventory. The
// snapshot pins which wrapper calls which route, so a wrapper that moves
// between files (F1) leaves it unchanged while a changed path does not.
import { describe, expect, it, vi } from 'vitest';
import { clientCallSites, matchSite, serverRoutes, strayApiLiterals, type CallSite } from './clientCalls';
import { REGENERATE } from './repo';

// Parsing and type-checking the source tree takes seconds; allow for a loaded CI runner.
vi.setConfig({ testTimeout: 60_000 });

describe('API client paths', () => {
  const sites = clientCallSites();
  const routes = serverRoutes();

  it('finds the call sites at all', () => {
    expect(sites.filter((s) => s.via === 'axios').length).toBeGreaterThan(250);
    expect(routes.length).toBeGreaterThan(300);
  });

  it('calls only routes the server registers', () => {
    const missing = sites
      .filter((s) => matchSite(s, routes).paths.length === 0)
      .map((s) => `${s.method} ${s.template}  (${s.owner}, ${s.at})`);
    expect(missing).toEqual([]);
  });

  it('builds every /api path through a call site this guard can read', () => {
    expect(strayApiLiterals()).toEqual([]);
  });

  it('matches the pinned wrapper-to-route table', async () => {
    const key = (s: CallSite) => {
      const m = matchSite(s, routes);
      return `${s.method} ${m.paths.join(' | ') || `${s.template} (NOT IN routes.txt)`}`;
    };
    const grouped = new Map<string, Set<string>>();
    const notes = new Set<string>();
    for (const s of sites) {
      const k = key(s);
      const owner = s.via === 'axios' ? s.owner : `${s.owner} (${s.via})`;
      if (!grouped.has(k)) grouped.set(k, new Set());
      grouped.get(k)!.add(owner);
      const n = matchSite(s, routes).normalised;
      if (n.length) notes.add(`${owner}: ${n.join('; ')}`);
    }
    const keys = [...grouped.keys()].sort((a, b) => {
      const [ma, pa] = a.split(' ');
      const [mb, pb] = b.split(' ');
      return pa === pb ? (ma < mb ? -1 : ma > mb ? 1 : 0) : pa < pb ? -1 : 1;
    });
    const distinct = (list: CallSite[]) => new Set(list.map((s) => s.site)).size;
    const count = (via: CallSite['via']) => distinct(sites.filter((s) => s.via === via));
    const byMethod = [...new Set(sites.filter((s) => s.via === 'axios').map((s) => s.method))]
      .sort()
      .map((m) => `${m} ${distinct(sites.filter((s) => s.via === 'axios' && s.method === m))}`)
      .join(', ');
    const normalisedSites = sites.filter((s) => matchSite(s, routes).normalised.length > 0);
    const used = new Set(sites.flatMap((s) => matchSite(s, routes).paths.map((p) => `${s.method} ${p}`)));
    const lines = [
      '# METHOD PATH pairs the frontend API layer calls, matched to internal/api/testdata/routes.txt (invariant I23).',
      `# Regenerate: ${REGENERATE}`,
      '',
      `call sites: ${distinct(sites)}`,
      `  axios instance calls: ${count('axios')} (${byMethod})`,
      `  helper calls (a function that passes its argument to the instance): ${count('helper')}`,
      `  plain URLs built on API_BASE_URL (streams, downloads, logo, sign-in): ${count('url')}`,
      `  needing normalisation: ${distinct(normalisedSites)} (${distinct(normalisedSites.filter((s) => s.via === 'axios'))} of them axios calls)`,
      `distinct METHOD PATH lines below: ${keys.length}; server routes reached: ${used.size}`,
      '',
      ...keys.map((k) => {
        const [method, ...rest] = k.split(' ');
        return `${method.padEnd(7)} ${rest.join(' ').padEnd(64)}  ${[...grouped.get(k)!].sort().join(', ')}`;
      }),
      '',
      '## Normalised call sites (the path the client writes is not literally a route template)',
      ...[...notes].sort().map((n) => `  ${n}`),
    ];
    await expect(lines.join('\n') + '\n').toMatchFileSnapshot('./__snapshots__/clientRoutes.txt');
  });
});
