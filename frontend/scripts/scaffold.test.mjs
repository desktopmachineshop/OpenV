// Tests for scaffold.mjs. Run: cd frontend && node --test 'scripts/*.test.mjs'
//
// The scaffold writes into a copy of src/ in a temporary directory, beside
// the frontend's tsconfig.json and node_modules, and the repo's TypeScript
// type-checks the result: client.ts and every module it reaches, so the new
// module, its types and the barrel's two new lines; for a page, App.tsx and
// so every view, the registry and the new stub. A page's output is also run
// through src/arch/pagesParity.test.ts, copied with it. The real tree is never
// written.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import ts from 'typescript';
import { spawnSync } from 'node:child_process';
import { FRONTEND_DIR, REGENERATE_SURFACE, main, parseName } from './scaffold.mjs';

/** A copy of src/ that type-checks with the frontend's tsconfig and node_modules. */
function copyFrontend(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'scaffold-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  fs.cpSync(path.join(FRONTEND_DIR, 'src'), path.join(dir, 'src'), { recursive: true });
  for (const f of ['tsconfig.json', 'package.json', 'vite.config.ts']) fs.copyFileSync(path.join(FRONTEND_DIR, f), path.join(dir, f));
  fs.symlinkSync(path.join(FRONTEND_DIR, 'node_modules'), path.join(dir, 'node_modules'), 'dir');
  return dir;
}

/** Runs the command line's main() on root, with a stand-in area step. */
function scaffold(root, ...args) {
  const out = [];
  const code = main([...args, '--root', root], {
    log: { log: (s) => out.push(s), error: (s) => out.push(s) },
    areaOf: (_, rel) => `Area: ${rel}`,
  });
  return { code, out: out.join('\n') };
}

/** The program of an entry file (client.ts by default) under root's tsconfig, and its errors. */
function typeCheck(root, entry = 'src/api/client.ts') {
  const configPath = path.join(root, 'tsconfig.json');
  const parsed = ts.parseJsonConfigFileContent(ts.readConfigFile(configPath, ts.sys.readFile).config, ts.sys, root, undefined, configPath);
  const program = ts.createProgram([path.join(root, entry)], parsed.options);
  const errors = ts.getPreEmitDiagnostics(program).map((d) => {
    const msg = ts.flattenDiagnosticMessageText(d.messageText, ' ');
    if (!d.file) return msg;
    const { line } = d.file.getLineAndCharacterOfPosition(d.start);
    return `${path.relative(root, d.file.fileName)}:${line + 1}: ${msg}`;
  });
  return { program, errors };
}

/** Every file under dir, relative, with its text. */
function snapshot(dir) {
  const out = {};
  for (const rel of fs.readdirSync(dir, { recursive: true }).sort()) {
    const p = path.join(dir, rel);
    if (fs.statSync(p).isFile()) out[rel] = fs.readFileSync(p, 'utf8');
  }
  return out;
}

const lastIndex = (lines, prefix) => lines.findLastIndex((l) => l.startsWith(prefix));

test('api-module writes a module, its types and two barrel lines, and they type-check', (t) => {
  const root = copyFrontend(t);
  const client = path.join(root, 'src/api/client.ts');
  const old = fs.readFileSync(client, 'utf8').split('\n');

  const r = scaffold(root, 'api-module', 'widget-reports');
  assert.equal(r.code, 0, r.out);
  assert.match(r.out, /Left to do:/);
  for (const f of ['src/api/widgetReports.ts', 'src/api/types/widgetReports.ts']) {
    assert.ok(fs.existsSync(path.join(root, f)), `${f} was not written`);
  }

  // The two lines are appended after the last line of their kind, and
  // nothing else in the barrel changes.
  const typeLine = "export type * from './types/widgetReports';";
  const modLine = "export * from './widgetReports';";
  const now = fs.readFileSync(client, 'utf8').split('\n');
  assert.equal(now.indexOf(typeLine), lastIndex(old, 'export type * from') + 1);
  assert.equal(now.indexOf(modLine), lastIndex(old, 'export * from') + 2);
  assert.deepEqual(
    now.filter((l) => l !== typeLine && l !== modLine),
    old,
  );

  const { program, errors } = typeCheck(root);
  assert.deepEqual(errors, []);
  // The barrel exports the module's object, with its method, and its type.
  const checker = program.getTypeChecker();
  const exports = checker.getExportsOfModule(checker.getSymbolAtLocation(program.getSourceFile(client)));
  const byName = new Map(exports.map((s) => [s.name, s]));
  assert.ok(byName.has('WidgetReportsItem'), 'the barrel does not export WidgetReportsItem');
  const api = byName.get('widgetReportsAPI');
  assert.ok(api, 'the barrel does not export widgetReportsAPI');
  const target = api.flags & ts.SymbolFlags.Alias ? checker.getAliasedSymbol(api) : api;
  assert.deepEqual(
    checker.getPropertiesOfType(checker.getTypeOfSymbolAtLocation(target, target.valueDeclaration)).map((p) => p.name),
    ['list'],
  );
});

test('-n and every refusal write nothing', (t) => {
  const root = copyFrontend(t);
  // The names refused are taken in the copy first, so the test leans on no
  // name of the real tree.
  fs.writeFileSync(path.join(root, 'src/api/types/widgetFiles.ts'), 'export {};\n', { flag: 'wx' });
  fs.writeFileSync(
    path.join(root, 'src/api/seeded.ts'),
    'export const widgetCountsAPI = {};\nexport interface WidgetTotalsItem {\n  id: string;\n}\nconst x = 1;\nexport { x as widgetSumsAPI };\n',
    { flag: 'wx' },
  );
  const before = snapshot(path.join(root, 'src/api'));
  for (const [args, code, out] of [
    [['-n', 'api-module', 'widget-reports'], 0, 'would create frontend/src/api/widgetReports.ts'],
    [['api-module', 'widget_reports', '-n'], 0, "export * from './widgetReports';"],
    [['api-module', 'widget-files'], 1, 'frontend/src/api/types/widgetFiles.ts already exists'],
    [['api-module', 'widget-counts'], 1, 'widgetCountsAPI is already exported by frontend/src/api/seeded.ts'],
    [['api-module', 'widget-totals'], 1, 'WidgetTotalsItem is already exported by frontend/src/api/seeded.ts'],
    [['api-module', 'widget-sums'], 1, 'widgetSumsAPI is already exported by frontend/src/api/seeded.ts'],
    [['api-module', 'Widgets'], 2, 'lower-case words'],
    [['page'], 2, 'want a kind and a name'],
    [['api-module'], 2, 'usage:'],
    [['-x', 'api-module', 'widgets'], 2, 'unknown flag -x'],
  ]) {
    const r = scaffold(root, ...args);
    assert.equal(r.code, code, `scaffold ${args.join(' ')}:\n${r.out}`);
    assert.ok(r.out.includes(out), `scaffold ${args.join(' ')} printed no ${JSON.stringify(out)}:\n${r.out}`);
  }
  assert.deepEqual(snapshot(path.join(root, 'src/api')), before);
});

test('names map as the header documents, and the printed command is the README\'s', () => {
  for (const s of ['work-items', 'work_items']) {
    assert.deepEqual(parseName(s), { kebab: 'work-items', camel: 'workItems', pascal: 'WorkItems', text: 'work items' });
  }
  const readme = fs.readFileSync(path.join(FRONTEND_DIR, 'src/api/README.md'), 'utf8');
  assert.ok(readme.includes(`\`${REGENERATE_SURFACE}\``), `frontend/src/api/README.md does not name ${REGENERATE_SURFACE}`);
});

// ---- page ----

// The nested vitest's output without colour codes: CI forces colour (FORCE_COLOR
// reaches the child), so the child is told not to, and a code that still gets
// through is stripped before any text is matched.
// eslint-disable-next-line no-control-regex
const stripAnsi = (text) => text.replace(/\u001b\[[0-9;]*m/g, '');

/** Runs the parity test inside a scaffolded copy; its exit status is what matters. */
function runParity(root) {
  const r = spawnSync('npx', ['vitest', 'run', 'src/arch/pagesParity.test.ts'], {
    cwd: root,
    encoding: 'utf8',
    env: { ...process.env, NO_COLOR: '1', FORCE_COLOR: '0' },
  });
  return { status: r.status, text: stripAnsi(`${r.stdout}\n${r.stderr}`) };
}

const PAGE_FILES = ['src/App.tsx', 'src/pages.ts', 'src/views/WidgetReports.tsx'];

/** Every file `PAGE_FILES` names, with its text ('' for one that does not exist). */
const pageFiles = (root) => Object.fromEntries(PAGE_FILES.map((f) => [f, fs.existsSync(path.join(root, f)) ? fs.readFileSync(path.join(root, f), 'utf8') : '']));

/** The lines of `after` that are not in `before`, in order, given that `before` is `after` less those lines. */
function addedLines(before, after) {
  const a = before.split('\n');
  const out = [];
  let i = 0;
  for (const line of after.split('\n')) {
    if (i < a.length && line === a[i]) i++;
    else out.push(line);
  }
  assert.equal(i, a.length, 'a line of the old file is missing from the new one');
  return out;
}

test('page adds a lazy project page, and the result type-checks and passes the parity tests', (t) => {
  const root = copyFrontend(t);
  const before = pageFiles(root);
  // The copy's own type errors, if any (App.tsx reaches a typing the symlinked
  // node_modules resolves differently); the scaffold must add none.
  const baseline = typeCheck(root, 'src/App.tsx').errors;

  const r = scaffold(root, 'page', 'widget-reports');
  assert.equal(r.code, 0, r.out);
  assert.match(r.out, /Left to do:/);
  const after = pageFiles(root);
  assert.match(after['src/views/WidgetReports.tsx'], /export const WidgetReports: React\.FC/);

  // Each existing file only gains lines: the constant after the last lazy()
  // one, the route as the last child of the project's route, the entry after
  // the project's last page. The rest of App.tsx, the JSX included, is as it was.
  assert.deepEqual(addedLines(before['src/App.tsx'], after['src/App.tsx']), [
    'const WidgetReports = lazy(() =>',
    "  import('./views/WidgetReports').then((m) => ({ default: m.WidgetReports }))",
    ');',
    '          <Route path="widget-reports" element={<WidgetReports />} />',
  ]);
  const appLines = after['src/App.tsx'].split('\n');
  assert.match(appLines[appLines.findIndex((l) => l.includes('const WidgetReports')) - 1], /^\);$/);
  assert.match(appLines[appLines.findIndex((l) => l.includes('<Route path="widget-reports"')) + 1], /^\s*<\/Route>$/);
  assert.deepEqual(addedLines(before['src/pages.ts'], after['src/pages.ts']), [
    "  { scope: 'project', path: 'widget-reports', load: 'lazy', public: false, help: NOT_WRITTEN },",
  ]);
  const pagesLines = after['src/pages.ts'].split('\n');
  const at = pagesLines.findIndex((l) => l.includes("path: 'widget-reports'"));
  assert.match(pagesLines[at - 1], /path: 'team\/network'/);
  assert.equal(pagesLines[at + 1], '');

  assert.deepEqual(typeCheck(root, 'src/App.tsx').errors, baseline);

  // The routes and the registry agree, as vitest reads them in the copy.
  const v = runParity(root);
  assert.equal(v.status, 0, v.text);
  assert.match(v.text, /Tests\s+\d+ passed/);
});

test('without its registry entry the new route fails the parity tests', (t) => {
  // The test above passes for a reason: take the entry away and it fails.
  const root = copyFrontend(t);
  assert.equal(scaffold(root, 'page', 'widget-reports').code, 0);
  const pagesFile = path.join(root, 'src/pages.ts');
  fs.writeFileSync(pagesFile, fs.readFileSync(pagesFile, 'utf8').split('\n').filter((l) => !l.includes("path: 'widget-reports'")).join('\n'));
  const v = runParity(root);
  assert.notEqual(v.status, 0);
  assert.match(v.text, /has an entry for every route of App\.tsx/);
});

test('page: -n and every refusal write nothing', (t) => {
  const root = copyFrontend(t);
  // A project page the registry holds that App.tsx does not route, and a view file.
  const pagesFile = path.join(root, 'src/pages.ts');
  fs.writeFileSync(
    pagesFile,
    fs.readFileSync(pagesFile, 'utf8').replace("  { scope: 'site', path: 'login'", "  { scope: 'project', path: 'widget-gauges', load: 'lazy', public: false, help: NOT_WRITTEN },\n\n  { scope: 'site', path: 'login'"),
  );
  fs.writeFileSync(path.join(root, 'src/views/WidgetFiles.tsx'), 'export {};\n', { flag: 'wx' });
  const before = snapshot(path.join(root, 'src'));
  for (const [args, code, out] of [
    [['-n', 'page', 'widget-reports'], 0, 'would create frontend/src/views/WidgetReports.tsx'],
    [['page', 'widget_reports', '-n'], 0, "path: 'widget-reports'"],
    [['page', 'widget-files'], 1, 'frontend/src/views/WidgetFiles.tsx already exists'],
    [['page', 'widget-gauges'], 1, "already has a project page with path 'widget-gauges'"],
    [['page', 'routes'], 1, 'already uses the name Routes'],
    [['page', 'guided'], 1, 'already has a route with path "guided"'],
    [['page', 'Widgets'], 2, 'lower-case words'],
    [['page', 'widget-reports', 'extra'], 2, 'want a kind and a name'],
  ]) {
    const r = scaffold(root, ...args);
    assert.equal(r.code, code, `scaffold ${args.join(' ')}:\n${r.out}`);
    assert.ok(r.out.includes(out), `scaffold ${args.join(' ')} printed no ${JSON.stringify(out)}:\n${r.out}`);
  }
  assert.deepEqual(snapshot(path.join(root, 'src')), before);
});

test('the printed regenerate commands are the ones the area guide names', () => {
  const readme = fs.readFileSync(path.join(FRONTEND_DIR, 'src/README.md'), 'utf8');
  assert.ok(readme.includes('UPDATE_BUNDLE_SHAPE=1 node scripts/bundle-check.mjs'));
  assert.ok(readme.includes(REGENERATE_SURFACE));
});
