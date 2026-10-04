// Tests for scaffold.mjs. Run: cd frontend && node --test 'scripts/*.test.mjs'
//
// The scaffold writes into a copy of src/ in a temporary directory, beside
// the frontend's tsconfig.json and node_modules, and the repo's TypeScript
// type-checks the result: client.ts and every module it reaches, so the new
// module, its types and the barrel's two new lines. The real tree is never
// written.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import ts from 'typescript';
import { FRONTEND_DIR, REGENERATE_SURFACE, main, parseName } from './scaffold.mjs';

/** A copy of src/ that type-checks with the frontend's tsconfig and node_modules. */
function copyFrontend(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'scaffold-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  fs.cpSync(path.join(FRONTEND_DIR, 'src'), path.join(dir, 'src'), { recursive: true });
  fs.copyFileSync(path.join(FRONTEND_DIR, 'tsconfig.json'), path.join(dir, 'tsconfig.json'));
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

/** The program of client.ts under root's tsconfig, and its errors. */
function typeCheck(root) {
  const configPath = path.join(root, 'tsconfig.json');
  const parsed = ts.parseJsonConfigFileContent(ts.readConfigFile(configPath, ts.sys.readFile).config, ts.sys, root, undefined, configPath);
  const program = ts.createProgram([path.join(root, 'src/api/client.ts')], parsed.options);
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
    [['page', 'widgets'], 2, 'X17'],
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
