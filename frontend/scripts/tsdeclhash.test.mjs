// Tests for tsdeclhash.mjs. Run: cd frontend && node --test 'scripts/*.test.mjs'
import test from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { analyzeTree, diffManifests, manifest } from './tsdeclhash.mjs';

const SCRIPT = path.join(path.dirname(fileURLToPath(import.meta.url)), 'tsdeclhash.mjs');
const hashes = (files, opts) => manifest(analyzeTree(new Map(Object.entries(files))), opts);
const keys = (lines) => lines.map((l) => l.slice(0, l.lastIndexOf('\t')));

const x = { 'src/x.ts': 'export function x(): number {\n  return 1;\n}\n' };

const before = {
  ...x,
  'src/a.ts': [
    "import { x } from './x';",
    '',
    '/** f doubles x. */',
    'export function f(): number {',
    '  return x() * 2; // twice',
    '}',
    '',
    'export const g = 1;',
    '',
  ].join('\n'),
  'src/use.ts': "import { f } from './a';\n\nexport const used = f();\n",
};

// f moves to b.ts, taking its import; use.ts imports it from there.
const after = {
  ...x,
  'src/a.ts': 'export const g = 1;\n',
  'src/b.ts': [
    "import { x } from './x';",
    '',
    '/** f doubles x. */',
    'export function f(): number {',
    '  return x() * 2; // twice',
    '}',
    '',
  ].join('\n'),
  'src/use.ts': "import { f } from './b';\n\nexport const used = f();\n",
};

test('declarations are keyed by module and name, imports and exports excluded', () => {
  const lines = hashes({
    'src/m.tsx': [
      "import React from 'react';",
      "import './m.css';",
      "export { other } from './other';",
      'export interface I { a: number }',
      'export type T = I | null;',
      'enum E { A, B }',
      'export class C {}',
      'export const { p, q: [r] } = { p: 1, q: [2] };',
      'let s = 1, t = 2;',
      "console.log('side effect');",
      'export default { s, t };',
      'const local = 1;',
      'export { local, E };',
      'export const el = <div>{React.version}</div>;',
    ].join('\n'),
  });
  assert.deepEqual(keys(lines), [
    'src/m.tsx\t(statement)',
    'src/m.tsx\tC',
    'src/m.tsx\tE',
    'src/m.tsx\tI',
    'src/m.tsx\tT',
    'src/m.tsx\tdefault',
    'src/m.tsx\tel',
    'src/m.tsx\tlocal',
    'src/m.tsx\tp',
    'src/m.tsx\tr',
    'src/m.tsx\ts',
    'src/m.tsx\tt',
  ]);
});

test('a move between modules keeps every hash', () => {
  const b = hashes(before, { noModule: true });
  const h = hashes(after, { noModule: true });
  assert.deepEqual(diffManifests(b, h).diffs, []);
  // With module paths, the move shows as one removal and one addition.
  assert.deepEqual(diffManifests(hashes(before), hashes(after)).diffs, ['removed\tsrc/a.ts\tf', 'added\tsrc/b.ts\tf']);
});

test('text, attached comments and bindings are part of the hash', () => {
  const b = hashes(after, { noModule: true });
  const changed = (files) => diffManifests(b, hashes(files, { noModule: true })).diffs;
  const edit = (from, to) => ({ ...after, 'src/b.ts': after['src/b.ts'].replace(from, to) });
  // used calls f, so it is bound to a changed declaration and differs too.
  assert.deepEqual(changed(edit('* 2', '* 3')), ['changed\tf', 'changed\tused']);
  assert.deepEqual(changed(edit('/** f doubles x. */', '/** f doubles it. */')), ['changed\tf', 'changed\tused']);
  assert.deepEqual(changed(edit('// twice', '// two times')), ['changed\tf', 'changed\tused']);
  // A comment separated by a blank line is not attached.
  assert.deepEqual(changed(edit("import { x } from './x';\n", "import { x } from './x';\n// loose\n")), []);
  // The same text bound to a different x.
  const rebound = { ...edit("from './x'", "from './y'"), 'src/y.ts': 'export function x(): number {\n  return 2;\n}\n' };
  assert.deepEqual(changed(rebound), ['changed\tf', 'changed\tx']);
  // ...but not when the x it now names is the same declaration, moved.
  const movedDep = { ...edit("from './x'", "from './y'"), 'src/y.ts': x['src/x.ts'] };
  delete movedDep['src/x.ts'];
  assert.deepEqual(changed(movedDep), []);
});

test('re-exports and module paths in calls resolve by content', () => {
  const base = {
    'src/views/V.tsx': 'export default function V() {\n  return null;\n}\n',
    'src/App.tsx': "const V = lazy(() => import('./views/V'));\nvi.mock('./views/V');\n",
    'src/ui/Button.tsx': 'export function Button() {\n  return null;\n}\n',
    'src/ui/index.ts': "export { Button } from './Button';\n",
    'src/page.tsx': "import { Button } from './ui';\n\nexport const page = Button();\n",
  };
  const head = {
    'src/pages/V.tsx': base['src/views/V.tsx'],
    'src/App.tsx': "const V = lazy(() => import('./pages/V'));\nvi.mock('./pages/V');\n",
    'src/ui/buttons/Button.tsx': base['src/ui/Button.tsx'],
    'src/ui/index.ts': "export * from './buttons/Button';\n",
    'src/page.tsx': base['src/page.tsx'],
  };
  assert.deepEqual(diffManifests(hashes(base, { noModule: true }), hashes(head, { noModule: true })).diffs, []);
  const other = { ...head, 'src/pages/V.tsx': 'export default function V() {\n  return 1;\n}\n' };
  assert.deepEqual(diffManifests(hashes(base, { noModule: true }), hashes(other, { noModule: true })).diffs, [
    'changed\t(statement)',
    'changed\tV',
  ]);
});

test('the command line writes a manifest and compares two', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'tsdeclhash-'));
  try {
    const write = (files) => {
      fs.rmSync(path.join(dir, 'src'), { recursive: true, force: true });
      for (const [p, text] of Object.entries(files)) {
        fs.mkdirSync(path.dirname(path.join(dir, p)), { recursive: true });
        fs.writeFileSync(path.join(dir, p), text);
      }
    };
    const run = (...args) => spawnSync(process.execPath, [SCRIPT, ...args], { encoding: 'utf8' });
    write(before);
    assert.equal(run('--root', dir, '--no-module', '-o', path.join(dir, 'base.txt')).status, 0);
    write(after);
    assert.equal(run('--root', dir, '--no-module', '-o', path.join(dir, 'head.txt')).status, 0);
    const same = run('--compare', path.join(dir, 'base.txt'), path.join(dir, 'head.txt'));
    assert.equal(same.status, 0, same.stdout);
    write({ ...after, 'src/b.ts': after['src/b.ts'].replace('* 2', '* 4') });
    assert.equal(run('--root', dir, '--no-module', '-o', path.join(dir, 'head.txt')).status, 0);
    const differ = run('--compare', path.join(dir, 'base.txt'), path.join(dir, 'head.txt'));
    assert.equal(differ.status, 1);
    assert.match(differ.stdout, /^changed\tf\n/);
    assert.equal(run('--bogus').status, 2);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});
