// Tests for tsmovecheck.mjs. Run: cd frontend && node --test 'scripts/*.test.mjs'
import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { analyzeTree } from './tsdeclhash.mjs';
import { moveCheck } from './tsmovecheck.mjs';

const SCRIPT = path.join(path.dirname(fileURLToPath(import.meta.url)), 'tsmovecheck.mjs');
const tree = (files) => analyzeTree(new Map(Object.entries(files)));

const base = {
  'src/utils/format.ts': [
    "import { pad } from './pad';",
    '',
    '/** Formats a count. */',
    'export function formatCount(n: number): string {',
    '  return pad(String(n));',
    '}',
    '',
    'export const UNIT = "items";',
    '',
  ].join('\n'),
  'src/utils/pad.ts': "export function pad(s: string): string {\n  return s.padStart(3, ' ');\n}\n",
  'src/view.ts': "import { formatCount, UNIT } from './utils/format';\n\nexport const label = formatCount(3) + UNIT;\n",
};

// formatCount moves to count.ts with its import; view.ts imports it anew.
const moved = {
  ...base,
  'src/utils/format.ts': 'export const UNIT = "items";\n',
  'src/utils/count.ts': [
    "import { pad } from './pad';",
    '',
    '/** Formats a count. */',
    'export function formatCount(n: number): string {',
    '  return pad(String(n));',
    '}',
    '',
  ].join('\n'),
  'src/view.ts': "import { UNIT } from './utils/format';\nimport { formatCount } from './utils/count';\n\nexport const label = formatCount(3) + UNIT;\n",
};

test('a move with its imports fixed passes', () => {
  const r = moveCheck(tree(base), tree(moved));
  assert.deepEqual(r.failures, []);
  assert.deepEqual(r.moved, ['moved formatCount: src/utils/format.ts -> src/utils/count.ts']);
  assert.deepEqual(r.rewired, ['rewired src/utils/count.ts', 'rewired src/utils/format.ts', 'rewired src/view.ts']);
  assert.equal(r.unchanged, 3);
});

test('an edited body fails, naming the declaration and its users', () => {
  const edited = { ...moved, 'src/utils/count.ts': moved['src/utils/count.ts'].replace('String(n)', 'String(n + 1)') };
  assert.deepEqual(moveCheck(tree(base), tree(edited)).failures, [
    'added src/utils/count.ts formatCount',
    'changed src/view.ts label (same text, but what formatCount refers to changed)',
    'removed src/utils/format.ts formatCount',
  ]);
});

test('importing a different declaration of the same name fails', () => {
  const rebound = {
    ...moved,
    'src/utils/count.ts': moved['src/utils/count.ts'].replace("from './pad'", "from './pad2'"),
    'src/utils/pad2.ts': "export function pad(s: string): string {\n  return s.padEnd(3, ' ');\n}\n",
  };
  assert.deepEqual(moveCheck(tree(base), tree(rebound)).failures, [
    'added src/utils/pad2.ts pad',
    'changed src/utils/count.ts formatCount: moved from src/utils/format.ts (same text, but what pad refers to changed)',
  ]);
});

test('wiring alone may change: import order, export lists becoming re-exports', () => {
  const b = {
    'src/api/types.ts': 'export interface Item {\n  id: string;\n}\n',
    'src/api/client.ts': "import type { Item } from './types';\n\nexport type { Item };\nexport function get(): Item[] {\n  return [];\n}\n",
  };
  const h = {
    'src/api/types.ts': b['src/api/types.ts'],
    'src/api/items.ts': "import type { Item } from './types';\n\nexport function get(): Item[] {\n  return [];\n}\n",
    'src/api/client.ts': "export type { Item } from './types';\nexport * from './items';\n",
  };
  const r = moveCheck(tree(b), tree(h));
  assert.deepEqual(r.failures, []);
  assert.deepEqual(r.moved, ['moved get: src/api/client.ts -> src/api/items.ts']);
});

test('the command line checks the working tree against a git ref', (t) => {
  const repo = fs.mkdtempSync(path.join(os.tmpdir(), 'tsmovecheck-'));
  t.after(() => fs.rmSync(repo, { recursive: true, force: true }));
  const root = path.join(repo, 'frontend');
  const write = (files) => {
    fs.rmSync(path.join(root, 'src'), { recursive: true, force: true });
    for (const [p, text] of Object.entries(files)) {
      fs.mkdirSync(path.dirname(path.join(root, p)), { recursive: true });
      fs.writeFileSync(path.join(root, p), text);
    }
  };
  const git = (...args) => execFileSync('git', ['-c', 'user.name=t', '-c', 'user.email=t@example.com', '-c', 'commit.gpgsign=false', '-c', 'gc.auto=0', '-c', 'maintenance.auto=false', '-c', 'core.hooksPath=/dev/null', ...args], { cwd: repo, stdio: 'pipe' });
  const run = (...args) => spawnSync(process.execPath, [SCRIPT, '--root', root, ...args], { encoding: 'utf8' });
  write(base);
  git('init', '-q');
  git('add', '.');
  git('commit', '-q', '-m', 'base');
  write(moved);
  const ok = run('HEAD');
  assert.equal(ok.status, 0, ok.stdout + ok.stderr);
  assert.match(ok.stdout, /^moved formatCount: src\/utils\/format\.ts -> src\/utils\/count\.ts$/m);
  assert.match(ok.stdout, /tsmovecheck: a pure move from HEAD to the working tree \(1 moved, 3 unchanged, 3 modules rewired\)/);
  git('add', '-A');
  git('commit', '-q', '-m', 'move');
  assert.equal(run('--head', 'HEAD', 'HEAD~1').status, 0);
  write({ ...moved, 'src/utils/count.ts': moved['src/utils/count.ts'].replace('String(n)', 'String(-n)') });
  const bad = run('HEAD~1');
  assert.equal(bad.status, 1);
  assert.match(bad.stdout, /^added src\/utils\/count\.ts formatCount$/m);
  assert.equal(run().status, 2);
  assert.equal(run('no-such-ref').status, 2);
  // A path with no source file on either side is an error, not a pass.
  for (const p of [path.join(root, 'src/nope'), path.join(root, 'src/**/*.ts')]) {
    const empty = run('HEAD~1', p);
    assert.equal(empty.status, 2, empty.stdout + empty.stderr);
    assert.match(empty.stderr, /no \.ts\/\.tsx files on either side/);
  }
  // A module that exists on one side only is not.
  assert.equal(run('HEAD~1', path.join(root, 'src/utils/count.ts')).status, 1);
});

test('evaluation order: reordered statements and imports, dropped side-effect imports and new cycles fail', () => {
  const b = {
    'src/x.ts': "import './x.css';\nimport './y.css';\nexport const K = 1;\nexport const L = K + 1;\nsetup();\nrender();\n",
    'src/y.ts': "import { L } from './x';\nexport const M = L * 2;\n",
  };
  const reordered = { ...b, 'src/x.ts': "import './y.css';\nimport './x.css';\nexport const K = 1;\nexport const L = K + 1;\nrender();\nsetup();\n" };
  assert.deepEqual(moveCheck(tree(b), tree(reordered)).failures, [
    'reordered imports of src/x.ts: src/y.css now loads before src/x.css',
    'reordered src/x.ts (import src/x.css): now runs after (import src/y.css)',
    'reordered src/x.ts (statement): now runs after (statement)',
  ]);
  const dropped = { ...b, 'src/x.ts': b['src/x.ts'].replace("import './y.css';\n", '') };
  assert.deepEqual(moveCheck(tree(b), tree(dropped)).failures, ['removed src/x.ts (import src/y.css)']);
  // K moves to y.ts, which x.ts now imports while y.ts imports x.ts: M reads
  // L before x.ts has initialised it.
  const cycle = {
    'src/x.ts': "import './x.css';\nimport './y.css';\nimport { K } from './y';\nexport const L = K + 1;\nsetup();\nrender();\n",
    'src/y.ts': "import { L } from './x';\nexport const K = 1;\nexport const M = L * 2;\n",
  };
  assert.deepEqual(moveCheck(tree(b), tree(cycle)).failures, ['new import cycle src/x.ts src/y.ts']);
  // Moving statements to a new module in their order, with no cycle, passes.
  const ok = {
    'src/x.ts': "import './x.css';\nimport './y.css';\nimport { K, L } from './k';\nexport { K, L };\nsetup();\nrender();\n",
    'src/k.ts': 'export const K = 1;\nexport const L = K + 1;\n',
    'src/y.ts': "import { L } from './k';\nexport const M = L * 2;\n",
  };
  assert.deepEqual(moveCheck(tree(b), tree(ok)).failures, []);
  // Named imports are evaluation order too; type-only ones are not.
  const two = {
    'src/a.ts': "import { p } from './p';\nimport { q } from './q';\nimport type { T } from './t';\nimport { U } from './u';\nexport const r: T = p + q + U;\n",
    'src/p.ts': 'export const p = 1;\n',
    'src/q.ts': 'export const q = 2;\n',
    'src/t.ts': 'export type T = number;\n',
    'src/u.ts': 'export type U = number;\nexport const U = 3;\n',
  };
  const swapped = { ...two, 'src/a.ts': two['src/a.ts'].replace("import { p } from './p';\nimport { q } from './q';\n", "import { q } from './q';\nimport { p } from './p';\n") };
  assert.deepEqual(moveCheck(tree(two), tree(swapped)).failures, ['reordered imports of src/a.ts: src/q.ts now loads before src/p.ts']);
  const typeFirst = { ...two, 'src/a.ts': "import type { T } from './t';\n" + two['src/a.ts'].replace("import type { T } from './t';\n", '') };
  assert.deepEqual(moveCheck(tree(two), tree(typeFirst)).failures, []);
});

test('a statement with side effects moves only with all of its module\'s, to a new module the old one loads', () => {
  const b = {
    'src/api/client.ts': [
      "import axios from 'axios';",
      "import { lazy } from 'react';",
      'const client = axios.create();',
      'client.interceptors.request.use((c) => c);',
      'const View = lazy(() => import("./View"));',
      'const limit = { n: 100 };',
      'export const api = { get: () => client.get("/", limit), View };',
      '',
    ].join('\n'),
    'src/api/View.ts': 'export default function View() {\n  return null;\n}\n',
  };
  // The F1 shape: the axios instance and its interceptor go to a new
  // http.ts that client.ts imports; lazy() and a literal are pure.
  const http = "import axios from 'axios';\nconst client = axios.create();\nclient.interceptors.request.use((c) => c);\nexport { client };\n";
  const f1 = {
    ...b,
    'src/api/http.ts': http,
    'src/api/client.ts': [
      "import { lazy } from 'react';",
      "import { client } from './http';",
      'const View = lazy(() => import("./View"));',
      'const limit = { n: 100 };',
      'export const api = { get: () => client.get("/", limit), View };',
      '',
    ].join('\n'),
  };
  assert.deepEqual(moveCheck(tree(b), tree(f1)).failures, []);
  // Moving only one of them splits the effects between two modules.
  const half = {
    ...f1,
    'src/api/http.ts': "import axios from 'axios';\nconst client = axios.create();\nexport { client };\n",
    'src/api/client.ts': f1['src/api/client.ts'].replace("import { client } from './http';\n", "import { client } from './http';\nclient.interceptors.request.use((c) => c);\n"),
  };
  assert.deepEqual(moveCheck(tree(b), tree(half)).failures, [
    'moved client: src/api/client.ts -> src/api/http.ts changes when it runs (it may have side effects; see tsmovecheck\'s header)',
  ]);
  // A target that client.ts does not load, or that existed before, fails.
  const unloaded = { ...f1, 'src/api/client.ts': f1['src/api/client.ts'].replace("import { client } from './http';\n", 'declare const client: any;\n') };
  assert.equal(moveCheck(tree(b), tree(unloaded)).failures.filter((f) => f.includes('changes when it runs')).length, 2);
  const existing = { ...b, 'src/api/http.ts': 'export const x = 1;\n' };
  assert.equal(moveCheck(tree(existing), tree({ ...f1, 'src/api/http.ts': http + 'export const x = 1;\n' })).failures.filter((f) => f.includes('changes when it runs')).length, 2);
});
