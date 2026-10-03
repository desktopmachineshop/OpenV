// Tests for bundle-check.mjs. Run: cd frontend && node --test 'scripts/*.test.mjs'
//
// The shape is read from a real Vite build of a small app made here, with
// packages of its own under node_modules, so the parsing is proved on the
// bundler's actual output; the last tests hold the committed shape to the
// app's sources.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { build } from 'vite';
import {
  DYNAMIC_ENTRY,
  FRONTEND_DIR,
  REGENERATE,
  SHAPE_FILE,
  bundleShape,
  chunkImports,
  diffShapes,
  lazyModules,
  main,
  packageOf,
  render,
} from './bundle-check.mjs';

const EAGER = 'in the entry: loaded eagerly';

const pkg = (name, code, dependencies = {}) => ({
  [`node_modules/${name}/package.json`]: JSON.stringify({ name, version: '1.0.0', type: 'module', main: 'index.js', dependencies }),
  [`node_modules/${name}/index.js`]: code,
});

// An app shaped like ours: an entry with a static import, a package that only
// re-exports another (react-router-dom's shape), two pages behind import(),
// one of them pulling in a heavy package, and a package nothing imports.
const APP = {
  'package.json': JSON.stringify({ name: 'app', dependencies: { heavy: '1', light: '1', shim: '1', unused: '1' } }),
  'index.html': '<!doctype html><html><body><div id="root"></div><script type="module" src="/src/main.ts"></script></body></html>\n',
  ...pkg('heavy', 'export const heavy = (x) => `heavy ${x}`;\n'),
  ...pkg('light', 'export const light = (x) => `light ${x}`;\n'),
  ...pkg('shim', "export * from 'inner';\n", { inner: '1' }),
  ...pkg('inner', 'export const inner = (x) => `inner ${x}`;\n'),
  ...pkg('unused', 'export const unused = 1;\n'),
  'src/main.ts': [
    "import { light } from 'light';",
    "import { inner } from 'shim';",
    "const page = () => import('./pages/Page').then((m) => m.Page);",
    "const other = () => import(`./pages/Other`).then((m) => m.Other);",
    'document.title = light(inner(String(page) + String(other)));',
    '',
  ].join('\n'),
  'src/pages/Page.ts': "import { heavy } from 'heavy';\nexport const Page = () => heavy('page');\n",
  'src/pages/Other.ts': 'export const Other = () => 2;\n',
  // No test, src/arch, src/test (F2's mock helper), test data or snapshot
  // ships, so their import() calls are not pages.
  'src/main.test.ts': "export const ghost = () => import('./pages/Ghost');\n",
  'src/arch/guard.ts': "export const ghost = () => import('../pages/Ghost');\n",
  'src/test/mockApi.ts': "export const ghost = () => import('../pages/Ghost');\n",
  'src/pages/testdata/fixture.ts': "export const ghost = () => import('../Ghost');\n",
  'src/pages/__snapshots__/fixture.ts': "export const ghost = () => import('../Ghost');\n",
  'src/pages/Ghost.ts': 'export const Ghost = 3;\n',
};

function makeApp(files = APP) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'bundle-check-'));
  for (const [rel, text] of Object.entries(files)) {
    fs.mkdirSync(path.dirname(path.join(dir, rel)), { recursive: true });
    fs.writeFileSync(path.join(dir, rel), text);
  }
  return dir;
}

async function viteBuild(dir) {
  await build({
    root: dir,
    configFile: false,
    logLevel: 'silent',
    build: { outDir: 'build', sourcemap: true, assetsInlineLimit: 0, emptyOutDir: true },
  });
}

/** main() with its output captured. */
function run(dir, env = {}, extra = []) {
  const out = [];
  const log = { log: (s) => out.push(s), error: (s) => out.push(s) };
  const code = main(['--root', dir, ...extra], env, log);
  return { code, out: out.join('\n') };
}

const edit = (dir, rel, from, to) => {
  const file = path.join(dir, rel);
  const text = fs.readFileSync(file, 'utf8');
  assert.ok(text.includes(from), `${rel} has no ${from}`);
  fs.writeFileSync(file, text.replace(from, to));
};

test('reads where each dependency lands and which import() targets are dynamic entries', async (t) => {
  const dir = makeApp();
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  await viteBuild(dir);
  const shape = bundleShape(dir);
  assert.deepEqual(shape.dependencies, { heavy: 'lazy', light: 'entry', shim: 'entry', unused: 'not in the build' });
  assert.deepEqual(shape.lazyPages, { 'src/pages/Other.ts': DYNAMIC_ENTRY, 'src/pages/Page.ts': DYNAMIC_ENTRY });
  assert.equal(shape.regenerate, REGENERATE);

  // Missing, then written only by UPDATE_BUNDLE_SHAPE=1, then matched.
  let r = run(dir);
  assert.equal(r.code, 1);
  assert.match(r.out, /bundle-shape\.json is missing/);
  assert.ok(r.out.includes(REGENERATE));
  r = run(dir, { UPDATE_BUNDLE_SHAPE: 'true' });
  assert.equal(r.code, 1, 'only the value 1 writes');
  assert.equal(fs.existsSync(path.join(dir, SHAPE_FILE)), false);
  r = run(dir, { UPDATE_BUNDLE_SHAPE: '1' });
  assert.equal(r.code, 0, r.out);
  assert.equal(fs.readFileSync(path.join(dir, SHAPE_FILE), 'utf8'), render(shape));
  r = run(dir);
  assert.equal(r.code, 0, r.out);
  assert.match(r.out, /2 of 4 dependencies in the entry/);
  assert.match(r.out, /2 of 2 lazy pages are dynamic entries/);
});

test('an eager import of a lazy page fails, naming the page and the heavy package it drags in', async (t) => {
  const dir = makeApp();
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  await viteBuild(dir);
  assert.equal(run(dir, { UPDATE_BUNDLE_SHAPE: '1' }).code, 0);
  // The page stays behind import() where it was, and is also imported eagerly elsewhere.
  edit(dir, 'src/main.ts', "import { inner } from 'shim';", "import { inner } from 'shim';\nimport { Page } from './pages/Page';");
  edit(dir, 'src/main.ts', 'String(page)', 'String(page) + Page()');
  await viteBuild(dir);
  const r = run(dir);
  assert.equal(r.code, 1, r.out);
  assert.ok(r.out.includes(`~ src/pages/Page.ts: was ${DYNAMIC_ENTRY}, now ${EAGER}`), r.out);
  assert.ok(r.out.includes('~ heavy: was lazy, now entry (it now loads on every page)'), r.out);
  assert.ok(r.out.includes(REGENERATE), r.out);
  assert.ok(!r.out.includes('src/pages/Other.ts'), 'an unchanged page is not reported');
});

test('a page dropped from import() and a new one both show', async (t) => {
  const dir = makeApp();
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  await viteBuild(dir);
  const before = bundleShape(dir);
  edit(dir, 'src/main.ts', 'import(`./pages/Other`).then((m) => m.Other)', "import('./pages/Ghost').then((m) => m.Ghost)");
  await viteBuild(dir);
  assert.deepEqual(diffShapes(before, bundleShape(dir)), [
    `+ src/pages/Ghost.ts: new (${DYNAMIC_ENTRY})`,
    `- src/pages/Other.ts: gone (was ${DYNAMIC_ENTRY})`,
  ]);
  // A page whose exports nothing reads is shaken to an empty chunk, written with no map.
  edit(dir, 'src/main.ts', '(m) => m.Ghost', '(m) => m.Nothing');
  await viteBuild(dir);
  assert.equal(bundleShape(dir).lazyPages['src/pages/Ghost.ts'], 'not in the build');
});

test('refuses a build older than the sources, a missing build and a build without source maps', async (t) => {
  const dir = makeApp();
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  let r = run(dir);
  assert.equal(r.code, 2);
  assert.match(r.out, /holds no build: run npm run build first/);
  await viteBuild(dir);
  edit(dir, 'src/pages/Other.ts', '2', '3');
  r = run(dir);
  assert.equal(r.code, 2);
  assert.match(r.out, /the build is older than the sources \(src\/pages\/Other\.ts\)/);
  edit(dir, 'src/pages/Other.ts', '3', '2');
  for (const f of fs.readdirSync(path.join(dir, 'build/assets'))) {
    if (f.endsWith('.map')) fs.rmSync(path.join(dir, 'build/assets', f));
  }
  r = run(dir);
  assert.equal(r.code, 2);
  assert.match(r.out, /has no source map/);
  assert.equal(run(dir, {}, ['--bogus']).code, 2);
});

test('a shape file that differs only in its text asks for a regenerate', async (t) => {
  const dir = makeApp();
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  await viteBuild(dir);
  assert.equal(run(dir, { UPDATE_BUNDLE_SHAPE: '1' }).code, 0);
  const file = path.join(dir, SHAPE_FILE);
  fs.writeFileSync(file, JSON.stringify(JSON.parse(fs.readFileSync(file, 'utf8'))));
  const r = run(dir);
  assert.equal(r.code, 1);
  assert.match(r.out, /differs in its text/);
});

// Vite's bundler splits modules the entry shares with lazy chunks into
// chunks of their own (our jsx-runtime, store, useViewport and Modal), which
// the entry imports statically; the small app above does not get one, so a
// build written by hand checks that what those chunks hold counts as the
// entry's, and that a chunk only import() loads does not.
test('a chunk the entry imports statically is the entry; one only import() loads is lazy', (t) => {
  const dir = makeApp({
    'package.json': JSON.stringify({ name: 'app', dependencies: { light: '1', heavy: '1', late: '1' } }),
    'src/pages/Page.ts': 'export const Page = 1;\n',
    'src/main.ts': "const page = () => import('./pages/Page');\n",
    'build/index.html': '<script crossorigin src="/assets/index-AAAAAAAA.js" type="module"></script>\n',
    'build/assets/index-AAAAAAAA.js': 'import{a as e}from"./shared-BBBBBBBB.js";const p=()=>import(`./Page-CCCCCCCC.js`);\n',
    'build/assets/shared-BBBBBBBB.js': 'import"./deep-DDDDDDDD.js";export const a=1;\n',
    'build/assets/deep-DDDDDDDD.js': 'export const d=1;\n',
    'build/assets/Page-CCCCCCCC.js': 'import{a}from"./shared-BBBBBBBB.js";import"./late-EEEEEEEE.js";export const Page=1;\n',
    'build/assets/late-EEEEEEEE.js': 'export const l=1;\n',
    'build/assets/index-AAAAAAAA.js.map': JSON.stringify({ sources: ['../../src/main.ts'] }),
    // light is two static imports away from the entry.
    'build/assets/shared-BBBBBBBB.js.map': JSON.stringify({ sources: ['../../node_modules/@scope/kit/index.js'] }),
    'build/assets/deep-DDDDDDDD.js.map': JSON.stringify({ sources: ['../../node_modules/light/index.js'] }),
    'build/assets/Page-CCCCCCCC.js.map': JSON.stringify({
      sources: ['../../node_modules/heavy/index.js', '../../src/pages/Page.ts'],
    }),
    'build/assets/late-EEEEEEEE.js.map': JSON.stringify({ sources: ['../../node_modules/late/index.js'] }),
  });
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const shape = bundleShape(dir);
  assert.deepEqual(shape.dependencies, { heavy: 'lazy', late: 'lazy', light: 'entry' });
  assert.deepEqual(shape.lazyPages, { 'src/pages/Page.ts': DYNAMIC_ENTRY });
});

test('chunk imports: static, re-exported and side-effect imports, and import() in any quoting', () => {
  const code = [
    'import{a as b}from"./one-AAAAAAAA.js";',
    'import"./two-BBBBBBBB.js";',
    "export{c}from'./three-CCCCCCCC.js';",
    'import x from"react";',
    'const p=()=>import(`./Four-DDDDDDDD.js`),q=()=>import("./five-EEEEEEEE.js"),r=()=>import(y);',
    'const s="import(\\"./not-a-chunk.js\\")";',
  ].join('');
  assert.deepEqual(chunkImports(code), {
    statics: ['one-AAAAAAAA.js', 'two-BBBBBBBB.js', 'three-CCCCCCCC.js'],
    dynamic: ['Four-DDDDDDDD.js', 'five-EEEEEEEE.js'],
  });
});

test('package names from source map paths', () => {
  assert.equal(packageOf('../../node_modules/react-dom/cjs/react-dom.production.min.js'), 'react-dom');
  assert.equal(packageOf('../../node_modules/@ungap/structured-clone/esm/index.js'), '@ungap/structured-clone');
  assert.equal(packageOf('../../node_modules/a/node_modules/b/index.js'), 'b');
  assert.equal(packageOf('../../../../work/frontend/node_modules/cytoscape/dist/x.mjs'), 'cytoscape');
  assert.equal(packageOf('../../src/views/ModuleView.tsx'), null);
});

// The committed shape against the app's sources, without a build (CI's
// frontend job runs the build and the full check after these tests).
test("the committed bundle-shape.json names every import() target and every dependency of the app", () => {
  const shape = JSON.parse(fs.readFileSync(path.join(FRONTEND_DIR, SHAPE_FILE), 'utf8'));
  assert.deepEqual(Object.keys(shape.lazyPages), lazyModules(FRONTEND_DIR), `regenerate: ${REGENERATE}`);
  const manifest = JSON.parse(fs.readFileSync(path.join(FRONTEND_DIR, 'package.json'), 'utf8'));
  assert.deepEqual(Object.keys(shape.dependencies), Object.keys(manifest.dependencies).sort(), `regenerate: ${REGENERATE}`);
  assert.equal(shape.regenerate, REGENERATE);
});
