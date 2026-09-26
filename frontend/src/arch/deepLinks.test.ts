// Regenerate the snapshot (only in a PR that means to change routes):
//   npx vitest run src/arch -u
//
// Every in-app URL the Go backend builds (testdata/backendDeepLinks.json)
// must land on a real route of the app (invariant I19): resolved with
// react-router's own matchRoutes against the route objects parsed from
// App.tsx, for a visitor or member who is not behind the verification wall.
// A link that only reaches the catch-all would silently redirect to
// /projects, so it counts as broken.
import { matchRoutes } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';
import { loadDeepLinks, parseAppRoutes, toRouteObjects } from './appRoutes';
import { REGENERATE, readRepoFile, table } from './repo';

const CATCH_ALL = '/*';

function resolve(url: string): { leaf: string; chain: string[] } {
  const routes = toRouteObjects(parseAppRoutes().routes, { walled: false });
  const { pathname } = new URL(url, 'http://openv.test');
  const matches = matchRoutes(routes, pathname) || [];
  const chain = matches.map((m) => m.route.id || '?');
  return { leaf: chain[chain.length - 1] || '(no match)', chain };
}

// Parsing and type-checking the source tree takes seconds; allow for a loaded CI runner.
vi.setConfig({ testTimeout: 60_000 });

describe('backend-built deep links', () => {
  const links = loadDeepLinks();

  it('are still built by the Go code the fixture names', () => {
    const missing = links
      .filter((l) => !readRepoFile(l.source.replace(/:\d+$/, '')).includes(l.goLiteral))
      .map((l) => `${l.source}: ${l.goLiteral}`);
    expect(missing).toEqual([]);
  });

  it('each resolve to an app route (and the nginx-only ones to none)', () => {
    const broken = links
      .filter((l) => (resolve(l.url).leaf === CATCH_ALL) !== (l.spa === false))
      .map((l) => `${l.url} -> ${resolve(l.url).leaf} (${l.source})`);
    expect(broken).toEqual([]);
  });

  it('matches the pinned resolution table', async () => {
    const rows = links.map((l) => [l.url, `-> ${resolve(l.url).chain.join(' > ')}`, l.source]);
    const text = [
      '# Backend-built deep links resolved with matchRoutes against App.tsx (invariant I19).',
      `# Fixture: src/arch/testdata/backendDeepLinks.json. Regenerate: ${REGENERATE}`,
      `# ${links.length} links; ${links.filter((l) => l.spa === false).length} served by nginx from the API, not the app.`,
      '',
      ...table(rows),
    ].join('\n');
    await expect(text + '\n').toMatchFileSnapshot('./__snapshots__/deepLinks.txt');
  });
});
