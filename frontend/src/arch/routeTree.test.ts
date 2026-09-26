// Regenerate the snapshot (only in a PR that means to change routes):
//   npx vitest run src/arch -u
//
// Pins the UI route table (invariant I19): every <Route> in App.tsx with its
// path, nesting, the JSX branch it sits in, what it renders, and whether that
// component loads eagerly or through lazy(); the relative redirects; and the
// relative order of App.tsx's side-effect imports and of the imports that
// bring in an eager route component, which decides the eager CSS cascade.
// Other imports and their bindings are left out, so an import added for a
// helper (F3's state/activeOrgStorage) leaves the snapshot unchanged. Parsed
// with the TypeScript compiler API, so a reformatted file reads the same.
import { describe, expect, it, vi } from 'vitest';
import { componentsOf, describeElement, flattenRoutes, parseAppRoutes, type RouteNode } from './appRoutes';
import { REGENERATE, table } from './repo';

function render(): string {
  const app = parseAppRoutes();
  const flat = flattenRoutes(app.routes);
  const loads = (load: string) =>
    flat.filter(({ node }) => componentsOf(node.element).some((c) => c.source?.load === load)).length;
  const redirects = flat.filter(({ node }) => node.element?.kind === 'redirect').length;
  const lines: string[] = [
    '# UI routes declared in src/App.tsx (invariant I19).',
    `# Regenerate: ${REGENERATE}`,
    '',
    `<Route> elements: ${flat.length}`,
    `  rendering a lazy() component: ${loads('lazy')}`,
    `  rendering an eagerly imported component: ${loads('eager')}`,
    `  redirects only: ${redirects}`,
    `index routes: ${flat.filter(({ node }) => node.index).length}`,
    '',
    '## App.tsx side-effect imports and imports of eager route components, in order',
  ];
  const routeModules = new Set(
    flat.flatMap(({ node }) => componentsOf(node.element).flatMap((c) => (c.source?.load === 'eager' ? [c.source.module] : [])))
  );
  lines.push(
    ...app.imports
      .filter((imp) => imp.sideEffect || routeModules.has(imp.module))
      .map((imp) => `  ${imp.module}${imp.sideEffect ? '  (side effect)' : ''}`)
  );
  lines.push('', '## lazy() components, in declaration order');
  lines.push(...table(app.lazy.map((l) => [l.local, `${l.module}#${l.exportName}`]), '  '));
  lines.push('', '## Route tree');
  const rows: string[][] = [];
  const emit = (nodes: RouteNode[], depth: number) => {
    for (const node of nodes) {
      const indent = '  '.repeat(depth);
      const where = node.branch.length ? `  when ${node.branch.join(' && ')}` : '';
      const label = node.index ? '(index)' : node.path ?? '(pathless)';
      rows.push([`${indent}${label}`, `${describeElement(node.element)}${where}`]);
      emit(node.children, depth + 1);
    }
  };
  emit(app.routes, 1);
  lines.push(...table(rows));
  lines.push('', '## Full paths, in declaration order');
  lines.push(
    ...table(
      flat.map(({ node, fullPath, branch }) => [
        `  ${fullPath}${node.index ? ' (index)' : ''}`,
        branch.length ? `when ${branch.join(' && ')}` : '',
      ])
    )
  );
  return lines.join('\n') + '\n';
}

// Parsing and type-checking the source tree takes seconds; allow for a loaded CI runner.
vi.setConfig({ testTimeout: 60_000 });

describe('App.tsx route tree', () => {
  it('matches the pinned route table, lazy boundaries and import order', async () => {
    await expect(render()).toMatchFileSnapshot('./__snapshots__/routeTree.txt');
  });

  it('resolves every rendered component to an import or a lazy() declaration', () => {
    const unresolved = flattenRoutes(parseAppRoutes().routes)
      .filter(({ node }) => componentsOf(node.element).some((c) => !c.source))
      .map(({ fullPath }) => fullPath);
    expect(unresolved).toEqual([]);
  });
});
