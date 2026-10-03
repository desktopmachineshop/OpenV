// Tests for tsdeclmove.mjs. Run: cd frontend && node --test 'scripts/*.test.mjs'
//
// The fixture's goldens (testdata/tsdeclmove/want) regenerate, when the tool
// changes on purpose, with: cd frontend && UPDATE_GOLDEN=1 node --test scripts/tsdeclmove.test.mjs
// (only the value 1 regenerates; any other compares). The F1 tests prove the
// committed spec, specs/F1.json: on testdata/tsdeclmove/d11dee8, a copy of
// d11dee8's client.ts and the modules it imports, and on a copy of this
// working tree, where they run every gate the frontend job runs.
import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import ts from 'typescript';
import { analyzeTree, diffManifests, manifest, readWorkingTree } from './tsdeclhash.mjs';
import { moveCheck } from './tsmovecheck.mjs';
import {
  BARREL_MAX_LINES,
  CheckError,
  MODULE_MAX_LINES,
  Refusal,
  SpecError,
  apply,
  loadSpec,
  parseSpec,
  plan,
  readSource,
  selfCheck,
} from './tsdeclmove.mjs';

const SCRIPTS = path.dirname(fileURLToPath(import.meta.url));
const FRONTEND = path.resolve(SCRIPTS, '..');
const REPO = path.resolve(FRONTEND, '..');
const SCRIPT = path.join(SCRIPTS, 'tsdeclmove.mjs');
const TESTDATA = path.join(SCRIPTS, 'testdata', 'tsdeclmove');
const FIXTURE = path.join(TESTDATA, 'fixture');
const FIXTURE_SPEC = path.join(TESTDATA, 'fixture.json');
const WANT = path.join(TESTDATA, 'want');
const D11DEE8 = path.join(TESTDATA, 'd11dee8');
const F1_SPEC = path.join(SCRIPTS, 'specs', 'F1.json');
const REGENERATE = 'cd frontend && UPDATE_GOLDEN=1 node --test scripts/tsdeclmove.test.mjs';
const SOURCE = 'src/api/client.ts';

// Children run as the frontend job's steps do, not as node:test children.
const childEnv = () => {
  const env = { ...process.env, CI: 'true' };
  delete env.NODE_TEST_CONTEXT;
  return env;
};

/** A temporary directory, removed after the test. */
function tmp(t, prefix = 'tsdeclmove-') {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), prefix));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  return dir;
}

/** Every file under dir, relative and sorted, with its bytes. */
function snapshot(dir) {
  const out = new Map();
  const walk = (rel) => {
    for (const e of fs.readdirSync(path.join(dir, rel), { withFileTypes: true }).sort((a, b) => (a.name < b.name ? -1 : 1))) {
      const r = rel ? `${rel}/${e.name}` : e.name;
      if (e.name === 'node_modules' || e.isSymbolicLink()) continue;
      if (e.isDirectory()) walk(r);
      else out.set(r, fs.readFileSync(path.join(dir, r)));
    }
  };
  walk('');
  return out;
}

/** A copy of a source tree that type-checks with the frontend's tsconfig
 * and node_modules. */
function copyTree(t, from) {
  const dir = tmp(t);
  fs.cpSync(from, dir, { recursive: true });
  fs.copyFileSync(path.join(FRONTEND, 'tsconfig.json'), path.join(dir, 'tsconfig.json'));
  fs.symlinkSync(path.join(FRONTEND, 'node_modules'), path.join(dir, 'node_modules'), 'dir');
  return dir;
}

/** Writes files (path -> text) into dir. */
function write(dir, files) {
  for (const [p, text] of Object.entries(files)) {
    fs.mkdirSync(path.dirname(path.join(dir, p)), { recursive: true });
    fs.writeFileSync(path.join(dir, p), text);
  }
}

/** The tree's TypeScript errors under its tsconfig.json, with extra options. */
function typeErrors(dir, extra = {}) {
  const configPath = path.join(dir, 'tsconfig.json');
  const parsed = ts.parseJsonConfigFileContent(ts.readConfigFile(configPath, ts.sys.readFile).config, ts.sys, dir, undefined, configPath);
  const program = ts.createProgram(parsed.fileNames, { ...parsed.options, ...extra });
  return ts.getPreEmitDiagnostics(program).map((d) => {
    const msg = ts.flattenDiagnosticMessageText(d.messageText, ' ');
    if (!d.file) return msg;
    const { line } = d.file.getLineAndCharacterOfPosition(d.start);
    return `${path.relative(dir, d.file.fileName)}:${line + 1}: ${msg}`;
  });
}

/** The export surface of a module as S12's clientSurface test reads it:
 * kind and name of every export, and the members of exported objects. */
function surface(dir, modPath) {
  const configPath = path.join(dir, 'tsconfig.json');
  const parsed = ts.parseJsonConfigFileContent(ts.readConfigFile(configPath, ts.sys.readFile).config, ts.sys, dir, undefined, configPath);
  const file = path.join(dir, modPath);
  const program = ts.createProgram([file], parsed.options);
  const checker = program.getTypeChecker();
  const rows = [];
  for (const exp of checker.getExportsOfModule(checker.getSymbolAtLocation(program.getSourceFile(file)))) {
    const target = exp.flags & ts.SymbolFlags.Alias ? checker.getAliasedSymbol(exp) : exp;
    rows.push(`${exp.name} ${target.flags}`);
    const decl = target.valueDeclaration;
    if (decl && ts.isVariableDeclaration(decl) && decl.initializer && ts.isObjectLiteralExpression(decl.initializer)) {
      for (const p of checker.getPropertiesOfType(checker.getTypeAtLocation(decl))) rows.push(`${exp.name}.${p.name}`);
    }
  }
  return rows.sort();
}

/** tsdeclhash --no-module and tsmovecheck between two source trees. */
function proofs(before, after) {
  const b = analyzeTree(before);
  const h = analyzeTree(after);
  return { hash: diffManifests(manifest(b, { noModule: true }), manifest(h, { noModule: true })).diffs, move: moveCheck(b, h) };
}

const srcFiles = (dir) => readWorkingTree(dir, [path.join(dir, 'src')]);
const fixtureSpec = () => loadSpec(FIXTURE_SPEC);
const fixtureRaw = () => JSON.parse(fs.readFileSync(FIXTURE_SPEC, 'utf8'));
const run = (...args) => spawnSync(process.execPath, [SCRIPT, ...args], { encoding: 'utf8', env: childEnv() });
const lineCount = (text) => text.split('\n').length - (text.endsWith('\n') ? 1 : 0);

/** Each declaration's text as tsdeclhash sees it (attached comments, the
 * statement, a comment on its last line), by statement index. */
function declarationTexts(modPath, text) {
  const src = readSource(modPath, text);
  return src.statements
    .filter((s) => s.kind === 'decl')
    .map((s) => {
      const ranges = ts.getLeadingCommentRanges(src.text, s.node.getFullStart()) ?? [];
      let from = s.node.getStart(src.sf);
      for (let i = ranges.length - 1; i >= 0; i--) {
        if ((src.text.slice(ranges[i].end, from).match(/\n/g) ?? []).length > 1) break;
        from = ranges[i].pos;
      }
      return { names: s.names, text: src.text.slice(from, s.node.end) };
    });
}

// ------------------------------------------------------------------ fixture

test('the fixture moves as fixture.json says, into the goldens under want/', (t) => {
  const dir = copyTree(t, FIXTURE);
  const before = snapshot(dir);
  const r = apply(dir, fixtureSpec(), { typecheck: false });
  assert.equal(r.moved, 21);
  assert.equal(r.barrelLines, 23);
  assert.deepEqual(r.notes, []);
  assert.deepEqual(
    r.map.map((m) => `${m.path}: ${m.names.join(', ')}`),
    [
      'src/api/http.ts: API_URL, client, (statement), uploadConfig, (statement), save',
      'src/api/types/items.ts: Progress, ItemStatus, Item, ItemPage, ItemQuery',
      'src/api/types/widgets.ts: Method, Widgets',
      'src/api/types/accounts.ts: Role, Account',
      'src/api/items.ts: PAGE, itemAPI',
      'src/api/widgets.ts: METHODS, widgetAPI, exportAPI',
      'src/api/accounts.ts: accountAPI',
    ],
  );
  const after = snapshot(dir);
  const written = [...after.keys()].filter((p) => !before.has(p) || !before.get(p).equals(after.get(p)));
  assert.deepEqual([...before.keys()].filter((p) => !after.has(p)), [], 'a move deletes no file');
  if (process.env.UPDATE_GOLDEN === '1') {
    fs.rmSync(WANT, { recursive: true, force: true });
    write(WANT, Object.fromEntries(written.map((p) => [p, after.get(p)])));
    t.diagnostic(`wrote ${written.length} goldens under ${path.relative(FRONTEND, WANT)}`);
    return;
  }
  const want = snapshot(WANT);
  assert.deepEqual(written.sort(), [...want.keys()].sort(), `the files tsdeclmove writes differ from the goldens; if it changed on purpose: ${REGENERATE}`);
  for (const [p, bytes] of want) {
    assert.equal(after.get(p).toString(), bytes.toString(), `${p} differs from its golden under testdata/tsdeclmove/want; if tsdeclmove changed on purpose: ${REGENERATE}`);
  }
});

test('the moved fixture type-checks with no unused import and is a pure move with the same surface', (t) => {
  const dir = copyTree(t, FIXTURE);
  assert.deepEqual(typeErrors(dir), [], 'the fixture type-checks before the move');
  const before = srcFiles(dir);
  const surfaceBefore = surface(dir, SOURCE);
  apply(dir, fixtureSpec(), { typecheck: false });
  assert.deepEqual(typeErrors(dir, { noUnusedLocals: true }), []);
  const { hash, move } = proofs(before, srcFiles(dir));
  assert.deepEqual(hash, [], 'tsdeclhash --no-module');
  assert.deepEqual(move.failures, [], 'tsmovecheck');
  assert.equal(move.moved.length, 21);
  assert.deepEqual(surface(dir, SOURCE), surfaceBefore);
});

test('each declaration moves byte for byte with the comments above it, the side effects in order', (t) => {
  const dir = copyTree(t, FIXTURE);
  const original = fs.readFileSync(path.join(dir, SOURCE), 'utf8');
  const r = apply(dir, fixtureSpec(), { typecheck: false });
  const home = new Map(r.map.flatMap((m) => m.names.map((n, i) => [`${n}#${m.names.slice(0, i).filter((x) => x === n).length}`, m.path])));
  const seen = new Map();
  for (const d of declarationTexts(SOURCE, original)) {
    const n = d.names[0];
    const k = `${n}#${seen.get(n) ?? 0}`;
    seen.set(n, (seen.get(n) ?? 0) + 1);
    const text = fs.readFileSync(path.join(dir, home.get(k)), 'utf8');
    assert.ok(text.includes(d.text), `${n} is not in ${home.get(k)} byte for byte:\n${d.text}`);
  }
  const http = fs.readFileSync(path.join(dir, 'src/api/http.ts'), 'utf8');
  const at = ['const API_URL', 'const client', 'client.interceptors.request', 'client.interceptors.response'].map((s) => http.indexOf(s));
  assert.ok(at.every((p, i) => p >= 0 && (i === 0 || p > at[i - 1])), `the statements with side effects keep their order in http.ts: ${at}`);
});

test('a dry run prints the map and writes nothing', (t) => {
  const dir = copyTree(t, FIXTURE);
  const before = snapshot(dir);
  const r = run('--root', dir, '-n', '--spec', FIXTURE_SPEC);
  assert.equal(r.status, 0, r.stdout + r.stderr);
  assert.match(r.stdout, /^src\/api\/http\.ts \(36 lines\): API_URL, client, \(statement\), uploadConfig, \(statement\), save$/m);
  assert.match(r.stdout, /tsdeclmove: would move 21 declarations of src\/api\/client\.ts into 7 modules; src\/api\/client\.ts would become a barrel of 23 lines/);
  assert.deepEqual(snapshot(dir), before);
});

test('the command line moves and type-checks, then finds nothing to move; its exit statuses', (t) => {
  const dir = copyTree(t, FIXTURE);
  const moved = run('--root', dir, '--spec', FIXTURE_SPEC);
  assert.equal(moved.status, 0, moved.stdout + moved.stderr);
  assert.match(moved.stdout, /tsdeclmove: moved 21 declarations of src\/api\/client\.ts into 7 modules; src\/api\/client\.ts is now a barrel of 23 lines/);
  const after = snapshot(dir);
  const again = run('--root', dir, '--spec', FIXTURE_SPEC);
  assert.equal(again.status, 0, again.stdout + again.stderr);
  assert.match(again.stdout, /^note: nothing to move: src\/api\/client\.ts declares nothing \(it is already a barrel\)$/m);
  assert.deepEqual(snapshot(dir), after, 'on its own output it changes nothing');

  const fresh = copyTree(t, FIXTURE);
  write(fresh, { 'src/api/items.ts': 'export const taken = 1;\n' });
  const before = snapshot(fresh);
  const refused = run('--root', fresh, '--spec', FIXTURE_SPEC);
  assert.equal(refused.status, 1, refused.stdout + refused.stderr);
  assert.match(refused.stdout, /^refused: src\/api\/items\.ts exists; a move creates its modules$/m);
  assert.match(refused.stdout, /tsdeclmove: refused, nothing written/);
  assert.deepEqual(snapshot(fresh), before);

  // A tree that does not type-check: the move is written, checked, restored.
  const broken = copyTree(t, FIXTURE);
  write(broken, { 'src/broken.ts': "export const n: number = 'not a number';\n" });
  const brokenBefore = snapshot(broken);
  const failed = run('--root', broken, '--spec', FIXTURE_SPEC);
  assert.equal(failed.status, 1, failed.stdout + failed.stderr);
  assert.match(failed.stdout, /^check: the moved tree does not type-check, so every file is restored:$/m);
  assert.match(failed.stdout, /^check: src\/broken\.ts:1:14: Type 'string' is not assignable to type 'number'\.$/m);
  assert.deepEqual(snapshot(broken), brokenBefore);

  const bad = path.join(tmp(t), 'bad.json');
  fs.writeFileSync(bad, '{"source": "src/api/client.ts"');
  for (const args of [[], ['--root', dir], ['--bogus'], ['--spec'], ['--spec', FIXTURE_SPEC, 'extra'], ['--spec', bad], ['--spec', path.join(dir, 'none.json')], ['--root', path.join(dir, 'src'), '--spec', FIXTURE_SPEC]]) {
    const r = run(...args);
    assert.equal(r.status, 2, `${args.join(' ')}: ${r.stdout}${r.stderr}`);
  }
});

test('the tree is restored byte for byte when the moved tree does not type-check', (t) => {
  const dir = copyTree(t, FIXTURE);
  const before = snapshot(dir);
  let checked = 0;
  assert.throws(
    () =>
      apply(dir, fixtureSpec(), {
        typecheck: (root) => {
          checked++;
          assert.ok(fs.existsSync(path.join(root, 'src/api/types/items.ts')), 'the check runs on the written move');
          return ['src/api/items.ts:1: boom'];
        },
      }),
    (e) => e instanceof CheckError && /does not type-check/.test(e.message) && /boom/.test(e.message),
  );
  assert.equal(checked, 1);
  assert.deepEqual(snapshot(dir), before);
  assert.ok(!fs.existsSync(path.join(dir, 'src/api/types')), 'a directory the move created is removed');
});

test('spec drift: an unnamed declaration joins its default module, a lost name and an emptied module are notes', (t) => {
  const dir = copyTree(t, FIXTURE);
  const client = fs.readFileSync(path.join(dir, SOURCE), 'utf8');
  write(dir, {
    [SOURCE]:
      client.replace('export default client;\n', 'export default client;\n\nexport const one = 1, two = 2;\n') +
      "\nexport interface Extra {\n  n: number;\n}\n\nexport const extraAPI = { get: () => client.get<Extra>('/api/extra') };\n",
  });
  const raw = fixtureRaw();
  raw.modules.push({ path: 'src/api/ghosts.ts', header: '// Nothing lands here.', declarations: ['ghostAPI'] });
  raw.modules.find((m) => m.path === 'src/api/items.ts').declarations.push('one');
  const before = srcFiles(dir);
  const r = apply(dir, parseSpec(raw), { typecheck: false });
  assert.deepEqual(r.notes, [
    'two is not in the spec: it moves to src/api/items.ts with one, which the same statement declares',
    'Extra is not in the spec: it joins src/api/types/accounts.ts (default.types); name it in the spec to place it',
    'extraAPI is not in the spec: it joins src/api/accounts.ts (default.values); name it in the spec to place it',
    'ghostAPI is in the spec but src/api/client.ts does not declare it: left out',
    'src/api/ghosts.ts gets no declaration: not written',
  ]);
  assert.ok(!fs.existsSync(path.join(dir, 'src/api/ghosts.ts')));
  assert.doesNotMatch(fs.readFileSync(path.join(dir, SOURCE), 'utf8'), /ghosts/);
  assert.match(fs.readFileSync(path.join(dir, 'src/api/types/accounts.ts'), 'utf8'), /export interface Extra \{/);
  assert.match(fs.readFileSync(path.join(dir, 'src/api/accounts.ts'), 'utf8'), /export const extraAPI = /);
  assert.match(fs.readFileSync(path.join(dir, 'src/api/items.ts'), 'utf8'), /export const one = 1, two = 2;/);
  const { hash, move } = proofs(before, srcFiles(dir));
  assert.deepEqual([...hash, ...move.failures], []);
});

test('a statement that declares nothing moves with the runtime declaration before it, past a type', (t) => {
  const dir = copyTree(t, FIXTURE);
  const client = fs.readFileSync(path.join(dir, SOURCE), 'utf8');
  write(dir, { [SOURCE]: client.replace('client.interceptors.response.use(', '/** Between the config and the interceptor. */\nexport type Gap = number;\n\nclient.interceptors.response.use(') });
  const raw = fixtureRaw();
  mod(raw, 'src/api/types/items.ts').declarations.push('Gap');
  const before = srcFiles(dir);
  const r = apply(dir, parseSpec(raw), { typecheck: false });
  assert.deepEqual(r.map.find((m) => m.path === 'src/api/http.ts').names, ['API_URL', 'client', '(statement)', 'uploadConfig', '(statement)', 'save']);
  assert.deepEqual(r.map.find((m) => m.path === 'src/api/types/items.ts').names, ['Progress', 'Gap', 'ItemStatus', 'Item', 'ItemPage', 'ItemQuery']);
  const { hash, move } = proofs(before, srcFiles(dir));
  assert.deepEqual([...hash, ...move.failures], []);
});

test('a helper type is exported in an `export type` list, beside a value list, which isolatedModules accepts', (t) => {
  // ItemQuery has no export modifier and items.ts uses it; placed in http.ts,
  // whose values items.ts uses too, it gets a list of its own (TS1205).
  const dir = copyTree(t, FIXTURE);
  const raw = fixtureRaw();
  relist(raw, 'ItemQuery', 'src/api/http.ts');
  const before = srcFiles(dir);
  apply(dir, parseSpec(raw), { typecheck: false });
  const http = fs.readFileSync(path.join(dir, 'src/api/http.ts'), 'utf8');
  assert.ok(http.endsWith('};\n\nexport { API_URL, client, uploadConfig, save };\nexport type { ItemQuery };\n'), http.slice(-200));
  assert.match(fs.readFileSync(path.join(dir, 'src/api/items.ts'), 'utf8'), /^import type \{ ItemQuery \} from '\.\/http';$/m);
  assert.deepEqual(typeErrors(dir, { isolatedModules: true, noUnusedLocals: true }), []);
  const { hash, move } = proofs(before, srcFiles(dir));
  assert.deepEqual([...hash, ...move.failures], []);
});

test('a module-scope let moves with the declaration that assigns it, and another module may read it', (t) => {
  // widgetAPI reads token and assigns a local of the same name, which is not
  // the module's binding.
  const dir = copyTree(t, FIXTURE);
  const client = fs.readFileSync(path.join(dir, SOURCE), 'utf8');
  write(dir, {
    [SOURCE]: client
      .replace('const PAGE = 100;\n', "const PAGE = 100;\n\nlet token = '';\n")
      .replace('  link: (id: string)', '  login: (t: string): void => {\n    token = t;\n  },\n  link: (id: string)')
      .replace(
        '  item: (id: string) => itemAPI.link(id),',
        "  item: (id: string) => itemAPI.link(id),\n  token: () => token,\n  tagged: (tag: string) => {\n    let token = tag;\n    token += '!';\n    return token;\n  },",
      ),
  });
  const raw = fixtureRaw();
  mod(raw, 'src/api/items.ts').declarations.push('token');
  const before = srcFiles(dir);
  apply(dir, parseSpec(raw), { typecheck: false });
  assert.match(fs.readFileSync(path.join(dir, 'src/api/items.ts'), 'utf8'), /^let token = '';$/m);
  assert.match(fs.readFileSync(path.join(dir, 'src/api/widgets.ts'), 'utf8'), /^import \{ itemAPI, token \} from '\.\/items';$/m);
  assert.deepEqual(typeErrors(dir, { noUnusedLocals: true }), []);
  const { hash, move } = proofs(before, srcFiles(dir));
  assert.deepEqual([...hash, ...move.failures], []);
});

// ---------------------------------------------------------------- refusals

/** Runs the fixture with an edited source or spec; returns the refusal. */
function refusal(t, { source = (s) => s, spec = () => {}, files = {} }) {
  const dir = copyTree(t, FIXTURE);
  write(dir, { [SOURCE]: source(fs.readFileSync(path.join(dir, SOURCE), 'utf8')), ...files });
  const raw = fixtureRaw();
  spec(raw);
  const before = snapshot(dir);
  let err;
  try {
    apply(dir, parseSpec(raw), { typecheck: false });
  } catch (e) {
    err = e;
  }
  assert.ok(err instanceof Refusal, `expected a refusal, got ${err ? err.stack : 'a move'}`);
  assert.deepEqual(snapshot(dir), before, 'a refusal writes nothing');
  return err.reasons;
}
const mod = (raw, p) => raw.modules.find((m) => m.path === p);
const relist = (raw, name, to) => {
  for (const m of raw.modules) m.declarations = m.declarations.filter((n) => n !== name);
  mod(raw, to).declarations.push(name);
};

test('it refuses what it cannot move whole, naming it, and writes nothing', async (t) => {
  const cases = [
    ['statements with side effects split', { spec: (raw) => relist(raw, 'client', 'src/api/items.ts') }, /^the statements with side effects must move together to one module .*API_URL \(line 14\) in src\/api\/http\.ts, client \(line 16\) in src\/api\/items\.ts/],
    ['a target module that exists', { files: { 'src/api/types/items.ts': 'export {};\n' } }, /^src\/api\/types\/items\.ts exists; a move creates its modules$/],
    ['a module over the bound', { source: (s) => `${s}\nexport interface Huge {\n${'  field: string;\n'.repeat(MODULE_MAX_LINES)}}\n`, spec: (raw) => mod(raw, 'src/api/types/items.ts').declarations.push('Huge') }, new RegExp(`^src/api/types/items\\.ts would be \\d+ lines, over the ${MODULE_MAX_LINES} the F1 row allows; split it in the spec$`)],
    ['a barrel over the bound', { spec: (raw) => (raw.barrelHeader = Array.from({ length: BARREL_MAX_LINES }, (_, i) => `// line ${i}`).join('\n')) }, new RegExp(`^the barrel src/api/client\\.ts would be \\d+ lines, over the ${BARREL_MAX_LINES} the F1 row allows$`)],
    ['two statements on one line', { source: (s) => s.replace('const PAGE = 100;', 'const PAGE = 100; const MORE = 1;') }, /^src\/api\/client\.ts:56: two statements on one line; put each on its own line$/],
    ['export default <expression>', { source: (s) => s.replace('export default client;', 'export default { client };') }, /`export default <expression>` cannot move whole/],
    ['export default on a declaration', { source: (s) => `${s}\nexport default function main() {}\n` }, /`export default` on a declaration cannot move whole/],
    ['declare global', { source: (s) => `${s}\ndeclare global {\n  interface Window {\n    fixture: string;\n  }\n}\n` }, /a `declare global` or `declare module '...'` block applies where it sits/],
    ["declare module '...'", { source: (s) => `${s}\ndeclare module 'fixture' {}\n` }, /a `declare global` or `declare module '...'` block applies where it sits/],
    ['export =', { source: (s) => `${s}\nexport = client;\n` }, /`export =` cannot move/],
    ['import x = require()', { source: (s) => `import fs = require('fs');\n${s}` }, /^src\/api\/client\.ts:1: `import fs = \.\.\.` cannot move; write it as an import declaration$/],
    ['a statement with no runtime declaration before it', { source: (s) => `console.log('first');\n${s}` }, /^src\/api\/client\.ts:1: \(statement\) declares nothing and has no runtime declaration before it to move with$/],
    ['one statement split across modules', { source: (s) => `${s}\nexport const one = 1, two = 2;\n`, spec: (raw) => { mod(raw, 'src/api/items.ts').declarations.push('one'); mod(raw, 'src/api/widgets.ts').declarations.push('two'); } }, /: one, two are declared by one statement, which the spec splits across src\/api\/items\.ts and src\/api\/widgets\.ts$/],
    ['an import only a shadowing local would use', { source: (s) => `${s}\nexport const shadowAPI = { get: (PAGE: number) => PAGE };\n`, spec: (raw) => mod(raw, 'src/api/widgets.ts').declarations.push('shadowAPI') }, /^src\/api\/widgets\.ts would import PAGE for declarations that use a local of that name instead/],
    // Unnamed, token joins api/accounts.ts (default.values); itemAPI assigns it.
    ['a let apart from a declaration that assigns it', { source: (s) => s.replace('const PAGE = 100;\n', "const PAGE = 100;\n\nlet token = '';\n").replace('  link: (id: string)', '  login: (t: string): void => {\n    token = t;\n  },\n  link: (id: string)') }, /^token \(src\/api\/client\.ts:58\) is assigned by itemAPI \(line \d+\), which the plan puts in src\/api\/items\.ts, but token lands in src\/api\/accounts\.ts; a module cannot assign a binding it imports: place them in one module in the spec$/],
    ['a var counted from another module', { source: (s) => `${s}\nvar calls = 0;\n`.replace('  item: (id: string) => itemAPI.link(id),', '  item: (id: string) => {\n    calls++;\n    return itemAPI.link(id);\n  },'), spec: (raw) => mod(raw, 'src/api/items.ts').declarations.push('calls') }, /^calls \(src\/api\/client\.ts:\d+\) is assigned by widgetAPI \(line 85\), which the plan puts in src\/api\/widgets\.ts, but calls lands in src\/api\/items\.ts/],
    ['a let destructured into from another module', { source: (s) => `${s}\nlet last = '';\n`.replace('  item: (id: string) => itemAPI.link(id),', '  item: (id: string) => {\n    [last] = [id];\n    return itemAPI.link(id);\n  },'), spec: (raw) => mod(raw, 'src/api/items.ts').declarations.push('last') }, /^last \(src\/api\/client\.ts:\d+\) is assigned by widgetAPI \(line 85\), which the plan puts in src\/api\/widgets\.ts, but last lands in src\/api\/items\.ts/],
    ['a runtime import cycle', { source: (s) => s.replace('  link: (id: string)', '  widgets: () => widgetAPI.list(),\n  link: (id: string)') }, /^new import cycle src\/api\/items\.ts src\/api\/widgets\.ts: the modules would import each other at run time/],
    // client.ts loads csrf.ts, which sets an axios default, before its own
    // statements; after the split only accounts.ts, which the barrel loads
    // after http.ts, imports it, so the default is set after axios.create.
    ['an import with side effects only a later module keeps', {
      files: { 'src/utils/csrf.ts': "import axios from 'axios';\n\naxios.defaults.headers.common['X-CSRF'] = 'on';\n\nexport const csrfField = 'csrf';\n" },
      source: (s) => s.replace("import { fileName } from './files';\n", "import { fileName } from './files';\nimport { csrfField } from '../utils/csrf';\n").replace("client.get<Account>('/api/me')", "client.get<Account>('/api/me', { params: { csrfField } })"),
    }, /^effects reordered when src\/api\/client\.ts loads: API_URL in src\/api\/http\.ts now runs before \(statement\) in src\/utils\/csrf\.ts, which src\/api\/client\.ts ran first; place the declarations so that src\/api\/http\.ts imports src\/utils\/csrf\.ts \(one that uses it, say\), or settle it in the source$/],
  ];
  for (const [name, edit, want] of cases) {
    await t.test(name, (t) => {
      const reasons = refusal(t, edit);
      assert.ok(reasons.some((r) => want.test(r)), `no refusal matches ${want}:\n${reasons.join('\n')}`);
    });
  }
});

test('it refuses statements with side effects that nothing the barrel loads imports', (t) => {
  const dir = copyTree(t, FIXTURE);
  write(dir, {
    [SOURCE]: "import { base } from './base';\n\nexport interface T {\n  n: number;\n}\n\nconst booted = base();\n",
    'src/consumer.ts': "import type { T } from './api/client';\n\nexport const n = (t: T): number => t.n;\n",
    'src/consumer.test.ts': 'export {};\n',
  });
  const spec = parseSpec({
    source: SOURCE,
    barrelHeader: '// The barrel.',
    default: { types: 'src/api/types.ts', values: 'src/api/boot.ts' },
    modules: [
      { path: 'src/api/types.ts', header: '// Types.', declarations: ['T'] },
      { path: 'src/api/boot.ts', header: '// Boot.', declarations: ['booted'] },
    ],
  });
  const before = snapshot(dir);
  assert.throws(
    () => apply(dir, spec, { typecheck: false }),
    (e) => e instanceof Refusal && e.reasons.some((r) => /^moved booted: src\/api\/client\.ts -> src\/api\/boot\.ts changes when it runs .*: nothing src\/api\/client\.ts loads imports src\/api\/boot\.ts at run time/.test(r)),
  );
  assert.deepEqual(snapshot(dir), before);
});

test('a malformed spec is a spec error', async (t) => {
  const base = fixtureRaw();
  // Each case edits a copy of fixture.json in place.
  const cases = [
    ['an unknown key', (raw) => (raw.extra = 1), /unknown key "extra"/],
    ['a description that is not a string', (raw) => (raw.description = 1), /description is not a string/],
    ['a source outside src/', (raw) => (raw.source = 'api/client.ts'), /source "api\/client\.ts" is not the path of a \.ts or \.tsx module under src\//],
    ['a path with ..', (raw) => (mod(raw, 'src/api/items.ts').path = 'src/../items.ts'), /is not the path of a \.ts or \.tsx module under src\//],
    ['a test module', (raw) => (mod(raw, 'src/api/items.ts').path = 'src/api/items.test.ts'), /is not the path of a \.ts or \.tsx module under src\//],
    ['a declaration file', (raw) => (mod(raw, 'src/api/items.ts').path = 'src/api/items.d.ts'), /is not the path of a \.ts or \.tsx module under src\//],
    ['the source as a module', (raw) => (mod(raw, 'src/api/items.ts').path = SOURCE), /src\/api\/client\.ts is the source, which becomes the barrel/],
    ['a module twice', (raw) => (mod(raw, 'src/api/widgets.ts').path = 'src/api/items.ts'), /src\/api\/items\.ts is listed twice/],
    ['a module that differs in case', (raw) => (mod(raw, 'src/api/widgets.ts').path = 'src/api/Items.ts'), /src\/api\/Items\.ts is listed twice, or names the same module as another path/],
    ['a .ts and a .tsx of one name', (raw) => (mod(raw, 'src/api/widgets.ts').path = 'src/api/items.tsx'), /names the same module as another path/],
    ['no header', (raw) => delete mod(raw, 'src/api/items.ts').header, /src\/api\/items\.ts: header is missing/],
    ['a header that is code', (raw) => (mod(raw, 'src/api/items.ts').header = 'export const x = 1;'), /src\/api\/items\.ts: header is not only a comment/],
    ['a header that is prose', (raw) => (mod(raw, 'src/api/items.ts').header = 'Items.'), /src\/api\/items\.ts: header is not only a comment/],
    ['no barrel header', (raw) => delete raw.barrelHeader, /barrelHeader is missing/],
    ['declarations that are not a list', (raw) => (mod(raw, 'src/api/items.ts').declarations = 'itemAPI'), /declarations is not a list of names/],
    ['a statement named', (raw) => mod(raw, 'src/api/http.ts').declarations.push('(statement)'), /"\(statement\)" is not a declaration name/],
    ['a name twice', (raw) => mod(raw, 'src/api/widgets.ts').declarations.push('itemAPI'), /itemAPI is listed in src\/api\/items\.ts and src\/api\/widgets\.ts/],
    ['a module key unknown', (raw) => (mod(raw, 'src/api/items.ts').names = []), /modules\[4\]: unknown key "names"/],
    ['a module that is not an object', (raw) => (raw.modules[0] = 'src/api/http.ts'), /modules\[0\] is not an object/],
    ['no modules', (raw) => (raw.modules = []), /modules is not a non-empty list/],
    ['no default', (raw) => delete raw.default, /default is missing/],
    ['a default key unknown', (raw) => (raw.default.other = 'src/api/items.ts'), /default: unknown key "other"/],
    ['a default that is not a module', (raw) => (raw.default.types = 'src/api/other.ts'), /default\.types "src\/api\/other\.ts" is not one of the spec's modules/],
  ];
  for (const [name, edit, want] of cases) {
    await t.test(name, () => {
      const raw = structuredClone(base);
      edit(raw);
      assert.throws(() => parseSpec(raw), (e) => e instanceof SpecError && want.test(e.message), `expected a spec error matching ${want}`);
    });
  }
  for (const raw of [[], null, 'spec']) assert.throws(() => parseSpec(raw), (e) => e instanceof SpecError && /not a JSON object/.test(e.message));
  const bad = path.join(tmp(t), 'bad.json');
  fs.writeFileSync(bad, '{');
  assert.throws(() => loadSpec(bad), (e) => e instanceof SpecError && e.message.startsWith(bad));
  assert.doesNotThrow(() => loadSpec(F1_SPEC));
});

test('the self-check refuses each kind of damage to a rendered move', async (t) => {
  const files = srcFiles(FIXTURE);
  const spec = fixtureSpec();
  const good = () => {
    const r = plan(spec, readSource(SOURCE, files.get(SOURCE)));
    return { ...r, files: new Map(r.files), map: r.map.map((m) => ({ ...m, names: [...m.names] })) };
  };
  assert.doesNotThrow(() => selfCheck(files, spec, good()));
  const edit = (r, p, from, to) => {
    if (p === SOURCE) {
      assert.ok(r.barrel.includes(from), `${p} has no ${from}`);
      r.barrel = r.barrel.replace(from, to);
    } else {
      assert.ok(r.files.get(p).includes(from), `${p} has no ${from}`);
      r.files.set(p, r.files.get(p).replace(from, to));
    }
    return r;
  };
  // Swaps http.ts's two interceptors (and so uploadConfig with them).
  const interceptors = (r) => {
    const http = r.files.get('src/api/http.ts');
    const at = ['// Tag every request.', 'const uploadConfig', 'client.interceptors.response', '// Hand the browser'].map((m) => http.indexOf(m));
    assert.ok(at.every((p, i) => p > 0 && (i === 0 || p > at[i - 1])), `http.ts has its interceptors where expected: ${at}`);
    const [req, mid, res] = [http.slice(at[0], at[1]), http.slice(at[1], at[2]), http.slice(at[2], at[3])];
    r.files.set('src/api/http.ts', http.slice(0, at[0]) + res + mid + req + http.slice(at[3]));
    return r;
  };
  const cases = [
    ['a moved declaration edited', (r) => edit(r, 'src/api/items.ts', 'limit: PAGE', 'limit: PAGE + 1'), /tsmovecheck: .*itemAPI/],
    ['an import dropped', (r) => edit(r, 'src/api/items.ts', "import { fileName } from './files';\n", ''), /itemAPI.*fileName|fileName.*itemAPI/],
    ['an import bound to another declaration', (r) => edit(r, 'src/api/items.ts', "from './files'", "from './base'"), /itemAPI/],
    ['the export modifier added instead of a list', (r) => edit(edit(r, 'src/api/http.ts', 'const save =', 'export const save ='), 'src/api/http.ts', ', save };', ' };'), /tsdeclhash --no-module: changed save/],
    ['statements with side effects reordered', interceptors, /reordered src\/api\/http\.ts/],
    ['a statement with side effects moved apart', (r) => {
      const http = r.files.get('src/api/http.ts');
      const res = http.slice(http.indexOf('client.interceptors.response'), http.indexOf('// Hand the browser'));
      edit(r, 'src/api/http.ts', res, '');
      return edit(r, 'src/api/items.ts', 'export const itemAPI', `${res}export const itemAPI`);
    }, /changes when it runs/],
    ['a type exported in a value list', (r) => edit(r, 'src/api/types/items.ts', 'export type { ItemQuery };', 'export { ItemQuery };'), /^src\/api\/types\/items\.ts:\d+:\d+: Re-exporting a type when 'isolatedModules' is enabled requires using 'export type'\.$/],
    ['a re-export dropped from the barrel', (r) => edit(r, SOURCE, "export * from './accounts';\n", ''), /the barrel no longer exports accountAPI/],
    ['a name the source did not export', (r) => edit(r, SOURCE, "export * from './accounts';\n", "export * from './accounts';\nexport { save } from './http';\n"), /the barrel exports save, which the source did not/],
    ['a declaration left in the barrel', (r) => edit(r, SOURCE, 'export default client;\n', 'export default client;\n\nconst leftover = 1;\n'), /the barrel still declares leftover/],
    ['a declaration elsewhere than planned', (r) => {
      r.map.find((m) => m.path === 'src/api/widgets.ts').names.push('accountAPI');
      return r;
    }, /accountAPI is not in src\/api\/widgets\.ts, where the plan puts it/],
  ];
  for (const [name, tamper, want] of cases) {
    await t.test(name, () => {
      assert.throws(
        () => selfCheck(files, spec, tamper(good())),
        (e) => e instanceof CheckError && e.problems.some((p) => want.test(p)),
        `expected a CheckError matching ${want}`,
      );
    });
  }
});

test('the bounds are the F1 row\'s', () => {
  const row = fs
    .readFileSync(path.join(REPO, 'docs/plans/codebase-refactor.md'), 'utf8')
    .split('\n')
    .find((l) => l.startsWith('| **F1**'));
  assert.ok(row, 'the plan has an F1 row');
  assert.match(row, new RegExp(`\`client\\.ts\` ≤ ${BARREL_MAX_LINES} lines and the largest module ≤ ${MODULE_MAX_LINES}`));
});

// ------------------------------------------------------------------- F1

test('F1 on d11dee8: the committed spec moves its client.ts into modules that type-check, a pure move with the same surface', (t) => {
  // CI's checkout holds one commit; where d11dee8 is there, the copy is its.
  const sha = spawnSync('git', ['-C', REPO, 'rev-parse', '--verify', '--quiet', 'd11dee8^{commit}'], { encoding: 'utf8' });
  const files = snapshot(D11DEE8);
  assert.deepEqual([...files.keys()], ['src/api/baseURL.ts', 'src/api/client.ts', 'src/api/contentDisposition.ts', 'src/utils/downloadSelection.ts', 'src/utils/publicPaths.ts', 'src/utils/randomProduct.ts']);
  if (sha.status === 0) {
    for (const [p, bytes] of files) {
      const git = execFileSync('git', ['-C', REPO, 'cat-file', 'blob', `d11dee8:frontend/${p}`], { maxBuffer: 1 << 26 });
      assert.ok(git.equals(bytes), `testdata/tsdeclmove/d11dee8/${p} is not d11dee8's frontend/${p}`);
    }
  } else t.diagnostic('d11dee8 is not in this clone (a shallow checkout): the copy is not compared with git');

  const dir = copyTree(t, D11DEE8);
  assert.deepEqual(typeErrors(dir), [], "d11dee8's client.ts type-checks before the move");
  const before = srcFiles(dir);
  const original = before.get(SOURCE);
  const src = readSource(SOURCE, original);
  const decls = src.statements.filter((s) => s.kind === 'decl');
  const named = decls.flatMap((s) => s.named);
  // What F1 moves, as d11dee8 has it: the counts the plan's row gives.
  assert.equal(decls.filter((s) => s.type).length, 145, 'types');
  assert.equal(named.filter((n) => /API$/.test(n)).length, 54, 'xxxAPI objects');
  assert.equal(decls.length, 212, 'declarations');
  const surfaceBefore = surface(dir, SOURCE);

  const r = apply(dir, loadSpec(F1_SPEC), { typecheck: false });
  for (const n of r.notes) t.diagnostic(`note: ${n}`);
  assert.equal(r.moved, 212);
  const errors = typeErrors(dir, { noUnusedLocals: true });
  const generated = new Set([SOURCE, ...r.map.map((m) => m.path)]);
  assert.deepEqual(errors.filter((e) => generated.has(e.split(':')[0]) || !/is declared but/.test(e)), [], 'the moved tree type-checks, with no unused import in a module the move wrote');
  const { hash, move } = proofs(before, srcFiles(dir));
  assert.deepEqual(hash, [], 'tsdeclhash --no-module');
  assert.deepEqual(move.failures, [], 'tsmovecheck');
  assert.equal(move.moved.length, 212);
  assert.deepEqual(surface(dir, SOURCE), surfaceBefore, "the barrel's surface is client.ts's (S12's clientSurface reading)");

  // The F1 row's bounds and its api/http.ts.
  const barrel = fs.readFileSync(path.join(dir, SOURCE), 'utf8');
  assert.ok(lineCount(barrel) <= BARREL_MAX_LINES, `the barrel is ${lineCount(barrel)} lines`);
  for (const m of r.map) assert.ok(m.lines <= MODULE_MAX_LINES, `${m.path} is ${m.lines} lines`);
  assert.deepEqual(r.map.find((m) => m.path === 'src/api/http.ts')?.names, ['API_BASE_URL', 'client', '(statement)', 'uploadConfig', '(statement)', 'saveBlob', 'downloadBlob']);
  assert.match(barrel, /^export default client;$/m);
  // Every declaration's text, with its comments, is in its module byte for byte.
  const home = new Map(r.map.flatMap((m) => m.names.map((n) => [n, m.path])));
  for (const d of declarationTexts(SOURCE, original)) {
    if (d.names[0].startsWith('(')) continue;
    assert.ok(fs.readFileSync(path.join(dir, home.get(d.names[0])), 'utf8').includes(d.text), `${d.names[0]} is not in ${home.get(d.names[0])} byte for byte`);
  }
});

test('F1 on the working tree: the committed spec generates a move every frontend gate passes', { timeout: 20 * 60_000 }, (t) => {
  const source = fs.readFileSync(path.join(FRONTEND, SOURCE), 'utf8');
  if (!readSource(SOURCE, source).statements.some((s) => s.kind === 'decl')) {
    t.skip(`${SOURCE} declares nothing: F1 has landed`);
    return;
  }
  // A copy of the repository: frontend/src and every file at the top of
  // frontend/ are copied (so Vite resolves its root, and the configs every
  // file they name, such as a setup file, in the copy), its other
  // directories linked, but not its build output; node_modules linked entry
  // by entry (so the copy's tool caches stay in the copy); and the rest of
  // the repository, which src/arch reads, linked.
  const repo = tmp(t, 'tsdeclmove-repo-');
  for (const e of fs.readdirSync(REPO)) {
    if (e === 'frontend' || e.startsWith('.')) continue;
    fs.symlinkSync(path.join(REPO, e), path.join(repo, e));
  }
  const fe = path.join(repo, 'frontend');
  fs.mkdirSync(path.join(fe, 'node_modules'), { recursive: true });
  for (const e of fs.readdirSync(path.join(FRONTEND, 'node_modules'))) {
    if (e.startsWith('.') && e !== '.bin') continue; // tool caches (.vite, .vite-temp) stay in the copy
    fs.symlinkSync(path.join(FRONTEND, 'node_modules', e), path.join(fe, 'node_modules', e));
  }
  fs.cpSync(path.join(FRONTEND, 'src'), path.join(fe, 'src'), { recursive: true });
  for (const e of fs.readdirSync(FRONTEND)) {
    if (['src', 'node_modules', 'build'].includes(e)) continue;
    const from = path.join(FRONTEND, e);
    if (!fs.statSync(from).isDirectory()) fs.copyFileSync(from, path.join(fe, e));
    else if (!e.startsWith('.')) fs.symlinkSync(from, path.join(fe, e)); // not a tool's cache
  }
  const git = (...args) => execFileSync('git', ['-c', 'user.name=t', '-c', 'user.email=t@example.com', '-c', 'commit.gpgsign=false', '-c', 'gc.auto=0', '-c', 'maintenance.auto=false', '-c', 'core.hooksPath=/dev/null', ...args], { cwd: repo, stdio: 'pipe' });
  const sh = (cmd, args, what) => {
    const r = spawnSync(cmd, args, { cwd: fe, encoding: 'utf8', env: childEnv(), maxBuffer: 1 << 28 });
    assert.equal(r.status, 0, `${what} failed on the moved tree:\n${(r.stdout + r.stderr).slice(-6000)}`);
    return r.stdout;
  };
  const cssHashes = () =>
    fs
      .readdirSync(path.join(fe, 'build/assets'))
      .filter((f) => f.endsWith('.css'))
      .sort()
      .map((f) => `${f} ${createHash('sha256').update(fs.readFileSync(path.join(fe, 'build/assets', f))).digest('hex')}`);
  git('init', '-q');
  git('add', 'frontend/src');
  git('commit', '-q', '-m', 'base');
  sh('npm', ['run', 'build'], 'the base build');
  const cssBefore = cssHashes();
  fs.rmSync(path.join(fe, 'build'), { recursive: true });

  const gen = spawnSync(process.execPath, [SCRIPT, '--no-typecheck', '--root', fe, '--spec', F1_SPEC], { encoding: 'utf8', env: childEnv() });
  for (const l of gen.stdout.split('\n').filter((l) => l.startsWith('note: '))) t.diagnostic(l);
  if (gen.status === 1 && /^refused: /m.test(gen.stdout)) {
    // The spec's to settle when F1 regenerates (R4), not a feature PR's (§8.4).
    t.skip(`tsdeclmove refuses F1 on this tree, which F1's author settles in specs/F1.json: ${gen.stdout.split('\n').filter((l) => l.startsWith('refused: ')).join('; ')}`);
    return;
  }
  assert.equal(gen.status, 0, gen.stdout + gen.stderr);
  git('add', '-A', 'frontend/src');
  git('commit', '-q', '-m', 'F1');

  // The Refactor guard's class A proof, as it runs it on F1's commit.
  sh(process.execPath, [path.join(SCRIPTS, 'tsdeclhash.mjs'), '--root', fe, '--base', 'HEAD~1', '--head', 'HEAD', '--no-module'], 'tsdeclhash');
  const mc = sh(process.execPath, [path.join(SCRIPTS, 'tsmovecheck.mjs'), '--root', fe, '--head', 'HEAD', 'HEAD~1'], 'tsmovecheck');
  t.diagnostic(mc.trim().split('\n').at(-1));
  // The F1 row's bounds.
  const barrel = fs.readFileSync(path.join(fe, SOURCE), 'utf8');
  assert.ok(lineCount(barrel) <= BARREL_MAX_LINES, `the barrel is ${lineCount(barrel)} lines`);
  for (const l of gen.stdout.split('\n')) {
    const m = l.match(/^(src\/\S+) \((\d+) lines\): /);
    if (m) assert.ok(Number(m[2]) <= MODULE_MAX_LINES, `${m[1]} is ${m[2]} lines`);
  }
  // Every gate of the frontend job, and S12's snapshots byte for byte.
  sh('npx', ['tsc', '--noEmit'], 'npx tsc --noEmit');
  sh('npm', ['run', 'lint'], 'npm run lint');
  sh('npx', ['vitest', 'run'], 'npx vitest run');
  const snaps = 'src/arch/__snapshots__';
  assert.deepEqual(snapshot(path.join(fe, snaps)), snapshot(path.join(FRONTEND, snaps)), `S12's snapshots (${snaps}) changed on the moved tree`);
  sh('npm', ['run', 'build'], 'npm run build');
  assert.deepEqual(cssHashes(), cssBefore, 'the build emits the same CSS before and after the move (what S12b will check)');
});
