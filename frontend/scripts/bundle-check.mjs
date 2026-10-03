#!/usr/bin/env node
// bundle-check: the shape of the production bundle (refactor plan S12b,
// invariant I20), read from what `npm run build` wrote and compared with
// scripts/testdata/bundle-shape.json. It pins two things:
//
//   - dependencies: where each of the app's dependencies (package.json)
//     lands: "entry" when the entry chunk, or a chunk it imports statically,
//     holds its code, so it loads on every page; "lazy" when only chunks
//     loaded through import() do; "not in the build". A heavy package moving
//     into the entry (cytoscape, ag-grid) is what code splitting is there to
//     prevent. A package with no code of its own that re-exports another
//     (react-router-dom, whose code is react-router's) counts by the code of
//     the packages it depends on.
//   - lazyPages: every module the app loads through import() (the lazy pages
//     of App.tsx's lazy() calls), and whether it is still a dynamic entry:
//     its own chunk, loaded by import(). An eager import of a lazy view
//     anywhere pulls it into the entry and shows here.
//
//   cd frontend && npm run build && node scripts/bundle-check.mjs
//   UPDATE_BUNDLE_SHAPE=1 node scripts/bundle-check.mjs     # writes the file
//
// Options: --build <dir> (default build/), --shape <file> (default
// scripts/testdata/bundle-shape.json), --root <frontend dir> (default the
// directory above this script). Only UPDATE_BUNDLE_SHAPE=1 writes; any other
// value compares. The file is a golden (refactor_guard.py's list): change it
// only in a pull request that means to change the bundle, with a release
// note.
//
// What it reads: build/index.html for the entry script, each chunk's static
// imports and import() calls (parsed with the TypeScript parser), each
// chunk's source map for the modules and packages it holds (vite.config.ts
// keeps build.sourcemap on), package.json and, for a package with no code of
// its own, the installed node_modules/<name>/package.json files. It refuses a
// build older than the sources: the source maps carry each module's text,
// which must match src/.
//
// Exit status: 0 the shape matches (or was written), 1 it differs or the
// file is missing, 2 the build could not be read.

import ts from 'typescript';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

export const FRONTEND_DIR = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
export const SHAPE_FILE = 'scripts/testdata/bundle-shape.json';
export const REGENERATE = 'cd frontend && npm run build && UPDATE_BUNDLE_SHAPE=1 node scripts/bundle-check.mjs';
export const DYNAMIC_ENTRY = 'dynamic entry';

const posix = (p) => p.split(path.sep).join('/');

export class BuildError extends Error {}

// ------------------------------------------------------------------ chunks

/** The relative chunk files a chunk imports statically and through import(). */
export function chunkImports(code, fileName = 'chunk.js') {
  const sf = ts.createSourceFile(fileName, code, ts.ScriptTarget.Latest, false, ts.ScriptKind.JS);
  const statics = [];
  const dynamic = [];
  const text = (node) =>
    node && (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) ? node.text : null;
  for (const stmt of sf.statements) {
    if ((ts.isImportDeclaration(stmt) || ts.isExportDeclaration(stmt)) && stmt.moduleSpecifier) {
      const spec = text(stmt.moduleSpecifier);
      if (spec?.startsWith('./')) statics.push(spec.slice(2));
    }
  }
  const visit = (node) => {
    if (ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword) {
      const spec = text(node.arguments[0]);
      if (spec?.startsWith('./')) dynamic.push(spec.slice(2));
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return { statics, dynamic };
}

/** The npm package a source path belongs to, or null for one of ours. */
export function packageOf(source) {
  const at = source.lastIndexOf('node_modules/');
  if (at < 0) return null;
  const parts = source.slice(at + 'node_modules/'.length).split('/');
  return parts[0].startsWith('@') ? `${parts[0]}/${parts[1]}` : parts[0];
}

/** Every chunk of the build: its imports and the modules its source map lists. */
export function readBuild(root, buildDir) {
  const assets = path.join(buildDir, 'assets');
  const html = path.join(buildDir, 'index.html');
  if (!fs.existsSync(html) || !fs.existsSync(assets)) {
    throw new BuildError(`${posix(path.relative(root, buildDir)) || buildDir} holds no build: run npm run build first`);
  }
  const entries = [];
  // HTML tag and attribute names are case-insensitive, and a value may be
  // single-quoted or bare: read them as a browser would.
  for (const [, attrs] of fs.readFileSync(html, 'utf8').matchAll(/<script\b([^>]*)>/gi)) {
    const src = /\bsrc\s*=\s*(?:"([^"]+)"|'([^']+)'|([^\s"'>]+))/i.exec(attrs);
    const isModule = /\btype\s*=\s*(?:"module"|'module'|module(?![^\s>]))/i.test(attrs);
    if (isModule && src) entries.push(path.basename(src[1] ?? src[2] ?? src[3]));
  }
  if (!entries.length) throw new BuildError('build/index.html loads no module script');
  const chunks = new Map();
  const stale = [];
  for (const file of fs.readdirSync(assets).filter((f) => f.endsWith('.js')).sort()) {
    const code = fs.readFileSync(path.join(assets, file), 'utf8');
    const { statics, dynamic } = chunkImports(code, file);
    const mapFile = path.join(assets, `${file}.map`);
    if (!fs.existsSync(mapFile)) {
      // A chunk whose module was tree-shaken to nothing is written empty, with no map.
      if (!code.trim()) {
        chunks.set(file, { file, statics, dynamic, modules: [], packages: [] });
        continue;
      }
      throw new BuildError(`${file} has no source map: vite.config.ts must keep build.sourcemap on`);
    }
    const map = JSON.parse(fs.readFileSync(mapFile, 'utf8'));
    const modules = [];
    const packages = new Set();
    (map.sources || []).forEach((source, i) => {
      const abs = path.resolve(assets, map.sourceRoot || '', source);
      const pkg = packageOf(posix(source));
      if (pkg) {
        packages.add(pkg);
        return;
      }
      const rel = posix(path.relative(root, abs));
      modules.push(rel);
      const content = map.sourcesContent?.[i];
      if (typeof content === 'string' && (!fs.existsSync(abs) || fs.readFileSync(abs, 'utf8') !== content)) {
        stale.push(rel);
      }
    });
    chunks.set(file, { file, statics, dynamic, modules, packages: [...packages].sort() });
  }
  if (!chunks.size) throw new BuildError('build/assets holds no .js chunk: run npm run build first');
  if (stale.length) {
    throw new BuildError(
      `the build is older than the sources (${stale.slice(0, 3).join(', ')}${stale.length > 3 ? ', …' : ''}): ` +
        'run npm run build first'
    );
  }
  for (const e of entries) if (!chunks.has(e)) throw new BuildError(`index.html loads ${e}, which build/assets lacks`);
  return { entries, chunks };
}

// --------------------------------------------------------------- the app

const SOURCE = /\.(ts|tsx)$/;
const TEST = /\.(test|spec)\.tsx?$/;

// What never ships, as the Refactor guard's ships_in_frontend() has it: S12's
// guards and their helpers (src/arch), F2's mock helper (src/test), test data
// and snapshots, besides the tests and declaration files the walk skips.
const TEST_ONLY_DIRS = new Set(['testdata', '__snapshots__']);
const testOnlyDir = (src, abs) =>
  abs === path.join(src, 'arch') || abs === path.join(src, 'test') || TEST_ONLY_DIRS.has(path.basename(abs));

/** The modules the app's source loads through import('<relative path>'), frontend-relative and sorted. */
export function lazyModules(root) {
  const src = path.join(root, 'src');
  const found = new Set();
  const walk = (dir) => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const abs = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        if (!testOnlyDir(src, abs)) walk(abs);
      } else if (SOURCE.test(entry.name) && !TEST.test(entry.name) && !entry.name.endsWith('.d.ts')) {
        const code = fs.readFileSync(abs, 'utf8');
        if (!code.includes('import(')) continue;
        const kind = abs.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS;
        const sf = ts.createSourceFile(abs, code, ts.ScriptTarget.Latest, false, kind);
        const visit = (node) => {
          if (ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword) {
            const arg = node.arguments[0];
            const spec = arg && (ts.isStringLiteral(arg) || ts.isNoSubstitutionTemplateLiteral(arg)) ? arg.text : null;
            if (spec?.startsWith('.')) found.add(posix(path.relative(root, resolveModule(abs, spec))));
          }
          ts.forEachChild(node, visit);
        };
        visit(sf);
      }
    }
  };
  walk(src);
  return [...found].sort();
}

function resolveModule(from, spec) {
  const base = path.resolve(path.dirname(from), spec);
  for (const c of [base, `${base}.ts`, `${base}.tsx`, path.join(base, 'index.ts'), path.join(base, 'index.tsx')]) {
    if (fs.existsSync(c) && fs.statSync(c).isFile()) return c;
  }
  throw new BuildError(`${posix(path.relative(FRONTEND_DIR, from))}: cannot resolve import('${spec}')`);
}

// --------------------------------------------------------------- the shape

/** Where a dependency's code lands: "entry", "lazy" or "not in the build". */
function placeOf(root, name, eagerPkgs, allPkgs, direct) {
  const own = (n) => (eagerPkgs.has(n) ? 'entry' : allPkgs.has(n) ? 'lazy' : null);
  const place = own(name);
  if (place) return place;
  // No code of its own: what it pulls in, through the packages it depends on
  // that are not dependencies of the app in their own right.
  const seen = new Set([name]);
  const queue = [name];
  let found = null;
  while (queue.length) {
    const manifest = path.join(root, 'node_modules', queue.shift(), 'package.json');
    if (!fs.existsSync(manifest)) continue;
    for (const dep of Object.keys(JSON.parse(fs.readFileSync(manifest, 'utf8')).dependencies || {})) {
      if (seen.has(dep) || direct.has(dep)) continue;
      seen.add(dep);
      const p = own(dep);
      if (p === 'entry') return 'entry';
      found ??= p;
      queue.push(dep);
    }
  }
  return found || 'not in the build';
}

/** The bundle's shape: where each dependency lands, and each lazy module's place. */
export function bundleShape(root = FRONTEND_DIR, buildDir = path.join(root, 'build')) {
  const { entries, chunks } = readBuild(root, buildDir);
  const eager = new Set();
  const visit = (file) => {
    if (eager.has(file)) return;
    const chunk = chunks.get(file);
    if (!chunk) throw new BuildError(`a chunk imports ${file}, which build/assets lacks`);
    eager.add(file);
    chunk.statics.forEach(visit);
  };
  entries.forEach(visit);
  const dynamicTargets = new Set([...chunks.values()].flatMap((c) => c.dynamic));
  const eagerPkgs = new Set([...eager].flatMap((f) => chunks.get(f).packages));
  const allPkgs = new Set([...chunks.values()].flatMap((c) => c.packages));
  const manifest = JSON.parse(fs.readFileSync(path.join(root, 'package.json'), 'utf8'));
  const direct = new Set(Object.keys(manifest.dependencies || {}));
  const dependencies = {};
  for (const name of [...direct].sort()) dependencies[name] = placeOf(root, name, eagerPkgs, allPkgs, direct);
  const lazyPages = {};
  for (const mod of lazyModules(root)) {
    const holders = [...chunks.values()].filter((c) => c.modules.includes(mod)).map((c) => c.file);
    if (holders.some((f) => eager.has(f))) lazyPages[mod] = 'in the entry: loaded eagerly';
    else if (holders.some((f) => dynamicTargets.has(f))) lazyPages[mod] = DYNAMIC_ENTRY;
    else if (holders.length) lazyPages[mod] = 'in a chunk no import() loads';
    else lazyPages[mod] = 'not in the build';
  }
  return {
    about:
      "The production bundle's shape (refactor plan S12b, invariant I20), written and checked by " +
      'scripts/bundle-check.mjs after npm run build: where each dependency in package.json lands (entry: the ' +
      'entry chunk or a chunk it imports statically, loaded on every page; lazy: only chunks loaded through ' +
      'import()), and whether each module the app loads through import() is still a dynamic entry. A golden: ' +
      'change it only in a pull request that means to change the bundle.',
    regenerate: REGENERATE,
    dependencies,
    lazyPages,
  };
}

export const render = (shape) => `${JSON.stringify(shape, null, 2)}\n`;

/** Human-readable differences between the pinned shape and the build's. */
export function diffShapes(want, got) {
  const out = [];
  const pairs = (key, label) => {
    const before = want?.[key] || {};
    for (const [name, place] of Object.entries(got[key])) {
      if (!(name in before)) out.push(`+ ${name}: new (${place})`);
      else if (before[name] !== place) out.push(`~ ${name}: was ${before[name]}, now ${place}${label(place)}`);
    }
    for (const name of Object.keys(before)) if (!(name in got[key])) out.push(`- ${name}: gone (was ${before[name]})`);
  };
  pairs('dependencies', (place) => (place === 'entry' ? ' (it now loads on every page)' : ''));
  pairs('lazyPages', () => '');
  return out;
}

// --------------------------------------------------------------------- CLI

export function main(argv = process.argv.slice(2), env = process.env, log = console) {
  let root = FRONTEND_DIR;
  let buildDir = null;
  let shapeFile = null;
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    const value = () => {
      if (i + 1 >= argv.length) throw new BuildError(`${arg} needs a value`);
      return argv[++i];
    };
    if (arg === '--root') root = path.resolve(value());
    else if (arg === '--build') buildDir = path.resolve(value());
    else if (arg === '--shape') shapeFile = path.resolve(value());
    else {
      log.error(`bundle-check: unknown argument ${arg}\nusage: node scripts/bundle-check.mjs [--root dir] [--build dir] [--shape file]`);
      return 2;
    }
  }
  buildDir ??= path.join(root, 'build');
  shapeFile ??= path.join(root, SHAPE_FILE);
  let got;
  try {
    got = bundleShape(root, buildDir);
  } catch (e) {
    if (!(e instanceof BuildError)) throw e;
    log.error(`bundle-check: ${e.message}`);
    return 2;
  }
  const text = render(got);
  const where = posix(path.relative(root, shapeFile));
  const summary =
    `${Object.values(got.dependencies).filter((p) => p === 'entry').length} of ` +
    `${Object.keys(got.dependencies).length} dependencies in the entry; ` +
    `${Object.values(got.lazyPages).filter((p) => p === DYNAMIC_ENTRY).length} of ` +
    `${Object.keys(got.lazyPages).length} lazy pages are dynamic entries`;
  if (env.UPDATE_BUNDLE_SHAPE === '1') {
    fs.mkdirSync(path.dirname(shapeFile), { recursive: true });
    fs.writeFileSync(shapeFile, text);
    log.log(`bundle-check: wrote ${where} (${summary})`);
    return 0;
  }
  if (!fs.existsSync(shapeFile)) {
    log.error(`bundle-check: ${where} is missing. Write it with:\n  ${REGENERATE}`);
    return 1;
  }
  const pinned = fs.readFileSync(shapeFile, 'utf8');
  if (pinned === text) {
    log.log(`bundle-check: the bundle matches ${where} (${summary})`);
    return 0;
  }
  let want = null;
  try {
    want = JSON.parse(pinned);
  } catch {
    // reported as a whole-file difference below
  }
  const diff = diffShapes(want, got);
  log.error(
    [
      `bundle-check: the bundle's shape differs from ${where} (invariant I20):`,
      ...(diff.length ? diff.map((d) => `  ${d}`) : ['  (the file differs in its text: regenerate it)']),
      'A refactor leaves the bundle as it is: keep a lazy page behind import() and out of every eager import,',
      'and keep heavy packages out of the entry. If the change is deliberate (a feature with a release note),',
      `regenerate:\n  ${REGENERATE}`,
    ].join('\n')
  );
  return 1;
}

if (import.meta.url === pathToFileURL(process.argv[1] || '').href) {
  process.exitCode = main();
}
