// Regenerate the snapshot (only in a PR that means to change the cascade):
//   npx vitest run src/arch -u
//
// Pins the CSS cascade (invariant I20, refactor plan S12b): the order in
// which the bundler emits the app's CSS. A later rule wins a tie in
// specificity, so the order is behaviour: ProjectList.css redefines
// `.button`, and it wins over index.css only because App.tsx imports
// components/ProjectList eagerly, after index.tsx has loaded index.css.
//
// The order is the modules' evaluation order, which is what the bundler
// concatenates CSS in: from src/index.tsx, each static import in source order,
// depth first, each module once, so a stylesheet lands where it is first
// reached. Each module is first transpiled the way the build does (one file at
// a time, imports used only as types dropped), so an import the build erases
// reaches nothing here either. Dynamic import() is not followed into the
// eager list: each lazy page gets its own list, the stylesheets its chunk adds
// beyond the eager ones, which is what a reordered import inside a lazy view
// (ModuleView's graph, say) changes. The snapshot names stylesheets and lazy
// modules only, never the module that imports a stylesheet, so a pure move
// (F1, F7, F8) that keeps evaluation order leaves it as it is.
//
// The Refactor guard's build identity (scripts/refactor/refactor_guard.py)
// checks the emitted CSS bytes themselves on a refactor pull request.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import ts from 'typescript';
import { describe, expect, it, vi } from 'vitest';
import { REGENERATE, SRC, srcRel, table } from './repo';

const ENTRY = path.join(SRC, 'index.tsx');

/** A module the walk reaches: one of ours (a file), or a package stylesheet. */
interface Reached {
  /** frontend/src-relative path, or the bare specifier of a package stylesheet. */
  name: string;
  file: string | null;
  css: boolean;
}

interface ModuleImports {
  /** Static imports and re-exports that survive transpiling, in source order. */
  imports: string[];
  /** import('…') targets, in source order. */
  dynamic: string[];
}

const cache = new Map<string, ModuleImports>();

/**
 * The imports the build keeps: the module transpiled on its own, as the
 * bundler's TypeScript transform does, so an import whose bindings are used
 * only as types is gone, then its import and export declarations read off
 * the output.
 */
function importsOf(file: string): ModuleImports {
  const hit = cache.get(file);
  if (hit) return hit;
  const out = ts.transpileModule(fs.readFileSync(file, 'utf8'), {
    fileName: file,
    compilerOptions: {
      module: ts.ModuleKind.ESNext,
      target: ts.ScriptTarget.ES2020,
      jsx: ts.JsxEmit.ReactJSX,
      isolatedModules: true,
    },
  }).outputText;
  const sf = ts.createSourceFile(`${file}.js`, out, ts.ScriptTarget.Latest, true, ts.ScriptKind.JS);
  const found: ModuleImports = { imports: [], dynamic: [] };
  for (const stmt of sf.statements) {
    if ((ts.isImportDeclaration(stmt) || ts.isExportDeclaration(stmt)) && stmt.moduleSpecifier) {
      if (ts.isStringLiteral(stmt.moduleSpecifier)) found.imports.push(stmt.moduleSpecifier.text);
    }
  }
  const visit = (node: ts.Node) => {
    if (ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword) {
      const arg = node.arguments[0];
      if (arg && (ts.isStringLiteral(arg) || ts.isNoSubstitutionTemplateLiteral(arg))) {
        found.dynamic.push(arg.text);
      } else {
        throw new Error(`${srcRel(file)}: import() of a computed specifier; the cascade guard cannot follow it`);
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  cache.set(file, found);
  return found;
}

const SCRIPT = /\.(tsx?|jsx?|mjs)$/;

/** Resolve a specifier the way Vite does for this app; null for a package module that is not a stylesheet. */
function resolve(from: string, specifier: string): Reached | null {
  if (!specifier.startsWith('.')) {
    // A package: its stylesheets count (ag-grid's theme), its scripts are
    // not ours to walk (none of them imports CSS).
    return specifier.endsWith('.css') ? { name: specifier, file: null, css: true } : null;
  }
  const base = path.resolve(path.dirname(from), specifier);
  const candidates = [base, `${base}.ts`, `${base}.tsx`, path.join(base, 'index.ts'), path.join(base, 'index.tsx')];
  for (const candidate of candidates) {
    if (fs.existsSync(candidate) && fs.statSync(candidate).isFile()) {
      return { name: srcRel(candidate), file: candidate, css: candidate.endsWith('.css') };
    }
  }
  throw new Error(`${srcRel(from)}: cannot resolve '${specifier}'`);
}

interface Walk {
  /** Stylesheets in the order the walk first reaches them. */
  css: string[];
  /** Every module the walk evaluated (frontend/src-relative). */
  modules: Set<string>;
  /** import() targets met on the way, in the order met. */
  dynamic: Reached[];
  /** For each stylesheet, the chain of modules that first reached it (for failure messages). */
  via: Map<string, string[]>;
}

/** Depth-first, post-order: what evaluating `start` loads, skipping what `loaded` already holds. */
function walk(start: string, loaded: ReadonlySet<string> = new Set()): Walk {
  const result: Walk = { css: [], modules: new Set(), dynamic: [], via: new Map() };
  const seen = new Set<string>(loaded);
  const dynamicSeen = new Set<string>();
  const visit = (file: string, chain: string[]) => {
    const name = srcRel(file);
    if (seen.has(name)) return;
    seen.add(name);
    result.modules.add(name);
    const { imports, dynamic } = importsOf(file);
    for (const specifier of imports) {
      const to = resolve(file, specifier);
      if (!to || seen.has(to.name)) continue;
      if (to.css) {
        seen.add(to.name);
        result.css.push(to.name);
        result.via.set(to.name, [...chain, name]);
      } else if (to.file && SCRIPT.test(to.file)) {
        visit(to.file, [...chain, name]);
      } else {
        seen.add(to.name); // JSON, images: no stylesheet below them
      }
    }
    for (const specifier of dynamic) {
      const to = resolve(file, specifier);
      if (to?.file && !dynamicSeen.has(to.name)) {
        dynamicSeen.add(to.name);
        result.dynamic.push(to);
      }
    }
  };
  visit(start, []);
  return result;
}

interface Cascade {
  eager: Walk;
  lazy: { page: string; css: string[]; via: Map<string, string[]> }[];
}

function cascade(): Cascade {
  const eager = walk(ENTRY);
  const lazy: Cascade['lazy'] = [];
  const queue = [...eager.dynamic];
  const queued = new Set(queue.map((r) => r.name));
  while (queue.length) {
    const page = queue.shift() as Reached;
    const w = walk(page.file as string, new Set([...eager.modules, ...eager.css]));
    lazy.push({ page: page.name, css: w.css, via: w.via });
    for (const next of w.dynamic) {
      if (!queued.has(next.name) && !eager.modules.has(next.name)) {
        queued.add(next.name);
        queue.push(next);
      }
    }
  }
  // Each lazy chunk's stylesheets load with it, whatever the order the pages are
  // declared in, so the pages are listed by path: moving the lazy() calls
  // elsewhere (X17's page registry) leaves the snapshot as it is.
  lazy.sort((a, b) => (a.page < b.page ? -1 : a.page > b.page ? 1 : 0));
  return { eager, lazy };
}

function render(c: Cascade): string {
  const lines = [
    '# The CSS cascade (invariant I20): the stylesheets the bundler emits, in order.',
    `# Regenerate: ${REGENERATE}`,
    '#',
    '# Eager: what src/index.tsx loads through static imports, depth first, each',
    '# module once, so a stylesheet sits where it is first reached; a later rule',
    '# wins a tie in specificity. Lazy: for each import() target, the stylesheets',
    '# its chunk adds beyond the eager ones, in the same order. Paths are relative',
    '# to frontend/src; a bare name is a package stylesheet. Last, the',
    '# stylesheets under src that no module the app loads imports: the bundle',
    '# leaves them out, and a change that starts loading one shows here.',
    '',
    `## Eager stylesheets (${c.eager.css.length}), in cascade order`,
    ...table(c.eager.css.map((css, i) => [String(i + 1), css]), '  '),
    '',
    `## Lazy modules (${c.lazy.length}), by path, and the stylesheets each adds, in cascade order`,
  ];
  for (const { page, css } of c.lazy) {
    lines.push(`  ${page}${css.length ? '' : '  (none)'}`);
    lines.push(...css.map((s) => `    ${s}`));
  }
  const unloaded = unreached(c);
  lines.push('', `## Stylesheets under src that no reachable module loads (${unloaded.length})`);
  lines.push(...unloaded.map((s) => `  ${s}`));
  return lines.join('\n') + '\n';
}

/** Stylesheets under src that neither walk reaches: the bundle leaves them out. */
function unreached(c: Cascade): string[] {
  const reached = new Set([...c.eager.css, ...c.lazy.flatMap((l) => l.css)]);
  const onDisk: string[] = [];
  const scan = (dir: string) => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const p = path.join(dir, entry.name);
      if (entry.isDirectory()) scan(p);
      else if (entry.name.endsWith('.css')) onDisk.push(srcRel(p));
    }
  };
  scan(SRC);
  return onDisk.filter((css) => !reached.has(css)).sort();
}

// Transpiling the reachable source tree takes a few seconds; allow for a loaded CI runner.
vi.setConfig({ testTimeout: 60_000 });

describe('CSS cascade', () => {
  const c = cascade();

  it('matches the pinned stylesheet order, eager and per lazy chunk', async () => {
    await expect(render(c)).toMatchFileSnapshot('./__snapshots__/cssOrder.txt');
  });

  // I20's example, spelled out so a failure names it: ProjectList.css
  // redefines `.button`, and wins over index.css because it comes later.
  it('loads ProjectList.css eagerly, after index.css', () => {
    const order = c.eager.css;
    const at = (name: string) => order.indexOf(name);
    expect(at('index.css'), 'index.css is no longer loaded eagerly').toBeGreaterThanOrEqual(0);
    expect(
      at('components/ProjectList.css'),
      `components/ProjectList.css is no longer eager (first reached ${c.eager.via.get('components/ProjectList.css')?.join(' -> ') ?? 'nowhere'}); ` +
        'its .button rules would stop overriding index.css until the view that loads it opens'
    ).toBeGreaterThan(at('index.css'));
    expect(c.eager.via.get('components/ProjectList.css')?.slice(0, 2)).toEqual(['index.tsx', 'App.tsx']);
  });

  it('reads the build the way the bundler does: type-only imports reach nothing', () => {
    // A type-only import of a module that loads a stylesheet must not pull it in.
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'css-order-'));
    try {
      fs.writeFileSync(path.join(dir, 'a.css'), '.a{}\n');
      fs.writeFileSync(path.join(dir, 'b.css'), '.b{}\n');
      fs.writeFileSync(path.join(dir, 'typed.ts'), "import './a.css';\nexport interface T { x: number }\n");
      fs.writeFileSync(path.join(dir, 'used.ts'), "import './b.css';\nexport const u = 1;\n");
      fs.writeFileSync(
        path.join(dir, 'entry.ts'),
        "import { T } from './typed';\nimport { u } from './used';\nexport const v: T = { x: u };\n"
      );
      expect(walk(path.join(dir, 'entry.ts')).css.map((s) => path.basename(s))).toEqual(['b.css']);
    } finally {
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });
});
