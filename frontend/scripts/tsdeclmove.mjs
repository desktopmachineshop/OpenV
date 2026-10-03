#!/usr/bin/env node
// tsdeclmove: generate a TypeScript pure move (refactor class A) from a spec
// keyed by declaration name, so that a move pull request is regenerated on
// the latest master, never rebased (rule R4). It is refactor step S14f's
// window generator for F1: the declarations of src/api/client.ts move into
// new modules (api/http.ts, api/types/<area>.ts, api/<area>.ts) and
// client.ts becomes a barrel that exports exactly what it exported before,
// so no import site changes and every vi.mock of it keeps intercepting.
//
//   node frontend/scripts/tsdeclmove.mjs [--root dir] [-n] [--no-typecheck] --spec <spec.json>
//
// Paths in the spec are relative to the root (default frontend/). The spec
// (frontend/scripts/specs/F1.json for F1) names the source module, a header
// comment for the barrel, and the new modules, each with a header comment
// and the names of the declarations it receives, as tsdeclhash names them;
// "default" names the listed module a type the spec does not name joins
// ("types") and the one any other declaration it does not name joins
// ("values").
//
// What moves where:
//   - Each declaration moves as the whole lines it occupies, with every
//     comment above it (attached or not) and the comment on its last line,
//     into its module in source order. Statements that follow each other in
//     the source and in their new module keep the text between them byte
//     for byte, so `git diff --color-moved` shows moved blocks; elsewhere
//     one blank line separates them. The comments above the source's first
//     statement, if separated from it by a blank line, are the file's header
//     and stay in the barrel.
//   - A statement that declares nothing (client.ts's two interceptors, a
//     side-effect import) moves with the runtime declaration before it.
//   - A new module imports what its declarations use: a name another new
//     module declares from there (`import type` when only types or type
//     declarations use it), and a name the source imported from the same
//     place, rewritten relative to its directory, in the source's form.
//   - A declaration that a module other than its own uses, and that has no
//     `export` modifier, is exported with an export list at the end of its
//     module: adding the modifier would change its text and its tsdeclhash
//     (API_BASE_URL, client, uploadConfig, saveBlob and downloadBlob in F1).
//     A type goes in an `export type { ... }` list, as isolatedModules
//     requires (TS1205), a value in an `export { ... }` list.
//   - The barrel keeps the source's wiring (its export lists, re-exports and
//     `export default <name>`) as it was, imports what that wiring names,
//     and re-exports each new module: `export type *` from one that holds
//     only types, `export *` from one whose exports are all the source's,
//     and a named list from any other. Its surface is the source's.
//
// Drift (R4): the spec is keyed by name, so it survives edits to the source.
// A declaration it does not name joins its default module, a name the
// source no longer declares is left out, and a module left with nothing is
// not written, each with a note.
//
// It refuses, writing nothing, what it cannot move whole (each a "refused:"
// line, the spec's or the source's to settle when F1 is regenerated, not a
// fault): a new module over MODULE_MAX_LINES or a barrel over
// BARREL_MAX_LINES (the F1 row's bounds); a target module that exists;
// statements with side effects split across modules, or moved to one that
// nothing imports (tsmovecheck certifies only their moving together, in
// order, to one new module the barrel loads); a split after which loading
// the barrel runs side effects in another order than loading the source did
// (tsmovecheck's "effects reordered": a module with side effects that the
// source imported, and only a module the barrel loads after the moved
// statements still imports); a runtime import cycle among the modules; one
// statement's names placed apart; a module-scope `let` or `var` placed apart
// from a statement that assigns it (a module cannot assign a binding it
// imports); an import a module would carry only for declarations that shadow
// it with a local of the same name (tsdeclhash binds by name, so the import
// must be there, and an unused one fails lint); a statement that declares
// nothing with no runtime declaration before it; two statements on one line;
// `export default` on a declaration or an expression, `export =`,
// `import x = ...`, and a `declare global` or `declare module '...'` block.
//
// Before writing it checks its own result as the Refactor guard's class A
// check will (tsdeclhash --no-module identical over src/, and tsmovecheck's
// pure move with evaluation order), plus that the barrel's export surface is
// the source's, that each declaration is in its planned module, that each
// import is used and that no module exports a type in a value export list; a
// failure there is a fault in this tool, reported as "check:" lines. After
// writing it type-checks the root's tsconfig.json project and restores every
// file on an error ("check:" lines too; --no-typecheck skips that). -n
// prints the declaration-to-module map (the heads-up §6.9 posts) and writes
// nothing. On its own output it finds nothing to move and says so.
//
// Exit status: 0 moved (or nothing to move), 1 refused or failed a check
// (nothing written, or everything restored), 2 a usage, spec or read error.

import ts from 'typescript';
import fs from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { FRONTEND_DIR, analyzeModule, analyzeTree, diffManifests, manifest, parseArgs, readWorkingTree, runtimeImports } from './tsdeclhash.mjs';
import { moveCheck } from './tsmovecheck.mjs';

/** The F1 row's bounds: the barrel, and the largest new module. */
export const BARREL_MAX_LINES = 60;
export const MODULE_MAX_LINES = 400;
/** Import and export lists longer than this are written one name a line. */
const WIDTH = 100;

const posix = path.posix;
const byText = (a, b) => (a < b ? -1 : a > b ? 1 : 0);

/** What the spec or the source must settle: nothing is written. */
export class Refusal extends Error {
  constructor(reasons) {
    super(reasons.join('\n'));
    this.reasons = reasons;
  }
}
/** A malformed spec, or a source that cannot be read. */
export class SpecError extends Error {}
/** The result is not what it must be: a fault in this tool. */
export class CheckError extends Error {
  constructor(problems) {
    super(problems.join('\n'));
    this.problems = problems;
  }
}

// ------------------------------------------------------------------- spec

const SPEC_KEYS = new Set(['description', 'source', 'barrelHeader', 'default', 'modules']);
const MODULE_KEYS = new Set(['path', 'header', 'declarations']);
const MODULE_PATH = /^src\/(?:[A-Za-z0-9_-][A-Za-z0-9_.-]*\/)*[A-Za-z0-9_-][A-Za-z0-9_.-]*\.tsx?$/;
const NAME = /^[A-Za-z_$][\w$]*$/;

/** Validates a parsed spec; throws SpecError. */
export function parseSpec(raw, where = 'spec') {
  const fail = (msg) => {
    throw new SpecError(`${where}: ${msg}`);
  };
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) fail('not a JSON object');
  for (const k of Object.keys(raw)) if (!SPEC_KEYS.has(k)) fail(`unknown key "${k}"`);
  if (raw.description !== undefined && typeof raw.description !== 'string') fail('description is not a string');
  const modulePath = (p, what) => {
    if (typeof p !== 'string' || !MODULE_PATH.test(p) || /\.d\.tsx?$/.test(p) || /\.(test|spec)\.tsx?$/.test(p)) {
      fail(`${what} ${JSON.stringify(p)} is not the path of a .ts or .tsx module under src/`);
    }
    return p;
  };
  const comment = (text, what) => {
    if (typeof text !== 'string' || !text.trim()) fail(`${what} is missing: a comment saying what the module holds`);
    const sf = ts.createSourceFile('header.ts', text, ts.ScriptTarget.Latest, true);
    if (sf.statements.length || !/^\s*(\/\/|\/\*)/.test(text)) fail(`${what} is not only a comment`);
    return text.replace(/\s+$/, '');
  };
  const source = modulePath(raw.source, 'source');
  const barrelHeader = comment(raw.barrelHeader, 'barrelHeader');
  if (!Array.isArray(raw.modules) || raw.modules.length === 0) fail('modules is not a non-empty list');
  const paths = new Set();
  const owner = new Map();
  const modules = raw.modules.map((m, i) => {
    if (!m || typeof m !== 'object' || Array.isArray(m)) fail(`modules[${i}] is not an object`);
    for (const k of Object.keys(m)) if (!MODULE_KEYS.has(k)) fail(`modules[${i}]: unknown key "${k}"`);
    const p = modulePath(m.path, `modules[${i}].path`);
    if (p === source) fail(`${p} is the source, which becomes the barrel`);
    const folded = p.toLowerCase().replace(/\.tsx?$/, '');
    if ([...paths].some((q) => q.toLowerCase().replace(/\.tsx?$/, '') === folded) || source.toLowerCase().replace(/\.tsx?$/, '') === folded) {
      fail(`${p} is listed twice, or names the same module as another path`);
    }
    paths.add(p);
    const header = comment(m.header, `${p}: header`);
    if (!Array.isArray(m.declarations)) fail(`${p}: declarations is not a list of names`);
    for (const n of m.declarations) {
      if (typeof n !== 'string' || !NAME.test(n)) {
        fail(`${p}: ${JSON.stringify(n)} is not a declaration name (a statement that declares nothing moves with the declaration before it, unnamed)`);
      }
      if (owner.has(n)) fail(`${n} is listed in ${owner.get(n)} and ${p}`);
      owner.set(n, p);
    }
    return { path: p, header, declarations: [...m.declarations] };
  });
  const d = raw.default;
  if (!d || typeof d !== 'object' || Array.isArray(d)) {
    fail('default is missing: {"types": <module a type the spec does not name joins>, "values": <module any other joins>}');
  }
  for (const k of Object.keys(d)) if (k !== 'types' && k !== 'values') fail(`default: unknown key "${k}"`);
  for (const k of ['types', 'values']) if (!paths.has(d[k])) fail(`default.${k} ${JSON.stringify(d[k])} is not one of the spec's modules`);
  return { description: raw.description ?? '', source, barrelHeader, modules, owner, default: { types: d.types, values: d.values } };
}

/** Reads and validates a spec file; throws SpecError. */
export function loadSpec(file) {
  let raw;
  try {
    raw = JSON.parse(fs.readFileSync(file, 'utf8'));
  } catch (e) {
    throw new SpecError(`${file}: ${e.message}`);
  }
  return parseSpec(raw, file);
}

// ----------------------------------------------------------------- source

const TYPE_KINDS = new Set([ts.SyntaxKind.InterfaceDeclaration, ts.SyntaxKind.TypeAliasDeclaration]);
const hasModifier = (node, kind) => (ts.canHaveModifiers(node) ? ts.getModifiers(node) ?? [] : []).some((m) => m.kind === kind);
const isRelative = (spec) => spec === '.' || spec === '..' || spec.startsWith('./') || spec.startsWith('../');
const lineEnd = (text, pos) => {
  const i = text.indexOf('\n', pos);
  return i < 0 ? text.length : i + 1;
};
const lineStart = (text, pos) => text.lastIndexOf('\n', pos - 1) + 1;
const lineOf = (text, pos) => text.slice(0, pos).split('\n').length;
const kindOf = (p) => (p.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS);

/** Where the comments attached to a statement start (tsdeclhash's rule:
 * no blank line between them and the statement). */
function attachedStart(text, sf, st) {
  const ranges = ts.getLeadingCommentRanges(text, st.getFullStart()) ?? [];
  let next = st.getStart(sf);
  for (let i = ranges.length - 1; i >= 0; i--) {
    if ((text.slice(ranges[i].end, next).match(/\n/g) ?? []).length > 1) break;
    next = ranges[i].pos;
  }
  return next;
}

/** Parses the source into its statements: each with its kind, the names it
 * declares and uses (tsdeclhash's), and the text it carries when it moves. */
export function readSource(modPath, raw) {
  const text = raw.replace(/\r\n/g, '\n');
  const sf = ts.createSourceFile(modPath, text, ts.ScriptTarget.Latest, true, kindOf(modPath));
  const mod = analyzeModule(modPath, text);
  const refusals = [];
  const at = (st) => `${modPath}:${lineOf(text, st.getStart(sf))}`;
  const tails = [];
  for (const [i, st] of sf.statements.entries()) {
    if (i > 0 && st.getStart(sf) < tails[i - 1]) refusals.push(`${at(st)}: two statements on one line; put each on its own line`);
    let end = st.end;
    for (const r of ts.getTrailingCommentRanges(text, st.end) ?? []) {
      if (!text.slice(st.end, r.pos).includes('\n')) end = Math.max(end, r.end);
    }
    tails.push(lineEnd(text, end));
  }
  const writes = moduleWrites(modPath, sf);
  let header = '';
  const statements = sf.statements.map((st, i) => {
    let from = i ? tails[i - 1] : 0;
    if (i === 0) {
      const attached = lineStart(text, attachedStart(text, sf, st));
      header = text.slice(0, attached).replace(/\s+$/, '');
      from = attached;
    }
    const s = { index: i, node: st, line: lineOf(text, st.getStart(sf)), chunk: text.slice(from, tails[i]), names: [], refs: new Set(), writes: writes[i] };
    const decls = mod.decls.filter((d) => d.pos === i);
    if (ts.isImportDeclaration(st) && st.importClause) {
      s.kind = 'import';
      s.bindings = importBindings(st);
    } else if (ts.isImportEqualsDeclaration(st)) {
      refusals.push(`${at(st)}: \`import ${st.name.text} = ...\` cannot move; write it as an import declaration`);
      s.kind = 'wiring';
    } else if (ts.isExportDeclaration(st)) {
      s.kind = 'wiring';
      if (!st.moduleSpecifier && st.exportClause && ts.isNamedExports(st.exportClause)) {
        for (const el of st.exportClause.elements) s.refs.add((el.propertyName ?? el.name).text);
      }
    } else if (ts.isExportAssignment(st)) {
      s.kind = 'wiring';
      if (st.isExportEquals) refusals.push(`${at(st)}: \`export =\` cannot move`);
      else if (!ts.isIdentifier(st.expression)) {
        refusals.push(`${at(st)}: \`export default <expression>\` cannot move whole; declare it under a name and export that name as the default`);
      } else s.refs.add(st.expression.text);
    } else {
      s.kind = 'decl';
      if (hasModifier(st, ts.SyntaxKind.DefaultKeyword)) {
        refusals.push(`${at(st)}: \`export default\` on a declaration cannot move whole; declare it without the modifier and export its name as the default`);
      }
      if (ts.isModuleDeclaration(st) && (!ts.isIdentifier(st.name) || st.name.text === 'global' || st.flags & ts.NodeFlags.GlobalAugmentation)) {
        refusals.push(`${at(st)}: a \`declare global\` or \`declare module '...'\` block applies where it sits and cannot move`);
      }
      s.names = decls.map((d) => d.name);
      s.named = decls.filter((d) => !d.name.startsWith('(')).map((d) => d.name);
      s.refs = new Set([...(decls[0]?.refs ?? [])].filter((r) => !decls[0].selfNames.has(r)));
      s.exportedAs = decls.flatMap((d) => d.exportedAs);
      s.type = TYPE_KINDS.has(st.kind);
      s.runtime = decls.some((d) => d.runtime);
      s.effectful = decls.some((d) => d.effectful);
    }
    return s;
  });
  const eof = text.slice(tails.at(-1) ?? 0);
  const quote = sf.statements.find((st) => ts.isImportDeclaration(st))?.moduleSpecifier.getText(sf)[0] ?? "'";
  return { path: modPath, text, sf, mod, statements, header, eof, quote, refusals };
}

/** A program of one module alone (no import resolved, no lib): what the
 * checker can tell within the module, such as what a name is bound to. */
function oneModuleProgram(modPath, sf, options = {}) {
  const host = {
    getSourceFile: (f) => (f === modPath ? sf : undefined),
    fileExists: (f) => f === modPath,
    readFile: (f) => (f === modPath ? sf.text : undefined),
    getDefaultLibFileName: () => 'lib.d.ts',
    writeFile: () => {},
    getCurrentDirectory: () => '/',
    getCanonicalFileName: (f) => f,
    useCaseSensitiveFileNames: () => true,
    getNewLine: () => '\n',
  };
  return ts.createProgram([modPath], { noResolve: true, noLib: true, types: [], jsx: ts.JsxEmit.ReactJSX, ...options }, host);
}

/** For each statement, the module-scope `let` and `var` bindings it assigns
 * (`=`, a compound assignment, a destructuring one, `++`, `--`, or the
 * variable of a for-in or for-of loop), resolved by the checker so that a
 * local of the same name is not one. A module cannot assign a binding it
 * imports, so such a statement must land in its binding's module. */
function moduleWrites(modPath, sf) {
  const none = sf.statements.map(() => new Set());
  const lets = sf.statements.filter((st) => ts.isVariableStatement(st) && !(st.declarationList.flags & (ts.NodeFlags.Const | ts.NodeFlags.Using)));
  if (lets.length === 0) return none;
  const checker = oneModuleProgram(modPath, sf).getTypeChecker();
  const mutable = new Map(); // symbol -> name
  const bind = (name) => {
    if (ts.isIdentifier(name)) mutable.set(checker.getSymbolAtLocation(name), name.text);
    else for (const el of name.elements) if (!ts.isOmittedExpression(el)) bind(el.name);
  };
  for (const st of lets) for (const d of st.declarationList.declarations) bind(d.name);
  return sf.statements.map((st, i) => {
    const out = none[i];
    const hit = (sym) => mutable.has(sym) && out.add(mutable.get(sym));
    const target = (e) => {
      while (ts.isParenthesizedExpression(e) || ts.isNonNullExpression(e) || ts.isAsExpression(e) || ts.isSatisfiesExpression(e) || ts.isTypeAssertionExpression(e)) e = e.expression;
      if (ts.isIdentifier(e)) hit(checker.getSymbolAtLocation(e));
      else if (ts.isArrayLiteralExpression(e)) for (const el of e.elements) target(ts.isSpreadElement(el) ? el.expression : el);
      else if (ts.isObjectLiteralExpression(e)) {
        for (const p of e.properties) {
          if (ts.isShorthandPropertyAssignment(p)) hit(checker.getShorthandAssignmentValueSymbol(p));
          else if (ts.isPropertyAssignment(p)) target(p.initializer);
          else if (ts.isSpreadAssignment(p)) target(p.expression);
        }
      } else if (ts.isBinaryExpression(e) && e.operatorToken.kind === ts.SyntaxKind.EqualsToken) target(e.left); // a default in a pattern
    };
    const visit = (node) => {
      if (ts.isBinaryExpression(node) && node.operatorToken.kind >= ts.SyntaxKind.FirstAssignment && node.operatorToken.kind <= ts.SyntaxKind.LastAssignment) {
        target(node.left);
      } else if ((ts.isPrefixUnaryExpression(node) || ts.isPostfixUnaryExpression(node)) && (node.operator === ts.SyntaxKind.PlusPlusToken || node.operator === ts.SyntaxKind.MinusMinusToken)) {
        target(node.operand);
      } else if ((ts.isForInStatement(node) || ts.isForOfStatement(node)) && !ts.isVariableDeclarationList(node.initializer)) {
        target(node.initializer);
      }
      ts.forEachChild(node, visit);
    };
    visit(st);
    return out;
  });
}

function importBindings(st) {
  const clause = st.importClause;
  const out = [];
  if (clause.name) out.push({ local: clause.name.text, kind: 'default' });
  const nb = clause.namedBindings;
  if (nb && ts.isNamespaceImport(nb)) out.push({ local: nb.name.text, kind: 'namespace' });
  if (nb && ts.isNamedImports(nb)) for (const el of nb.elements) out.push({ local: el.name.text, kind: 'named', el });
  return out;
}

// --------------------------------------------------------------- planning

/** Where each statement goes, the notes on drift, and every refusal. */
function place(src, spec) {
  const refusals = [...src.refusals];
  const notes = [];
  const decls = src.statements.filter((s) => s.kind === 'decl');
  const declared = new Set(decls.flatMap((s) => s.named));
  // A name the spec does not list goes to a default module by what declares
  // it: types when every statement declaring it is a type declaration.
  const homeOf = new Map(); // name -> [module, 'types' | 'values']
  for (const n of declared) {
    if (spec.owner.has(n)) continue;
    const key = decls.filter((s) => s.named.includes(n)).every((s) => s.type) ? 'types' : 'values';
    homeOf.set(n, [spec.default[key], key]);
  }
  const target = new Map(); // statement index -> module path
  let lastRuntime = null;
  for (const s of decls) {
    if (s.named.length === 0) {
      if (!lastRuntime) {
        refusals.push(`${src.path}:${s.line}: ${s.names.join(', ')} declares nothing and has no runtime declaration before it to move with`);
      } else target.set(s.index, target.get(lastRuntime.index));
    } else {
      const listed = [...new Set(s.named.map((n) => spec.owner.get(n)).filter(Boolean))];
      if (listed.length > 1) {
        refusals.push(`${src.path}:${s.line}: ${s.named.join(', ')} are declared by one statement, which the spec splits across ${listed.join(' and ')}`);
      }
      const where = listed[0] ?? homeOf.get(s.named[0])[0];
      target.set(s.index, where);
      for (const n of s.named) {
        if (spec.owner.has(n)) continue;
        notes.push(
          listed.length
            ? `${n} is not in the spec: it moves to ${where} with ${s.named.find((m) => spec.owner.has(m))}, which the same statement declares`
            : `${n} is not in the spec: it joins ${where} (default.${homeOf.get(n)[1]}); name it in the spec to place it`,
        );
      }
    }
    if (s.runtime) lastRuntime = s;
  }
  // A name declared by several statements (declaration merging) lives in one module.
  for (const n of declared) {
    const where = new Set(decls.filter((s) => s.named.includes(n)).map((s) => target.get(s.index)));
    if (where.size > 1) refusals.push(`${n} is declared by several statements that would land in ${[...where].join(' and ')}`);
  }
  // A module-scope let or var stays with every statement that assigns it:
  // the module a writer lands in would import the binding, and a module
  // cannot assign a binding it imports.
  for (const s of decls) {
    if (!target.has(s.index)) continue;
    for (const n of s.writes) {
      const d = decls.find((x) => x.named.includes(n));
      if (!target.has(d.index) || target.get(d.index) === target.get(s.index)) continue;
      refusals.push(
        `${n} (${src.path}:${d.line}) is assigned by ${s.names.join(', ')} (line ${s.line}), which the plan puts in ${target.get(s.index)}, but ${n} lands in ${target.get(d.index)}; a module cannot assign a binding it imports: place them in one module in the spec`,
      );
    }
  }
  for (const m of spec.modules) {
    for (const n of m.declarations) if (!declared.has(n)) notes.push(`${n} is in the spec but ${src.path} does not declare it: left out`);
  }
  // Statements with side effects: tsmovecheck certifies their move only
  // when all of them go, in order, to one new module the barrel loads.
  const effects = decls.filter((s) => s.effectful);
  const effectHomes = new Set(effects.map((s) => target.get(s.index)));
  if (effectHomes.size > 1) {
    refusals.push(
      `the statements with side effects must move together to one module (tsmovecheck certifies only that), and the spec puts ${effects
        .map((s) => `${s.names.join(', ')} (line ${s.line}) in ${target.get(s.index)}`)
        .join(', ')}`,
    );
  }
  return { target, notes, refusals, effects: effectHomes.size === 1 ? [...effectHomes][0] : null };
}

/** Relative module specifier from one module path to another (no extension). */
function relativeSpec(from, to) {
  let rel = posix.relative(posix.dirname(from), to.replace(/\.tsx?$/, ''));
  if (!rel.startsWith('.')) rel = `./${rel}`;
  return rel;
}

/** Rewrites a specifier the source used so it names the same module from `from`. */
function respecifier(source, from, spec) {
  if (!isRelative(spec)) return spec;
  const abs = posix.normalize(posix.join(posix.dirname(source), spec));
  let rel = posix.relative(posix.dirname(from), abs);
  if (!rel.startsWith('.')) rel = `./${rel}`;
  return rel;
}

function list(open, names, close) {
  const one = `${open} { ${names.join(', ')} }${close}`;
  return one.length <= WIDTH ? one : `${open} {\n${names.map((n) => `  ${n},`).join('\n')}\n}${close}`;
}

/** An import of the source's, keeping only some of its bindings. */
function renderImport(src, s, keep, spec) {
  const st = s.node;
  const clause = st.importClause;
  const q = st.moduleSpecifier.getText(src.sf)[0];
  const tail = ` from ${q}${spec}${q};`;
  const kw = clause.isTypeOnly ? 'import type' : 'import';
  const head = [];
  const named = [];
  for (const b of s.bindings) {
    if (!keep.has(b.local)) continue;
    if (b.kind === 'default') head.push(b.local);
    else if (b.kind === 'namespace') head.push(`* as ${b.local}`);
    else named.push(b.el.getText(src.sf));
  }
  if (named.length === 0) return `${kw} ${head.join(', ')}${tail}`;
  return list(`${kw} ${head.map((h) => `${h}, `).join('')}`.replace(/ $/, ''), named, tail);
}

const lines = (text) => text.split('\n').length - (text.endsWith('\n') ? 1 : 0);

/** Plans the move and renders every file; throws Refusal. */
export function plan(spec, src) {
  const { target, notes, refusals, effects } = place(src, spec);
  if (refusals.length) throw new Refusal(refusals);
  const byPath = new Map(spec.modules.map((m) => [m.path, { ...m, statements: [] }]));
  for (const s of src.statements) if (target.has(s.index)) byPath.get(target.get(s.index)).statements.push(s);
  const modules = [...byPath.values()].filter((m) => {
    if (m.statements.length) return true;
    notes.push(`${m.path} gets no declaration: not written`);
    return false;
  });
  const order = new Map(modules.map((m, i) => [m.path, i]));

  // Where each name lives now, what each statement is, who imports what.
  const home = new Map();
  const declKinds = new Map(); // name -> [is a type declaration, ...]
  for (const m of modules) {
    for (const s of m.statements) {
      for (const n of s.named) {
        home.set(n, m.path);
        declKinds.set(n, [...(declKinds.get(n) ?? []), s.type]);
      }
    }
  }
  const isType = (n) => declKinds.get(n).every(Boolean);
  const binding = new Map(); // local name -> the import statement that binds it
  for (const s of src.statements) if (s.kind === 'import') for (const b of s.bindings) binding.set(b.local, s);
  const exportedByModifier = new Set(src.statements.flatMap((s) => (s.kind === 'decl' ? s.exportedAs : [])));
  const helpers = new Map(modules.map((m) => [m.path, new Set()])); // names a module must export with a list
  const valueImported = new Set(); // modules some module or the barrel imports a value from

  /** The imports a module (or the barrel) needs for the given statements. */
  const importsFor = (self, statements, isTypeUser) => {
    const orig = new Map(); // import statement index -> Set of locals
    const moved = new Map(); // module -> Map(name -> type-only)
    for (const s of statements) {
      for (const r of s.refs) {
        if (home.has(r)) {
          const where = home.get(r);
          if (where === self) continue;
          if (!moved.has(where)) moved.set(where, new Map());
          const typeOnly = isType(r) || isTypeUser(s);
          const names = moved.get(where);
          names.set(r, names.has(r) ? names.get(r) && typeOnly : typeOnly);
          if (!exportedByModifier.has(r)) helpers.get(where).add(r);
        } else if (binding.has(r)) {
          const imp = binding.get(r);
          if (!orig.has(imp.index)) orig.set(imp.index, new Set());
          orig.get(imp.index).add(r);
        }
      }
    }
    const out = [];
    for (const [index, keep] of [...orig].sort(([a], [b]) => a - b)) {
      const s = src.statements[index];
      const spec = respecifier(src.path, self, s.node.moduleSpecifier.text);
      if (keep.size < s.bindings.length) {
        out.push(renderImport(src, s, keep, spec));
        continue;
      }
      // Whole: the source's own text, only the specifier rewritten, and in
      // the barrel, which is the source, with the comments above it.
      const st = s.node;
      const from = st.getStart(src.sf);
      const lit = st.moduleSpecifier;
      let text = src.text.slice(from, lit.getStart(src.sf) + 1) + spec + src.text.slice(lit.end - 1, st.end);
      if (self === src.path) text = importComments(src, s) + text;
      out.push(text);
    }
    for (const [where, names] of [...moved].sort(([a], [b]) => order.get(a) - order.get(b))) {
      const spec = `${src.quote}${relativeSpec(self, where)}${src.quote};`;
      const values = [...names].filter(([, t]) => !t).map(([n]) => n).sort(byText);
      if (values.length) valueImported.add(where);
      const types = [...names].filter(([, t]) => t).map(([n]) => n).sort(byText);
      if (values.length) out.push(list('import', values, ` from ${spec}`));
      if (types.length) out.push(list('import type', types, ` from ${spec}`));
    }
    return { lines: out, orig };
  };

  /** Joins statements, keeping the text between neighbours in the source. */
  const body = (statements) => {
    let out = '';
    let prev = null;
    for (const s of statements) {
      if (prev && prev.index === s.index - 1) out += s.chunk;
      else out += `\n${s.chunk.replace(/^(?:[ \t]*\n)+/, '')}`;
      prev = s;
    }
    return out;
  };

  const rendered = modules.map((m) => {
    const imports = importsFor(m.path, m.statements, (s) => s.type);
    return { ...m, imports: imports.lines };
  });
  // Barrel: the source's wiring, the imports it needs, then the re-exports.
  const wiring = src.statements.filter((s) => s.kind === 'wiring');
  const barrelImports = importsFor(src.path, wiring, () => false);
  for (const s of src.statements) {
    if (s.kind !== 'import' || !importComments(src, s)) continue;
    const used = barrelImports.orig.get(s.index);
    if (!used || used.size < s.bindings.length) {
      notes.push(`the comment above ${src.path}:${s.line}'s import from '${s.node.moduleSpecifier.text}' is not kept: that import leaves the barrel`);
    }
  }
  const texts = new Map();
  for (const m of rendered) {
    const exports = [...helpers.get(m.path)].sort((a, b) => srcIndex(m, a) - srcIndex(m, b));
    let text = `${m.header}\n`;
    if (m.imports.length) text += `\n${m.imports.join('\n')}\n`;
    text += body(m.statements);
    // A type is exported in a list of its own: isolatedModules, which the
    // frontend's tsconfig sets, refuses one in a value list (TS1205).
    const valueExports = exports.filter((n) => !isType(n));
    const typeExports = exports.filter(isType);
    if (exports.length) text += '\n';
    if (valueExports.length) text += `${list('export', valueExports, ';')}\n`;
    if (typeExports.length) text += `${list('export type', typeExports, ';')}\n`;
    texts.set(m.path, text);
    m.exports = new Set([...m.statements.flatMap((s) => s.exportedAs), ...exports]);
  }
  const reexports = [];
  for (const m of rendered) {
    const surface = [...new Set(m.statements.flatMap((s) => s.exportedAs))];
    if (surface.length === 0) continue;
    const spec = `${src.quote}${relativeSpec(src.path, m.path)}${src.quote};`;
    if (surface.length === m.exports.size) {
      reexports.push(`${m.statements.every((s) => s.type) ? 'export type *' : 'export *'} from ${spec}`);
      continue;
    }
    const values = surface.filter((n) => !isType(n)).sort(byText);
    const types = surface.filter(isType).sort(byText);
    if (values.length) reexports.push(list('export', values, ` from ${spec}`));
    if (types.length) reexports.push(list('export type', types, ` from ${spec}`));
  }
  let barrel = src.header ? `${src.header}\n\n` : '';
  barrel += `${spec.barrelHeader}\n`;
  if (barrelImports.lines.length) barrel += `\n${barrelImports.lines.join('\n')}\n`;
  const kept = body(wiring);
  if (kept) barrel += kept;
  if (reexports.length) barrel += `\n${reexports.join('\n')}\n`;
  if (src.eof.trim()) {
    barrel += `\n${src.eof.replace(/^\s+/, '')}`;
    notes.push(`the comments after ${src.path}'s last statement stay at the end of the barrel`);
  }

  // Budgets.
  const over = [];
  if (lines(barrel) > BARREL_MAX_LINES) over.push(`the barrel ${src.path} would be ${lines(barrel)} lines, over the ${BARREL_MAX_LINES} the F1 row allows`);
  for (const [p, t] of texts) {
    if (lines(t) > MODULE_MAX_LINES) over.push(`${p} would be ${lines(t)} lines, over the ${MODULE_MAX_LINES} the F1 row allows; split it in the spec`);
  }
  if (over.length) throw new Refusal(over);
  // An import nothing at module scope uses: one a declaration names but
  // reads as a local of the same name is the source's to settle; any other
  // would be this tool's fault.
  const shadowed = [];
  const stray = [];
  const usedNames = new Map([...rendered.map((m) => [m.path, m.statements]), [src.path, wiring]]);
  for (const [p, t] of [...texts, [src.path, barrel]]) {
    for (const name of unusedImports(p, t)) {
      if (usedNames.get(p).some((s) => s.refs.has(name))) {
        shadowed.push(`${p} would import ${name} for declarations that use a local of that name instead (tsdeclhash binds by name): rename the local, or move the declaration to the module that declares ${name}`);
      } else stray.push(`${p} imports ${name}, which nothing in it uses`);
    }
  }
  if (stray.length) throw new CheckError(stray);
  if (shadowed.length) throw new Refusal(shadowed);

  const map = rendered.map((m) => ({ path: m.path, names: m.statements.flatMap((s) => s.names), lines: lines(texts.get(m.path)) }));
  const moved = rendered.reduce((n, m) => n + m.statements.reduce((k, s) => k + s.names.length, 0), 0);
  // Whether the plan has anything load the module the side effects move to;
  // a surface the barrel re-exports with `export *` loads it too.
  const loaded = effects !== null && (valueImported.has(effects) || reexports.some((l) => l.startsWith('export ') && !l.startsWith('export type') && l.includes(`${relativeSpec(src.path, effects)}${src.quote}`)));
  return { files: texts, barrel, map, notes, moved, effects, effectsLoaded: loaded };
}

/** The comments above an import of the source (not the file's header). */
function importComments(src, s) {
  const st = s.node;
  const from = s.index === 0 ? lineStart(src.text, attachedStart(src.text, src.sf, st)) : st.getFullStart();
  const comments = src.text.slice(from, st.getStart(src.sf)).trim();
  return comments ? `${comments}\n` : '';
}

function srcIndex(m, name) {
  return m.statements.findIndex((s) => s.named.includes(name));
}

/** The names a rendered module imports that nothing at module scope uses
 * (a declaration that names one may read a local of the same name, which
 * tsdeclhash cannot tell from the module-level one). */
function unusedImports(modPath, text) {
  const sf = ts.createSourceFile(modPath, text, ts.ScriptTarget.Latest, true, kindOf(modPath));
  const program = oneModuleProgram(modPath, sf);
  const checker = program.getTypeChecker();
  const file = program.getSourceFile(modPath);
  const aliases = new Map();
  for (const st of file.statements) {
    if (!ts.isImportDeclaration(st) || !st.importClause) continue;
    const c = st.importClause;
    const names = [c.name, c.namedBindings && ts.isNamespaceImport(c.namedBindings) ? c.namedBindings.name : null];
    if (c.namedBindings && ts.isNamedImports(c.namedBindings)) names.push(...c.namedBindings.elements.map((e) => e.name));
    for (const n of names) if (n) aliases.set(checker.getSymbolAtLocation(n), n.text);
  }
  const used = new Set();
  const visit = (node) => {
    if (ts.isImportDeclaration(node)) return;
    if (ts.isIdentifier(node)) {
      const p = node.parent;
      let sym;
      if (ts.isShorthandPropertyAssignment(p) && p.name === node) sym = checker.getShorthandAssignmentValueSymbol(p);
      else if (ts.isExportSpecifier(p) && !p.parent.parent.moduleSpecifier) sym = checker.getExportSpecifierLocalTargetSymbol(p);
      else sym = checker.getSymbolAtLocation(node);
      if (sym && aliases.has(sym)) used.add(sym);
    }
    ts.forEachChild(node, visit);
  };
  visit(file);
  return [...aliases].filter(([sym]) => !used.has(sym)).map(([, name]) => name);
}

// ------------------------------------------------------------- self-check

const PROBE = '__tsdeclmove_surface_probe__.ts';
/** tsmovecheck's failure for effects that loading a module runs in another
 * order: the module of the one that now runs first, and of the one it now
 * runs before. */
const EFFECTS_REORDERED = /^effects reordered when \S+ loads: .+ in (\S+) now runs before .+ in (\S+)$/;
/** TS1205: a type in a value export list, which isolatedModules refuses. */
const TYPE_IN_VALUE_EXPORT = 1205;

/** The errors a type check under isolatedModules finds in one rendered
 * module taken alone: a type exported in a value list. */
function exportListErrors(modPath, text) {
  const sf = ts.createSourceFile(modPath, text, ts.ScriptTarget.Latest, true, kindOf(modPath));
  return oneModuleProgram(modPath, sf, { isolatedModules: true })
    .getSemanticDiagnostics(sf)
    .filter((d) => d.code === TYPE_IN_VALUE_EXPORT)
    .map((d) => {
      const { line, character } = sf.getLineAndCharacterOfPosition(d.start);
      return `${modPath}:${line + 1}:${character + 1}: ${ts.flattenDiagnosticMessageText(d.messageText, ' ')}`;
    });
}

/** The names a module exports, following `export *` (tsdeclhash's rule). */
function surfaceNames(modules, modPath, seen = new Set()) {
  const out = new Set();
  const mod = modules.get(modPath);
  if (!mod || seen.has(modPath)) return out;
  seen.add(modPath);
  for (const d of mod.decls) for (const n of d.exportedAs) out.add(n);
  for (const n of mod.localExports.keys()) out.add(n);
  for (const re of mod.reexports) {
    if (!re.star) out.add(re.exported);
    else if (isRelative(re.spec)) {
      const base = posix.normalize(posix.join(posix.dirname(modPath), re.spec));
      const hit = [base, `${base}.ts`, `${base}.tsx`, `${base}/index.ts`, `${base}/index.tsx`].find((c) => modules.has(c));
      for (const n of surfaceNames(modules, hit, seen)) if (n !== 'default') out.add(n);
    } else out.add(`* ${re.spec}`);
  }
  return out;
}

/** Whether evaluating one module loads another, directly or not. */
function loads(modules, from, to) {
  const seen = new Set([from]);
  const work = [from];
  while (work.length) {
    const mod = modules.get(work.pop());
    for (const k of mod ? runtimeImports(modules, mod) : []) {
      if (k === to) return true;
      if (!seen.has(k)) seen.add(k), work.push(k);
    }
  }
  return false;
}

/** Checks a planned move as the Refactor guard's class A check will, over
 * the whole tree: tsdeclhash --no-module identical and tsmovecheck's pure
 * move; plus the barrel's surface, each declaration's module, and no type
 * in a value export list (TS1205, which the type check after writing would
 * find only once the files are written). Throws Refusal for what the spec
 * settles (a runtime import cycle, effects the barrel no longer loads first
 * or runs in another order) and CheckError for anything else. */
export function selfCheck(files, spec, result) {
  const base = new Map(files);
  const head = new Map(files);
  head.set(spec.source, result.barrel);
  for (const [p, t] of result.files) head.set(p, t);
  let probe = posix.join(posix.dirname(spec.source), PROBE);
  while (base.has(probe)) probe = probe.replace(/\.ts$/, '_.ts');
  const probeText = `import * as surface from '${relativeSpec(probe, spec.source)}';\nexport const probe = surface;\n`;
  base.set(probe, probeText);
  head.set(probe, probeText);
  const b = analyzeTree(base);
  const h = analyzeTree(head);
  const problems = [];
  const refusals = [];
  const r = moveCheck(b, h);
  for (const f of r.failures) {
    if (f.startsWith('new import cycle')) refusals.push(`${f}: the modules would import each other at run time; place the declarations so that they do not`);
    else if (f.startsWith('effects reordered')) {
      const [, first, later] = f.match(EFFECTS_REORDERED) ?? [];
      refusals.push(
        first
          ? `${f}, which ${spec.source} ran first; place the declarations so that ${first} imports ${later} (one that uses it, say), or settle it in the source`
          : `${f}; place the declarations so that they run in their old order, or settle it in the source`,
      );
    } else if (f.includes('changes when it runs') && result.effects && !result.effectsLoaded && !loads(h, spec.source, result.effects)) {
      refusals.push(`${f}: nothing ${spec.source} loads imports ${result.effects} at run time; place a declaration the others use there`);
    } else if (f.includes('changes when it runs')) problems.push(`tsmovecheck: ${f}`);
    else if (f.includes(PROBE.replace(/\.ts$/, ''))) problems.push('the barrel exports a name, or binds one, differently from the source (see below)');
    else problems.push(`tsmovecheck: ${f}`);
  }
  const { diffs } = diffManifests(manifest(b, { noModule: true }), manifest(h, { noModule: true }));
  for (const d of diffs) problems.push(`tsdeclhash --no-module: ${d.replace(/\t/g, ' ')}`);
  const before = surfaceNames(b, spec.source);
  const after = surfaceNames(h, spec.source);
  for (const n of [...before].filter((n) => !after.has(n)).sort(byText)) problems.push(`the barrel no longer exports ${n}`);
  for (const n of [...after].filter((n) => !before.has(n)).sort(byText)) problems.push(`the barrel exports ${n}, which the source did not`);
  for (const { path: p, names } of result.map) {
    const got = new Set(h.get(p)?.decls.map((d) => d.name) ?? []);
    for (const n of names) if (!got.has(n)) problems.push(`${n} is not in ${p}, where the plan puts it`);
  }
  const left = h.get(spec.source).decls.map((d) => d.name);
  if (left.length) problems.push(`the barrel still declares ${left.join(', ')}`);
  for (const [p, t] of [...result.files, [spec.source, result.barrel]]) problems.push(...exportListErrors(p, t));
  if (refusals.length) throw new Refusal(refusals);
  if (problems.length) throw new CheckError(problems);
  return r;
}

// ---------------------------------------------------------------- applying

/** Type-checks the project in root/tsconfig.json; returns the errors. */
export function typecheck(root) {
  const configPath = path.join(root, 'tsconfig.json');
  const cfg = ts.readConfigFile(configPath, ts.sys.readFile);
  if (cfg.error) return [ts.flattenDiagnosticMessageText(cfg.error.messageText, '\n')];
  // The config's path, not the process's directory, anchors its "types".
  const parsed = ts.parseJsonConfigFileContent(cfg.config, ts.sys, root, undefined, configPath);
  const program = ts.createProgram(parsed.fileNames, parsed.options);
  return ts
    .getPreEmitDiagnostics(program)
    .filter((d) => d.category === ts.DiagnosticCategory.Error)
    .map((d) => {
      const msg = ts.flattenDiagnosticMessageText(d.messageText, '\n');
      if (!d.file) return msg;
      const { line, character } = d.file.getLineAndCharacterOfPosition(d.start);
      return `${path.relative(root, d.file.fileName)}:${line + 1}:${character + 1}: ${msg}`;
    });
}

/**
 * Plans, checks and (unless dryRun) writes the move under root. Returns
 * { nothing, notes, map, moved, barrelLines }; throws SpecError, Refusal
 * or CheckError, and writes nothing or restores everything when it throws.
 * typecheck: a function (root) => errors run after writing, or false.
 */
export function apply(root, spec, { dryRun = false, typecheck: check = typecheck } = {}) {
  const files = readWorkingTree(root, [path.join(root, 'src')]);
  if (!files.has(spec.source)) throw new SpecError(`${path.join(root, spec.source)}: no such module`);
  const src = readSource(spec.source, files.get(spec.source));
  if (!src.statements.some((s) => s.kind === 'decl')) {
    return { nothing: true, notes: [`nothing to move: ${spec.source} declares nothing (it is already a barrel)`], map: [], moved: 0 };
  }
  const exists = spec.modules.filter((m) => fs.existsSync(path.join(root, m.path)) || files.has(m.path)).map((m) => `${m.path} exists; a move creates its modules`);
  if (exists.length) throw new Refusal(exists);
  const result = plan(spec, src);
  selfCheck(files, spec, result);
  const summary = { nothing: false, notes: result.notes, map: result.map, moved: result.moved, barrelLines: lines(result.barrel) };
  if (dryRun) return summary;
  const created = [];
  const dirs = [];
  const restore = () => {
    for (const f of created) fs.rmSync(f, { force: true });
    for (const d of dirs.reverse()) fs.rmSync(d, { recursive: true, force: true });
    fs.writeFileSync(path.join(root, spec.source), files.get(spec.source));
  };
  try {
    for (const [p, t] of result.files) {
      const abs = path.join(root, p);
      for (let d = path.dirname(abs); !fs.existsSync(d); d = path.dirname(d)) dirs.unshift(d);
      fs.mkdirSync(path.dirname(abs), { recursive: true });
      fs.writeFileSync(abs, t, { flag: 'wx' });
      created.push(abs);
    }
    fs.writeFileSync(path.join(root, spec.source), result.barrel);
  } catch (e) {
    restore();
    throw new SpecError(`writing the move: ${e.message}`);
  }
  if (check) {
    const errors = check(root);
    if (errors.length) {
      restore();
      throw new CheckError([`the moved tree does not type-check, so every file is restored:`, ...errors.slice(0, 40)]);
    }
  }
  return summary;
}

// -------------------------------------------------------------------- CLI

function main(argv) {
  const usage = 'usage: tsdeclmove.mjs [--root dir] [-n] [--no-typecheck] --spec <spec.json>\n';
  let opts;
  try {
    opts = parseArgs(argv, { root: 'value', spec: 'value', n: 'bool', 'no-typecheck': 'bool', help: 'bool' });
  } catch (e) {
    process.stderr.write(`tsdeclmove: ${e.message}\n${usage}`);
    return 2;
  }
  if (opts.help || !opts.spec || opts.rest.length) {
    process.stderr.write(usage);
    return 2;
  }
  const root = path.resolve(opts.root ?? FRONTEND_DIR);
  const out = (l) => process.stdout.write(`${l}\n`);
  try {
    const spec = loadSpec(opts.spec);
    const r = apply(root, spec, { dryRun: !!opts.n, typecheck: opts['no-typecheck'] ? false : typecheck });
    for (const n of r.notes) out(`note: ${n}`);
    if (r.nothing) return 0;
    for (const m of r.map) out(`${m.path} (${m.lines} lines): ${m.names.join(', ')}`);
    out(
      `tsdeclmove: ${opts.n ? 'would move' : 'moved'} ${r.moved} declarations of ${spec.source} into ${r.map.length} modules; ${spec.source} ${opts.n ? 'would become' : 'is now'} a barrel of ${r.barrelLines} lines`,
    );
    return 0;
  } catch (e) {
    if (e instanceof Refusal) {
      for (const l of e.reasons) out(`refused: ${l}`);
      out('tsdeclmove: refused, nothing written: settle the lines above in the spec or the source, then run it again');
      return 1;
    }
    if (e instanceof CheckError) {
      for (const l of e.problems) out(`check: ${l}`);
      out('tsdeclmove: the result failed its checks, nothing written or everything restored (a fault in tsdeclmove, or a tree that did not type-check before)');
      return 1;
    }
    process.stderr.write(`tsdeclmove: ${e.message}\n`);
    return 2;
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(fs.realpathSync(process.argv[1])).href) {
  process.exitCode = main(process.argv.slice(2));
}
