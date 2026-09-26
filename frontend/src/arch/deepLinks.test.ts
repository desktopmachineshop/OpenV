// Regenerate the snapshot (only in a PR that means to change routes):
//   npx vitest run src/arch -u
//
// Every in-app URL the Go backend builds (testdata/backendDeepLinks.json)
// must land on a real route of the app (invariant I19): resolved with
// react-router's own matchRoutes against the route objects parsed from
// App.tsx, for a visitor or member who is not behind the verification wall.
// A link that only reaches the catch-all would silently redirect to
// /projects, so it counts as broken. The snapshot holds URLs and routes only:
// where the Go code builds a link is in the fixture and in failure messages,
// so a file split on the Go side (M8) leaves it unchanged.
import path from 'node:path';
import { matchRoutes } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';
import { loadDeepLinks, parseAppRoutes, toRouteObjects, type DeepLink } from './appRoutes';
import { REGENERATE, goSources, readRepoFile, table } from './repo';

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

  // Bound to the directory of the file the fixture names, not the file, so a
  // class A split (M8 moves the interview handlers out of suite_handlers.go)
  // leaves the guard green. Counting keeps it exact: `dest = "/"` is built
  // twice in internal/api, and the fixture lists both.
  it('are still built by the Go code under the directory the fixture names', () => {
    const listed = new Map<string, DeepLink[]>();
    for (const l of links) {
      const key = `${path.posix.dirname(l.source)}\t${l.goLiteral}`;
      listed.set(key, [...(listed.get(key) || []), l]);
    }
    const wrong = [...listed].flatMap(([key, entries]) => {
      const [dir, literal] = key.split('\t');
      const found = goSources(dir).flatMap((file) => {
        const text = readRepoFile(file);
        const at: string[] = [];
        for (let i = text.indexOf(literal); i >= 0; i = text.indexOf(literal, i + literal.length)) {
          at.push(`${file}:${text.slice(0, i).split('\n').length}`);
        }
        return at;
      });
      if (found.length === entries.length) return [];
      return [
        `${literal} is built ${found.length}x under ${dir} (${found.join(', ') || 'nowhere'}); ` +
          `the fixture lists it ${entries.length}x (${entries.map((l) => l.source).join(', ')})`,
      ];
    });
    expect(wrong).toEqual([]);
  });

  it('each resolve to an app route (and the nginx-only ones to none)', () => {
    const broken = links
      .filter((l) => (resolve(l.url).leaf === CATCH_ALL) !== (l.spa === false))
      .map((l) => `${l.url} -> ${resolve(l.url).leaf} (${l.source})`);
    expect(broken).toEqual([]);
  });

  it('matches the pinned resolution table', async () => {
    const rows = links.map((l) => [l.url, `-> ${resolve(l.url).chain.join(' > ')}`]);
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
