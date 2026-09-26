#!/usr/bin/env node
// tsdeclhash: hash every top-level declaration of the frontend's TypeScript
// modules, so a pure move (refactor class A) can be proved: moving
// declarations between modules and fixing imports leaves every hash alone,
// and any other edit changes one.
//
//   node frontend/scripts/tsdeclhash.mjs [--root dir] [--no-module] [-o manifest] [path...]
//   node frontend/scripts/tsdeclhash.mjs --compare base.txt head.txt
//   node frontend/scripts/tsdeclhash.mjs --base <git-ref> [--head <git-ref>] [--no-module] [path...]
//
// Paths are .ts/.tsx files or directories (default: <root>/src; root
// defaults to frontend/). The manifest is sorted "module<TAB>name<TAB>sha256"
// lines, modules named relative to the root; --no-module drops the module
// column, so declarations that moved between modules still compare equal.
//
// What a declaration is: each top-level function, class, interface, type,
// enum, namespace, and each name a variable statement binds (a statement
// that declares nothing is "(statement)"; `export default <expression>` is
// "default"). Import declarations, re-exports, local `export { ... }` lists
// and `export default <identifier>` are wiring, not declarations: they are
// excluded, like Go's import blocks. A declaration's hash covers:
//   - its text, with the attached comments above it (no blank line between)
//     and a comment on its last line;
//   - what each module-level name it uses is bound to: a declaration by its
//     own text hash, wherever it lives now, or a package export. Rebinding a
//     name to another declaration with the same name therefore shows, and so
//     does every user of a declaration whose text changed;
//   - each relative module path it passes to a call (import('./x'),
//     vi.mock('../api/client')), by what each name that module exports
//     resolves to, so a module split behind a barrel that re-exports the
//     same names keeps it. A declaration that becomes importable from its
//     new module is exported with an `export { name }` list: adding the
//     `export` modifier changes its text.
//
// Evaluation order: a side-effect import (`import './x.css'`) is a
// declaration named "(import <module>)", not wiring, and each declaration
// records its position and whether it runs when the module is evaluated
// and may have side effects; tsmovecheck checks the order (see there).
//
// --compare and --base print every added, removed or changed declaration
// and exit 1 on any difference; exit 2 is a usage or read error.

import ts from 'typescript';
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

export const FRONTEND_DIR = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const SOURCE = /\.(ts|tsx|mts|cts)$/;

const sha = (s) => createHash('sha256').update(s).digest('hex');
const posix = (p) => p.split(path.sep).join('/');

// ---------------------------------------------------------------- reading

/** Reads the .ts/.tsx files under the given absolute paths, keyed by their
 * path relative to root. */
export function readWorkingTree(root, absPaths) {
  const files = new Map();
  const walk = (abs) => {
    const st = fs.statSync(abs, { throwIfNoEntry: false });
    if (!st) return;
    if (st.isDirectory()) {
      if (path.basename(abs) === 'node_modules') return;
      for (const e of fs.readdirSync(abs).sort()) walk(path.join(abs, e));
    } else if (SOURCE.test(abs)) {
      files.set(posix(path.relative(root, abs)), fs.readFileSync(abs, 'utf8'));
    }
  };
  for (const p of absPaths) walk(p);
  return files;
}

function git(cwd, args, input) {
  return execFileSync('git', args, { cwd, input, maxBuffer: 1 << 30, stdio: ['pipe', 'pipe', 'pipe'] });
}

/** Resolves symbolic links in the longest existing prefix of p. */
function realPath(p) {
  try {
    return fs.realpathSync(p);
  } catch {
    const parent = path.dirname(p);
    return parent === p ? p : path.join(realPath(parent), path.basename(p));
  }
}

/** Reads the same files as readWorkingTree, as they are at a git ref. */
export function readGitTree(root, ref, absPaths) {
  const top = realPath(git(root, ['rev-parse', '--show-toplevel']).toString().trim());
  const rootRel = posix(path.relative(top, realPath(root)));
  const specs = absPaths.map((p) => posix(path.relative(top, realPath(p))) || '.');
  const listing = git(top, ['ls-tree', '-r', '-z', '--full-name', ref, '--', ...specs]).toString('utf8');
  const names = [];
  const oids = [];
  for (const entry of listing.split('\0')) {
    const tab = entry.indexOf('\t');
    if (tab < 0) continue;
    const [, type, oid] = entry.slice(0, tab).split(' ');
    const file = entry.slice(tab + 1);
    if (type !== 'blob' || !SOURCE.test(file) || file.split('/').includes('node_modules')) continue;
    names.push(path.posix.relative(rootRel, file));
    oids.push(oid);
  }
  const files = new Map();
  if (oids.length === 0) return files;
  let buf = git(top, ['cat-file', '--batch'], oids.join('\n') + '\n');
  for (const name of names) {
    const nl = buf.indexOf(10);
    const size = Number(buf.subarray(0, nl).toString().split(' ')[2]);
    files.set(name, buf.subarray(nl + 1, nl + 1 + size).toString('utf8'));
    buf = buf.subarray(nl + 1 + size + 1);
  }
  return new Map([...files].sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)));
}

// --------------------------------------------------------------- analysis

const hasModifier = (node, kind) => (ts.canHaveModifiers(node) ? ts.getModifiers(node) ?? [] : []).some((m) => m.kind === kind);

/** The names a binding pattern or identifier binds. */
function bindingNames(name, out = []) {
  if (ts.isIdentifier(name)) out.push(name.text);
  else for (const el of name.elements) if (!ts.isOmittedExpression(el)) bindingNames(el.name, out);
  return out;
}

/** Whether an identifier is a reference to a binding, rather than a
 * property name, label or the name a declaration introduces. */
function isReference(id) {
  const p = id.parent;
  if (!p) return true;
  if ((ts.isPropertyAccessExpression(p) || ts.isQualifiedName(p)) && (p.name ?? p.right) === id) return false;
  if (ts.isBindingElement(p) && p.propertyName === id) return false;
  if (ts.isJsxAttribute(p) || ts.isLabeledStatement(p) || ts.isBreakOrContinueStatement(p)) return false;
  if (ts.isImportSpecifier(p) || ts.isExportSpecifier(p) || ts.isNamespaceImport(p) || ts.isImportClause(p)) return false;
  if (ts.isShorthandPropertyAssignment(p)) return true;
  if ('name' in p && p.name === id) {
    return !(
      ts.isDeclaration(p) ||
      ts.isPropertyAssignment(p) ||
      ts.isPropertySignature(p) ||
      ts.isMethodSignature(p) ||
      ts.isEnumMember(p)
    );
  }
  return true;
}

function isRelative(spec) {
  return spec === '.' || spec === '..' || spec.startsWith('./') || spec.startsWith('../');
}

/** Returns the declaration's text with attached comments, the identifiers
 * it references, and the relative module paths its calls pass, each of
 * which the text shows as a numbered placeholder. */
function statementText(sf, st) {
  const full = sf.text;
  const ranges = ts.getLeadingCommentRanges(full, st.getFullStart()) ?? [];
  const attached = [];
  let next = st.getStart(sf);
  for (let i = ranges.length - 1; i >= 0; i--) {
    if ((full.slice(ranges[i].end, next).match(/\n/g) ?? []).length > 1) break;
    attached.unshift(full.slice(ranges[i].pos, ranges[i].end));
    next = ranges[i].pos;
  }
  const trailing = (ts.getTrailingCommentRanges(full, st.end) ?? [])
    .filter((r) => !full.slice(st.end, r.pos).includes('\n'))
    .map((r) => full.slice(r.pos, r.end));
  const refs = new Set();
  const literals = [];
  const visit = (n) => {
    if (ts.isIdentifier(n) && isReference(n)) refs.add(n.text);
    if (ts.isCallExpression(n) && n.arguments.length > 0) {
      const arg = n.arguments[0];
      if ((ts.isStringLiteral(arg) || ts.isNoSubstitutionTemplateLiteral(arg)) && isRelative(arg.text)) {
        literals.push({ start: arg.getStart(sf), end: arg.end, spec: arg.text });
      }
    }
    ts.forEachChild(n, visit);
  };
  visit(st);
  literals.sort((a, b) => a.start - b.start);
  let body = '';
  let at = st.getStart(sf);
  literals.forEach((l, i) => {
    body += full.slice(at, l.start) + `<module ref ${i}>`;
    at = l.end;
  });
  body += full.slice(at, st.end);
  const text = [...attached, body, ...trailing].join('\n');
  return { text, refs, moduleRefs: literals.map((l) => l.spec) };
}

/** The names a top-level statement declares, each with the names it is
 * exported under; null for wiring (imports and exports). */
function declared(st) {
  const exported = hasModifier(st, ts.SyntaxKind.ExportKeyword);
  const isDefault = hasModifier(st, ts.SyntaxKind.DefaultKeyword);
  const one = (name) => [{ name, exportedAs: exported ? [isDefault ? 'default' : name] : [] }];
  if (ts.isFunctionDeclaration(st) || ts.isClassDeclaration(st)) return one(st.name ? st.name.text : 'default');
  if (ts.isInterfaceDeclaration(st) || ts.isTypeAliasDeclaration(st) || ts.isEnumDeclaration(st)) return one(st.name.text);
  if (ts.isModuleDeclaration(st)) return one(ts.isIdentifier(st.name) ? st.name.text : `module ${st.name.text}`);
  if (ts.isVariableStatement(st)) {
    return st.declarationList.declarations.flatMap((d) =>
      bindingNames(d.name).map((name) => ({ name, exportedAs: exported ? [name] : [] })),
    );
  }
  if (ts.isExportAssignment(st)) return [{ name: st.isExportEquals ? '=' : 'default', exportedAs: [st.isExportEquals ? '=' : 'default'] }];
  return [{ name: '(statement)', exportedAs: [] }];
}

// Statements that do nothing when the module is evaluated: hoisted
// functions and erased types.
const HOISTED_OR_ERASED = new Set([ts.SyntaxKind.FunctionDeclaration, ts.SyntaxKind.InterfaceDeclaration, ts.SyntaxKind.TypeAliasDeclaration]);

// Stylesheets: their order is S12b's CSS-bytes pin, not a script effect.
const STYLE = /\.(css|scss|sass|less)$/;

/** Parses one module into its imports, exports and declarations. */
export function analyzeModule(modPath, source) {
  const text = source.replace(/\r\n/g, '\n');
  const kind = modPath.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS;
  const sf = ts.createSourceFile(modPath, text, ts.ScriptTarget.Latest, true, kind);
  const mod = { path: modPath, text, imports: new Map(), reexports: [], localExports: new Map(), wiring: [], decls: [] };
  const nodes = new Map(); // decl -> statement, for the effect pass below
  for (const [pos, st] of sf.statements.entries()) {
    if (ts.isImportDeclaration(st) && !st.importClause) {
      // A side-effect import runs its module at this point: a statement to
      // keep, not wiring to rewrite.
      const spec = st.moduleSpecifier.text;
      const name = `(import ${isRelative(spec) ? path.posix.normalize(path.posix.join(path.posix.dirname(modPath), spec)) : spec})`;
      const own = sha(`ImportDeclaration ${name}`);
      mod.decls.push({ name, exportedAs: [], own, refs: new Set(), selfNames: new Set([name]), moduleRefs: [], hash: '', bindings: [], pos, runtime: true, effectful: !STYLE.test(spec) });
      continue;
    }
    if (ts.isImportDeclaration(st) || ts.isImportEqualsDeclaration(st)) {
      mod.wiring.push(st.getText(sf));
      addImport(mod, st);
      continue;
    }
    if (ts.isExportDeclaration(st)) {
      mod.wiring.push(st.getText(sf));
      const spec = st.moduleSpecifier?.text;
      if (!st.exportClause) mod.reexports.push({ spec, star: true });
      else if (ts.isNamespaceExport(st.exportClause)) mod.reexports.push({ spec, exported: st.exportClause.name.text, imported: '*' });
      else {
        for (const el of st.exportClause.elements) {
          const local = (el.propertyName ?? el.name).text;
          if (spec !== undefined) mod.reexports.push({ spec, exported: el.name.text, imported: local });
          else mod.localExports.set(el.name.text, local);
        }
      }
      continue;
    }
    if (ts.isExportAssignment(st) && ts.isIdentifier(st.expression)) {
      mod.wiring.push(st.getText(sf));
      mod.localExports.set(st.isExportEquals ? '=' : 'default', st.expression.text);
      continue;
    }
    const names = declared(st);
    const { text: body, refs, moduleRefs } = statementText(sf, st);
    const kindName = ts.SyntaxKind[st.kind];
    const selfNames = new Set(names.map((n) => n.name));
    const runtime = !HOISTED_OR_ERASED.has(st.kind) && !hasModifier(st, ts.SyntaxKind.DeclareKeyword);
    for (const { name, exportedAs } of names) {
      const own = sha(`${kindName} ${name}\n${body}`);
      const d = { name, exportedAs, own, refs, selfNames, moduleRefs, hash: '', bindings: [], pos, runtime, effectful: false };
      if (runtime) nodes.set(d, st);
      mod.decls.push(d);
    }
  }
  const shadowed = new Set([...mod.imports.keys(), ...mod.decls.map((d) => d.name)]);
  for (const [d, st] of nodes) d.effectful = mayHaveEffects(st, mod, shadowed);
  return mod;
}

// Calls known to have no side effect: globals (unless a module-level name
// shadows them) and React's component and context constructors.
const PURE_GLOBAL_CALLS = new Set([
  'Symbol', 'String', 'Number', 'Boolean', 'BigInt',
  'Object.freeze', 'Object.keys', 'Object.values', 'Object.entries', 'Object.fromEntries',
  'Array.from', 'Array.isArray', 'Array.of',
]);
const PURE_GLOBAL_NEWS = new Set(['Set', 'Map', 'WeakMap', 'WeakSet', 'RegExp', 'Error', 'URLSearchParams']);
const PURE_REACT = new Set(['lazy', 'createContext', 'memo', 'forwardRef']);

/** Whether evaluating a top-level statement may have a side effect, so the
 * point where it runs matters. Function bodies, non-static class members
 * and types run later or never; any call not known to be pure, `new`,
 * tagged template, await, assignment, increment, delete or decorator
 * counts. Property reads (getters) do not. */
function mayHaveEffects(st, mod, shadowed) {
  const pureCall = (n) => {
    const e = n.expression;
    if (ts.isIdentifier(e)) {
      const im = mod.imports.get(e.text);
      if (im) return im.spec === 'react' && PURE_REACT.has(im.imported);
      return !shadowed.has(e.text) && (ts.isNewExpression(n) ? PURE_GLOBAL_NEWS : PURE_GLOBAL_CALLS).has(e.text);
    }
    if (ts.isPropertyAccessExpression(e) && ts.isIdentifier(e.expression)) {
      const head = e.expression.text;
      const im = mod.imports.get(head);
      if (im) return im.spec === 'react' && (im.imported === '*' || im.imported === 'default') && PURE_REACT.has(e.name.text);
      return !shadowed.has(head) && !ts.isNewExpression(n) && PURE_GLOBAL_CALLS.has(`${head}.${e.name.text}`);
    }
    return false;
  };
  let found = false;
  const visit = (n) => {
    if (found || (ts.isTypeNode(n) && !ts.isExpressionWithTypeArguments(n))) return;
    if (ts.isFunctionLike(n) && !ts.isClassStaticBlockDeclaration(n)) return; // runs when called
    if (ts.isPropertyDeclaration(n) && !hasModifier(n, ts.SyntaxKind.StaticKeyword)) return; // runs per instance
    if (ts.isCallExpression(n) || ts.isNewExpression(n)) {
      if (!pureCall(n)) found = true; // import() included
    } else if (ts.isTaggedTemplateExpression(n) || ts.isAwaitExpression(n) || ts.isDeleteExpression(n) || ts.isDecorator(n)) {
      found = true;
    } else if (ts.isBinaryExpression(n) && n.operatorToken.kind >= ts.SyntaxKind.FirstAssignment && n.operatorToken.kind <= ts.SyntaxKind.LastAssignment) {
      found = true;
    } else if ((ts.isPrefixUnaryExpression(n) || ts.isPostfixUnaryExpression(n)) && (n.operator === ts.SyntaxKind.PlusPlusToken || n.operator === ts.SyntaxKind.MinusMinusToken)) {
      found = true;
    }
    if (!found) ts.forEachChild(n, visit);
  };
  if (ts.isVariableStatement(st)) {
    // The declared names are not assignments.
    for (const d of st.declarationList.declarations) if (d.initializer) visit(d.initializer);
    for (const d of st.declarationList.declarations) if (!ts.isIdentifier(d.name)) visit(d.name);
  } else visit(st);
  return found;
}

function addImport(mod, st) {
  if (ts.isImportEqualsDeclaration(st)) {
    const ref = st.moduleReference;
    if (ts.isExternalModuleReference(ref) && ts.isStringLiteral(ref.expression)) {
      mod.imports.set(st.name.text, { spec: ref.expression.text, imported: '*' });
    }
    return;
  }
  const spec = st.moduleSpecifier.text;
  const clause = st.importClause;
  if (!clause) return;
  if (clause.name) mod.imports.set(clause.name.text, { spec, imported: 'default' });
  const nb = clause.namedBindings;
  if (nb && ts.isNamespaceImport(nb)) mod.imports.set(nb.name.text, { spec, imported: '*' });
  if (nb && ts.isNamedImports(nb)) {
    for (const el of nb.elements) mod.imports.set(el.name.text, { spec, imported: (el.propertyName ?? el.name).text });
  }
}

/** Finds the analysed module a relative specifier names. */
function resolveModule(modules, from, spec) {
  const base = path.posix.normalize(path.posix.join(path.posix.dirname(from), spec));
  for (const cand of [base, `${base}.ts`, `${base}.tsx`, `${base}.d.ts`, `${base}/index.ts`, `${base}/index.tsx`]) {
    if (modules.has(cand)) return modules.get(cand);
  }
  return null;
}

/** What a local top-level name is: its declarations' own text. */
function localIdentity(mod, name) {
  return 'decl:' + sha(mod.decls.filter((d) => d.name === name).map((d) => d.own).sort().join(','));
}

/** The names a module exports: its exported declarations, export lists
 * and re-exports, following `export *` to the modules it names. */
function exportedNames(ctx, mod, seen = new Set()) {
  if (seen.has(mod.path)) return new Set();
  seen.add(mod.path);
  const names = new Set();
  for (const d of mod.decls) for (const n of d.exportedAs) names.add(n);
  for (const n of mod.localExports.keys()) names.add(n);
  for (const re of mod.reexports) {
    if (!re.star) names.add(re.exported);
    else if (isRelative(re.spec)) {
      const target = resolveModule(ctx.modules, mod.path, re.spec);
      if (target) for (const n of exportedNames(ctx, target, seen)) if (n !== 'default') names.add(n);
    } else names.add(`* ${re.spec}`);
  }
  return names;
}

/** A module's identity: what each name it exports is bound to, so a module
 * whose declarations moved behind re-exports keeps it. */
function moduleIdentity(ctx, mod) {
  if (!ctx.moduleIds.has(mod.path)) {
    ctx.moduleIds.set(mod.path, `cycle:${mod.path}`);
    const parts = [...exportedNames(ctx, mod)]
      .sort()
      .map((n) => (n.startsWith('* ') ? n : `${n}=${exportIdentity(ctx, mod, n, new Set())}`));
    ctx.moduleIds.set(mod.path, sha(parts.join('\n')));
  }
  return ctx.moduleIds.get(mod.path);
}

function importIdentity(ctx, from, spec, imported, seen) {
  if (!isRelative(spec)) return `pkg:${spec}#${imported}`;
  const target = resolveModule(ctx.modules, from.path, spec);
  if (!target) return `file:${path.posix.normalize(path.posix.join(path.posix.dirname(from.path), spec))}#${imported}`;
  return exportIdentity(ctx, target, imported, seen);
}

/** Follows local exports and re-exports to the declaration a module
 * exports under name. */
function exportIdentity(ctx, mod, name, seen) {
  const key = `${mod.path}#${name}`;
  if (seen.has(key)) return `cycle:${name}`;
  seen.add(key);
  if (name === '*') return `module:${moduleIdentity(ctx, mod)}`;
  const decl = mod.decls.find((d) => d.exportedAs.includes(name));
  if (decl) return localIdentity(mod, decl.name);
  if (mod.localExports.has(name)) {
    const local = mod.localExports.get(name);
    if (mod.decls.some((d) => d.name === local)) return localIdentity(mod, local);
    const im = mod.imports.get(local);
    return im ? importIdentity(ctx, mod, im.spec, im.imported, seen) : `missing:${name}`;
  }
  const named = mod.reexports.find((re) => !re.star && re.exported === name);
  if (named) return importIdentity(ctx, mod, named.spec, named.imported, seen);
  if (name !== 'default') {
    for (const re of mod.reexports.filter((r) => r.star && isRelative(r.spec))) {
      const target = resolveModule(ctx.modules, mod.path, re.spec);
      const id = target ? exportIdentity(ctx, target, name, new Set(seen)) : null;
      if (id && !id.startsWith('missing:')) return id;
    }
  }
  return `missing:${name}`;
}

/** Analyses every module and computes each declaration's full hash: its
 * own text plus what the names and module paths it uses resolve to. */
export function analyzeTree(files) {
  const modules = new Map();
  for (const [p, text] of [...files].sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))) modules.set(p, analyzeModule(p, text));
  const ctx = { modules, moduleIds: new Map() };
  for (const mod of modules.values()) {
    const top = new Set(mod.decls.map((d) => d.name));
    for (const d of mod.decls) {
      const binds = [];
      for (const r of [...d.refs].sort()) {
        if (d.selfNames.has(r)) continue;
        if (top.has(r)) binds.push(`${r}=${localIdentity(mod, r)}`);
        else if (mod.imports.has(r)) {
          const im = mod.imports.get(r);
          binds.push(`${r}=${importIdentity(ctx, mod, im.spec, im.imported, new Set())}`);
        }
      }
      d.moduleRefs.forEach((spec, i) => {
        const target = resolveModule(modules, mod.path, spec);
        const id = target ? `module:${moduleIdentity(ctx, target)}` : `file:${path.posix.join(path.posix.dirname(mod.path), spec)}`;
        binds.push(`<module ref ${i}>=${id}`);
      });
      d.bindings = binds;
      d.hash = sha(`${d.own}\n${binds.join('\n')}`);
    }
  }
  return modules;
}

/** The modules a module's evaluation runs first, in order: its static
 * imports and re-exports as they survive compilation. Each module is
 * transpiled alone, as Vite's esbuild does, so imports used only as types
 * drop out. A relative specifier is named by the module it resolves to (or
 * its path), a package by its specifier. */
export function runtimeImports(modules, mod) {
  if (mod.runtimeImports) return mod.runtimeImports;
  const out = [];
  if (!mod.path.endsWith('.d.ts')) {
    const js = ts.transpileModule(mod.text, {
      fileName: mod.path,
      compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2020, jsx: ts.JsxEmit.ReactJSX, isolatedModules: true },
    }).outputText;
    for (const st of ts.createSourceFile(`${mod.path}.js`, js, ts.ScriptTarget.Latest, false, ts.ScriptKind.JS).statements) {
      if (!(ts.isImportDeclaration(st) || ts.isExportDeclaration(st)) || !st.moduleSpecifier) continue;
      const spec = st.moduleSpecifier.text;
      let key = spec;
      if (isRelative(spec)) {
        const target = resolveModule(modules, mod.path, spec);
        key = target ? target.path : path.posix.normalize(path.posix.join(path.posix.dirname(mod.path), spec));
      }
      if (!out.includes(key)) out.push(key);
    }
  }
  mod.runtimeImports = out;
  return out;
}

/** The import cycles that survive compilation (see runtimeImports), each
 * as its sorted module paths joined by spaces. In a cycle a module can read
 * another's binding before it is initialised. */
export function runtimeCycles(modules) {
  const graph = new Map();
  for (const mod of modules.values()) graph.set(mod.path, runtimeImports(modules, mod).filter((k) => modules.has(k)));
  // Tarjan's strongly connected components.
  let next = 0;
  const index = new Map();
  const low = new Map();
  const stack = [];
  const onStack = new Set();
  const cycles = [];
  const visit = (v) => {
    index.set(v, next);
    low.set(v, next++);
    stack.push(v);
    onStack.add(v);
    for (const w of graph.get(v)) {
      if (!index.has(w)) {
        visit(w);
        low.set(v, Math.min(low.get(v), low.get(w)));
      } else if (onStack.has(w)) low.set(v, Math.min(low.get(v), index.get(w)));
    }
    if (low.get(v) !== index.get(v)) return;
    const scc = [];
    let w;
    do {
      w = stack.pop();
      onStack.delete(w);
      scc.push(w);
    } while (w !== v);
    if (scc.length > 1 || graph.get(v).includes(v)) cycles.push(scc.sort().join(' '));
  };
  for (const v of graph.keys()) if (!index.has(v)) visit(v);
  return cycles.sort();
}

/** The sorted manifest lines of analysed modules. */
export function manifest(modules, { noModule = false } = {}) {
  const lines = [];
  for (const mod of modules.values()) {
    for (const d of mod.decls) lines.push(noModule ? `${d.name}\t${d.hash}` : `${mod.path}\t${d.name}\t${d.hash}`);
  }
  return lines.sort();
}

/** Compares two manifests: each key's hashes as a multiset. */
export function diffManifests(base, head) {
  const index = (lines) => {
    const m = new Map();
    for (const l of lines) {
      const i = l.lastIndexOf('\t');
      const k = l.slice(0, i);
      if (!m.has(k)) m.set(k, []);
      m.get(k).push(l.slice(i + 1));
    }
    for (const hs of m.values()) hs.sort();
    return m;
  };
  const b = index(base);
  const h = index(head);
  const keys = [...new Set([...b.keys(), ...h.keys()])].sort();
  const diffs = [];
  for (const k of keys) {
    const bh = b.get(k) ?? [];
    const hh = h.get(k) ?? [];
    if (bh.length === 0) diffs.push(`added\t${k}`);
    else if (hh.length === 0) diffs.push(`removed\t${k}`);
    else if (bh.join(',') !== hh.join(',')) diffs.push(`changed\t${k}`);
  }
  return { diffs, total: keys.length };
}

// -------------------------------------------------------------------- CLI

/** Parses the shared command-line options; positional arguments are
 * returned in rest. */
export function parseArgs(argv, flags) {
  const opts = { rest: [] };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (!a.startsWith('-') || a === '-') {
      opts.rest.push(a);
      continue;
    }
    const name = a.replace(/^--?/, '');
    if (!(name in flags)) throw new Error(`unknown option ${a}`);
    if (flags[name] === 'bool') opts[name] = true;
    else {
      if (i + 1 >= argv.length) throw new Error(`${a} needs a value`);
      opts[name] = argv[++i];
    }
  }
  return opts;
}

/** Reads and analyses the tree at ref, or the working tree without one. */
export function load(root, ref, absPaths) {
  return analyzeTree(ref ? readGitTree(root, ref, absPaths) : readWorkingTree(root, absPaths));
}

/** Throws for a path with no source file on any side: a mistyped path, a
 * path relative to frontend/ given from another directory, or an unexpanded
 * glob would otherwise compare nothing and pass. */
export function requireSources(root, absPaths, ...sides) {
  for (const p of absPaths) {
    const rel = posix(path.relative(root, p));
    const under = (k) => rel === '' || k === rel || k.startsWith(`${rel}/`);
    if (!sides.some((mods) => [...mods.keys()].some(under))) throw new Error(`${p}: no .ts/.tsx files on either side`);
  }
}

function main(argv) {
  const usage =
    'usage: tsdeclhash.mjs [--root dir] [--no-module] [-o manifest] [path...]\n' +
    '       tsdeclhash.mjs --compare base.txt head.txt\n' +
    '       tsdeclhash.mjs --base <git-ref> [--head <git-ref>] [--root dir] [--no-module] [path...]\n';
  let opts;
  try {
    opts = parseArgs(argv, { root: 'value', o: 'value', 'no-module': 'bool', compare: 'bool', base: 'value', head: 'value', help: 'bool' });
  } catch (e) {
    process.stderr.write(`tsdeclhash: ${e.message}\n${usage}`);
    return 2;
  }
  if (opts.help || (opts.head && !opts.base)) {
    process.stderr.write(usage);
    return 2;
  }
  const report = (base, head, what) => {
    const { diffs, total } = diffManifests(base, head);
    for (const d of diffs) process.stdout.write(`${d}\n`);
    if (diffs.length === 0) {
      process.stdout.write(`tsdeclhash: ${total} declarations identical in ${what}\n`);
      return 0;
    }
    process.stdout.write(`tsdeclhash: ${diffs.length} of ${total} declarations differ between ${what}\n`);
    return 1;
  };
  try {
    if (opts.compare) {
      if (opts.rest.length !== 2) {
        process.stderr.write(usage);
        return 2;
      }
      const read = (p) => fs.readFileSync(p, 'utf8').split('\n').filter(Boolean);
      const [b, h] = opts.rest.map(read);
      if (b.length + h.length === 0) throw new Error(`${opts.rest[0]} and ${opts.rest[1]} list no declarations`);
      return report(b, h, `${opts.rest[0]} and ${opts.rest[1]}`);
    }
    const root = path.resolve(opts.root ?? FRONTEND_DIR);
    const paths = (opts.rest.length ? opts.rest : [path.join(root, 'src')]).map((p) => path.resolve(p));
    const sides = (opts.base ? [opts.base, opts.head] : [undefined]).map((ref) => load(root, ref, paths));
    requireSources(root, paths, ...sides);
    const [b, h] = sides.map((mods) => manifest(mods, { noModule: !!opts['no-module'] }));
    if (opts.base) return report(b, h, `${opts.base} and ${opts.head ?? 'the working tree'}`);
    const text = b.join('\n') + '\n';
    if (opts.o) fs.writeFileSync(opts.o, text);
    else process.stdout.write(text);
    return 0;
  } catch (e) {
    process.stderr.write(`tsdeclhash: ${e.stderr ? e.stderr.toString().trim() : e.message}\n`);
    return 2;
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(fs.realpathSync(process.argv[1])).href) {
  process.exitCode = main(process.argv.slice(2));
}
