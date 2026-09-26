// Regenerate the snapshot (only in a PR that means to change what is public):
//   npx vitest run src/arch -u
//
// Public pages must work without a session (OpenV REQ-114, invariant I19).
// Two lists decide that and nothing tied them together:
//   - utils/publicPaths.ts isPublicPath: pages the 401 interceptor never
//     redirects away from;
//   - internal/api/authmiddleware.go isOpenPath: API paths the server
//     answers without a session.
// This guard checks each public route of App.tsx against isPublicPath, and
// every API call a public page's module graph can make against isOpenPath
// (parsed from the Go source, so a change on either side is seen).
import path from 'node:path';
import ts from 'typescript';
import { describe, expect, it, vi } from 'vitest';
import { isPublicPath } from '../utils/publicPaths';
import { componentsOf, flattenRoutes, parseAppRoutes, type FlatRoute } from './appRoutes';
import { clientCallSites } from './clientCalls';
import { REGENERATE, SRC, literalText, parseFile, productionSources, readRepoFile, resolveModule, srcRel, walk } from './repo';

const GO_FILE = 'internal/api/authmiddleware.go';
const CLIENT = path.join(SRC, 'api/client.ts');

/** isOpenPath's rule, read from the Go function body. */
function goOpenRule(): { exact: string[]; prefixes: string[] } {
  const src = readRepoFile(GO_FILE);
  const body = src.match(/func isOpenPath\(path string\) bool \{\n([\s\S]*?)\n\}/);
  if (!body) throw new Error(`${GO_FILE}: func isOpenPath not found`);
  const exact = [...body[1].matchAll(/path == "([^"]*)"/g)].map((m) => m[1]);
  const prefixes = [...body[1].matchAll(/strings\.HasPrefix\(path, "([^"]*)"\)/g)].map((m) => m[1]);
  return { exact, prefixes };
}

function isOpenApiPath(p: string): boolean {
  const rule = goOpenRule();
  return rule.exact.includes(p) || rule.prefixes.some((prefix) => p.startsWith(prefix));
}

/** The PUBLIC_SEGMENTS array literal in utils/publicPaths.ts. */
function publicSegments(): string[] {
  const out: string[] = [];
  walk(parseFile(path.join(SRC, 'utils/publicPaths.ts')), (node) => {
    if (ts.isVariableDeclaration(node) && node.name.getText() === 'PUBLIC_SEGMENTS' && node.initializer && ts.isArrayLiteralExpression(node.initializer)) {
      node.initializer.elements.forEach((e) => out.push(literalText(e) ?? `<${e.getText()}>`));
    }
  });
  return out;
}

/** A concrete URL for a route pattern: each :param becomes "x". */
const example = (fullPath: string) => fullPath.replace(/:[^/]+/g, 'x');

/** Every `xxxAPI.member` a module and its local imports (src/api excluded) reference. */
function apiReferences(entry: string): Set<string> {
  const refs = new Set<string>();
  const seen = new Set<string>();
  const queue = [entry];
  while (queue.length) {
    const file = queue.shift()!;
    if (seen.has(file)) continue;
    seen.add(file);
    const sf = parseFile(file);
    const fromClient = new Set<string>();
    walk(sf, (node) => {
      let spec: string | null = null;
      if ((ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) && node.moduleSpecifier) spec = literalText(node.moduleSpecifier);
      if (ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword) spec = literalText(node.arguments[0]);
      const target = spec ? resolveModule(file, spec) : null;
      if (!target) return;
      if (target === CLIENT && ts.isImportDeclaration(node)) {
        const nb = node.importClause?.namedBindings;
        if (nb && ts.isNamedImports(nb)) nb.elements.forEach((el) => fromClient.add(el.name.text));
      } else if (!target.startsWith(path.join(SRC, 'api') + path.sep)) {
        queue.push(target);
      }
    });
    walk(sf, (node) => {
      if (ts.isPropertyAccessExpression(node) && ts.isIdentifier(node.expression) && fromClient.has(node.expression.text)) {
        refs.add(`${node.expression.text}.${node.name.text}`);
      }
    });
  }
  return refs;
}

interface PublicPage {
  module: string;
  routes: string[];
  calls: { owner: string; method: string; path: string; open: boolean }[];
}

function publicPages(flat: FlatRoute[]): PublicPage[] {
  const byModule = new Map<string, PublicPage>();
  const sites = clientCallSites();
  for (const { node, fullPath } of flat) {
    if (fullPath.endsWith('*') || !isPublicPath(example(fullPath))) continue;
    for (const c of componentsOf(node.element)) {
      const file = c.source?.file;
      if (!file) continue;
      const module = srcRel(file);
      if (!byModule.has(module)) {
        const calls = [...apiReferences(file)]
          .flatMap((owner) =>
            sites.filter((s) => s.owner === owner).map((s) => ({ owner, method: s.method, path: s.template, open: isOpenApiPath(s.template) }))
          )
          .sort((a, b) => (a.path === b.path ? (a.method < b.method ? -1 : 1) : a.path < b.path ? -1 : 1));
        byModule.set(module, { module, routes: [], calls });
      }
      const page = byModule.get(module)!;
      if (!page.routes.includes(fullPath)) page.routes.push(fullPath);
    }
  }
  return [...byModule.values()].sort((a, b) => (a.module < b.module ? -1 : 1));
}

/** Prefixes the 401 interceptor exempts, e.g. .includes('/api/v1/auth/'). */
function interceptorExemptions(): string[] {
  const out: string[] = [];
  for (const file of productionSources(path.join(SRC, 'api'))) {
    walk(parseFile(file), (node) => {
      if (
        ts.isCallExpression(node) &&
        ts.isPropertyAccessExpression(node.expression) &&
        ['includes', 'startsWith'].includes(node.expression.name.text)
      ) {
        const text = literalText(node.arguments[0]);
        if (text?.startsWith('/api/')) out.push(text);
      }
    });
  }
  return out;
}

// Parsing and type-checking the source tree takes seconds; allow for a loaded CI runner.
vi.setConfig({ testTimeout: 60_000 });

describe('public pages and open API paths', () => {
  const flat = flattenRoutes(parseAppRoutes().routes);
  const rule = goOpenRule();

  it('reads the Go rule and the frontend list', () => {
    expect(rule.prefixes.length).toBeGreaterThan(0);
    expect(publicSegments().length).toBeGreaterThan(0);
  });

  it('declares a route for every public segment', () => {
    const paths = flat.map((f) => f.fullPath);
    const orphans = publicSegments().filter((seg) => !paths.some((p) => p === seg || p.startsWith(`${seg}/`)));
    expect(orphans).toEqual([]);
  });

  it('treats every route declared outside the signed-in branch as public', () => {
    const always = flat.filter((f) => f.branch.length === 0 && !f.fullPath.endsWith('*'));
    expect(always.filter((f) => !isPublicPath(example(f.fullPath))).map((f) => f.fullPath)).toEqual([]);
  });

  it('lets public pages call only paths the server opens without a session', () => {
    const closed = publicPages(flat).flatMap((p) =>
      p.calls.filter((c) => !c.open).map((c) => `${p.module}: ${c.owner} ${c.method} ${c.path}`)
    );
    expect(closed).toEqual([]);
  });

  it('exempts from the 401 redirect only API prefixes the server opens', () => {
    const exempt = interceptorExemptions();
    expect(exempt.length).toBeGreaterThan(0);
    expect(exempt.filter((p) => !rule.prefixes.includes(p))).toEqual([]);
  });

  it('matches the pinned public-access table', async () => {
    const lines = [
      '# Which pages are public (utils/publicPaths.ts isPublicPath) and which API paths their module graph',
      `# calls, checked against ${GO_FILE} isOpenPath (OpenV REQ-114). Regenerate: ${REGENERATE}`,
      '',
      `isOpenPath exact: ${rule.exact.join(', ')}`,
      `isOpenPath prefixes: ${rule.prefixes.join(', ')}`,
      `401 interceptor exempts calls under: ${interceptorExemptions().join(', ')}`,
      `PUBLIC_SEGMENTS: ${publicSegments().join(', ')}`,
      '',
      '## Routes (example URL, :param as x)',
      ...flat
        .filter((f) => !f.fullPath.endsWith('*'))
        .map((f) => `  ${(isPublicPath(example(f.fullPath)) ? 'public' : 'session').padEnd(8)} ${f.fullPath}${f.node.index ? ' (index)' : ''}`),
      '',
      '## API calls reachable from each public page module (open = isOpenPath)',
      ...publicPages(flat).flatMap((p) => [
        `${p.module}  <- ${p.routes.join(', ')}`,
        ...(p.calls.length
          ? p.calls.map((c) => `  ${c.open ? 'open  ' : 'CLOSED'} ${c.method.padEnd(6)} ${c.path.padEnd(48)} ${c.owner}`)
          : ['  (no API calls)']),
      ]),
    ];
    await expect(lines.join('\n') + '\n').toMatchFileSnapshot('./__snapshots__/publicAccess.txt');
  });
});
