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
  const git = (...args) => execFileSync('git', ['-c', 'user.name=t', '-c', 'user.email=t@example.com', ...args], { cwd: repo, stdio: 'pipe' });
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
});
