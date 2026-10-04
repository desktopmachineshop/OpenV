// Go↔TS vocabulary parity (refactor plan S13, invariant I24). No snapshot:
// the pinned files are contracts/vocab.json, which TestVocabulary
// (internal/vocabparity/vocab_test.go) writes from the Go catalogues, and
// contracts/vocab-allowed-diffs.json, today's differences, both on the
// golden list.
//
// The frontend holds hand-written copies of vocabularies Go owns: link rules,
// artifact types and statuses, feature keys, event types, plans, error codes,
// V&V gap labels, providers, upload extensions, the API paths open without a
// session, the guided wizard's steps. COPIES below names each copy, read
// from the TypeScript source without running it: a declared list, map,
// union, switch or <select>, or, for feature keys, error codes and the 401
// interceptor's exemptions, every place the code uses one. A "mirror" copy
// should hold the whole vocabulary, so an item missing on either side is a
// difference; a "uses" copy names some of it, so only an item Go does not
// have is one. Every difference must be listed in
// contracts/vocab-allowed-diffs.json, and every listed one must still occur:
// a new drift on either side fails, and so does a fixed one until its entry
// is removed, so the list only shrinks. Both files change only in a
// behavior-change pull request with a release note. When refactor plan X4
// generates a vocabulary into src/generated/contract.ts, the readers here
// follow the copies into the generated module or their named overrides, and
// neither file changes.
//
// A copy is found by declaration name in its file, or, after a move, by the
// same name anywhere in the production sources when only one declaration has
// it; the ids in contracts/vocab-allowed-diffs.json name no file.
import fs from 'node:fs';
import path from 'node:path';
import ts from 'typescript';
import { beforeAll, describe, expect, it, vi } from 'vitest';
import { REPO, SRC, lineOf, literalText, parseFile, productionSources, readRepoFile, resolveModule, srcRel, walk } from './repo';

const VOCAB = 'contracts/vocab.json';
const ALLOWED = 'contracts/vocab-allowed-diffs.json';
const GO_REGENERATE = "UPDATE_GOLDEN=1 go test ./internal/vocabparity -count=1 -run '^TestVocabulary$'";
const ALLOWED_REGENERATE = 'cd frontend && UPDATE_GOLDEN=1 npx vitest run src/arch/vocabParity.test.ts';

// ---------------------------------------------------------------------------
// The Go side: contracts/vocab.json.
// ---------------------------------------------------------------------------

interface Vocab {
  link_rules: {
    type: string;
    label: string;
    inverse_label: string;
    allowed_from_types: string[];
    allowed_to_types: string[];
    description: string;
  }[];
  artifact_types: { value: string; label: string }[];
  quality_linted_types: string[];
  artifact_statuses: { value: string; next: string[] }[];
  feature_keys: string[];
  event_types: string[];
  plans: string[];
  error_codes: string[];
  gap_labels: { key: string; label: string }[];
  providers: string[];
  upload_extensions: { ext: string; kind: string }[];
  open_paths: { exact: string[]; prefixes: string[] };
  wizard_step_labels: string[];
}
type VocabName = keyof Vocab;

/** Field values of one item; arrays compare as sets. */
type Fields = Record<string, string | string[]>;
interface Item {
  fields: Fields;
  /** Where the TypeScript side names it, for messages only. */
  at?: string;
}
/** A vocabulary or a copy of one: item key -> fields, in source order. */
type Items = Map<string, Item>;

const items = (entries: [string, Fields][]): Items => new Map(entries.map(([k, fields]) => [k, { fields }]));
const plain = (keys: string[]): Items => items(keys.map((k) => [k, {}]));

/** Each vocabulary of vocab.json as items, with the fields a copy may mirror. */
function goItems(v: Vocab): Record<VocabName, Items> {
  return {
    link_rules: items(
      v.link_rules.map((r) => [
        r.type,
        {
          label: r.label,
          inverse_label: r.inverse_label,
          allowed_from_types: r.allowed_from_types,
          allowed_to_types: r.allowed_to_types,
          description: r.description,
        },
      ])
    ),
    artifact_types: items(v.artifact_types.map((t) => [t.value, { label: t.label }])),
    quality_linted_types: plain(v.quality_linted_types),
    artifact_statuses: items(v.artifact_statuses.map((s) => [s.value, { next: s.next }])),
    feature_keys: plain(v.feature_keys),
    event_types: plain(v.event_types),
    plans: plain(v.plans),
    error_codes: plain(v.error_codes),
    gap_labels: items(v.gap_labels.map((g) => [g.key, { label: g.label }])),
    providers: plain(v.providers),
    upload_extensions: items(v.upload_extensions.map((e) => [e.ext, { kind: e.kind }])),
    open_paths: items([
      ...v.open_paths.exact.map((p): [string, Fields] => [p, { match: 'exact' }]),
      ...v.open_paths.prefixes.map((p): [string, Fields] => [p, { match: 'prefix' }]),
    ]),
    wizard_step_labels: plain(v.wizard_step_labels),
  };
}

// ---------------------------------------------------------------------------
// The TypeScript side: a syntactic evaluator for the literals a copy is
// written in, following const declarations through imports and re-exports.
// ---------------------------------------------------------------------------

type Value = string | number | boolean | null | Value[] | { [key: string]: Value };

class EvalError extends Error {}

const sourceCache = new Map<string, ts.SourceFile>();
const sourceOf = (abs: string): ts.SourceFile => {
  let sf = sourceCache.get(abs);
  if (!sf) {
    sf = parseFile(abs);
    sourceCache.set(abs, sf);
  }
  return sf;
};

const at = (node: ts.Node) => `${srcRel(node.getSourceFile().fileName)}:${lineOf(node)}`;
const fail = (node: ts.Node, why: string): never => {
  throw new EvalError(`${at(node)}: ${why}: ${node.getText().replace(/\s+/g, ' ').slice(0, 80)}`);
};

const unwrap = (e: ts.Expression): ts.Expression => {
  while (
    ts.isParenthesizedExpression(e) ||
    ts.isAsExpression(e) ||
    ts.isSatisfiesExpression(e) ||
    ts.isNonNullExpression(e) ||
    ts.isTypeAssertionExpression(e)
  ) {
    e = e.expression;
  }
  return e;
};

const hasExport = (node: ts.Node) =>
  ts.canHaveModifiers(node) && !!ts.getModifiers(node)?.some((m) => m.kind === ts.SyntaxKind.ExportKeyword);

/** A top-level `const name = <init>` of a file, exported or not. */
function topLevelConst(sf: ts.SourceFile, name: string, exported: boolean): ts.VariableDeclaration | undefined {
  for (const st of sf.statements) {
    if (!ts.isVariableStatement(st) || (exported && !hasExport(st))) continue;
    if (!(st.declarationList.flags & ts.NodeFlags.Const)) continue;
    for (const d of st.declarationList.declarations) {
      if (ts.isIdentifier(d.name) && d.name.text === name && d.initializer) return d;
    }
  }
  return undefined;
}

/** The const a module exports as `name`, through export lists and re-exports. */
function exportedConst(file: string, name: string, depth = 0): ts.VariableDeclaration | undefined {
  if (depth > 8) return undefined;
  const sf = sourceOf(file);
  const direct = topLevelConst(sf, name, true);
  if (direct) return direct;
  for (const st of sf.statements) {
    if (!ts.isExportDeclaration(st) || st.isTypeOnly) continue;
    const from = st.moduleSpecifier ? literalText(st.moduleSpecifier) : null;
    const target = from ? resolveModule(file, from) : null;
    if (st.exportClause && ts.isNamedExports(st.exportClause)) {
      for (const el of st.exportClause.elements) {
        if (el.name.text !== name) continue;
        const local = (el.propertyName ?? el.name).text;
        if (target) return exportedConst(target, local, depth + 1);
        return topLevelConst(sf, local, false);
      }
    } else if (!st.exportClause && target) {
      const found = exportedConst(target, name, depth + 1); // export * from '...'
      if (found) return found;
    }
  }
  return undefined;
}

/** The const an identifier names in its file: its own, or one it imports by name. */
function constOf(id: ts.Identifier): ts.VariableDeclaration {
  const sf = id.getSourceFile();
  const own = topLevelConst(sf, id.text, false);
  if (own) return own;
  for (const st of sf.statements) {
    if (!ts.isImportDeclaration(st) || !st.importClause?.namedBindings) continue;
    const nb = st.importClause.namedBindings;
    if (!ts.isNamedImports(nb)) continue;
    for (const el of nb.elements) {
      if (el.name.text !== id.text) continue;
      const spec = literalText(st.moduleSpecifier);
      const target = spec ? resolveModule(sf.fileName, spec) : null;
      const found = target ? exportedConst(target, (el.propertyName ?? el.name).text) : undefined;
      if (found) return found;
    }
  }
  return fail(id, 'names no const this test can follow (a literal, or a const in this file or one it imports)');
}

/** The value of a literal expression, following consts; anything else fails. */
function valueOf(expr: ts.Expression, depth = 0): Value {
  if (depth > 16) fail(expr, 'too deep');
  const e = unwrap(expr);
  const text = literalText(e);
  if (text !== null) return text;
  if (ts.isNumericLiteral(e)) return Number(e.text);
  if (e.kind === ts.SyntaxKind.TrueKeyword) return true;
  if (e.kind === ts.SyntaxKind.FalseKeyword) return false;
  if (e.kind === ts.SyntaxKind.NullKeyword) return null;
  if (ts.isArrayLiteralExpression(e)) {
    const out: Value[] = [];
    for (const el of e.elements) {
      if (ts.isSpreadElement(el)) {
        const spread = valueOf(el.expression, depth + 1);
        if (!Array.isArray(spread)) fail(el, 'spreads something that is not an array');
        out.push(...(spread as Value[]));
      } else {
        out.push(valueOf(el, depth + 1));
      }
    }
    return out;
  }
  if (ts.isObjectLiteralExpression(e)) {
    const out: { [key: string]: Value } = {};
    for (const p of e.properties) {
      if (ts.isPropertyAssignment(p)) {
        const key = ts.isIdentifier(p.name) ? p.name.text : literalText(p.name);
        if (key === null) fail(p, 'has a computed key');
        out[key as string] = valueOf(p.initializer, depth + 1);
      } else if (ts.isShorthandPropertyAssignment(p)) {
        out[p.name.text] = valueOf(p.name, depth + 1);
      } else if (ts.isSpreadAssignment(p)) {
        const spread = valueOf(p.expression, depth + 1);
        if (!spread || typeof spread !== 'object' || Array.isArray(spread)) fail(p, 'spreads something that is not an object');
        Object.assign(out, spread);
      } else {
        fail(p, 'is not a plain property');
      }
    }
    return out;
  }
  if (ts.isIdentifier(e)) return valueOf(constOf(e).initializer!, depth + 1);
  if (ts.isPropertyAccessExpression(e)) {
    const obj = valueOf(e.expression, depth + 1);
    if (!obj || typeof obj !== 'object' || Array.isArray(obj) || !(e.name.text in obj)) fail(e, 'reads no property of a literal');
    return (obj as { [key: string]: Value })[e.name.text];
  }
  // new Set([...]) is its list.
  if (ts.isNewExpression(e) && ts.isIdentifier(e.expression) && e.expression.text === 'Set' && e.arguments?.length === 1) {
    return valueOf(e.arguments[0], depth + 1);
  }
  // [...].join(',') is the joined string.
  if (
    ts.isCallExpression(e) &&
    ts.isPropertyAccessExpression(e.expression) &&
    e.expression.name.text === 'join' &&
    e.arguments.length === 1
  ) {
    const list = valueOf(e.expression.expression, depth + 1);
    const sep = valueOf(e.arguments[0], depth + 1);
    if (!Array.isArray(list) || typeof sep !== 'string') fail(e, 'joins something that is not a list');
    return (list as Value[]).map(String).join(sep as string);
  }
  return fail(e, 'is not a literal this test can read');
}

const asString = (v: Value, node: ts.Node): string => (typeof v === 'string' ? v : fail(node, 'is not a string'));
const asStrings = (v: Value, node: ts.Node): string[] =>
  Array.isArray(v) && v.every((x) => typeof x === 'string') ? (v as string[]) : fail(node, 'is not a list of strings');
const asRecord = (v: Value, node: ts.Node): { [key: string]: Value } =>
  v && typeof v === 'object' && !Array.isArray(v) ? v : fail(node, 'is not an object literal');

// ---------------------------------------------------------------------------
// Finding a copy: a declaration by name, in its file or, once moved, the one
// declaration of that name in the production sources.
// ---------------------------------------------------------------------------

type Declaration = ts.VariableDeclaration | ts.FunctionDeclaration | ts.TypeAliasDeclaration;

function declarationsIn(sf: ts.SourceFile, name: string): Declaration[] {
  const out: Declaration[] = [];
  walk(sf, (node) => {
    if (
      (ts.isVariableDeclaration(node) && ts.isIdentifier(node.name) && node.name.text === name) ||
      ((ts.isFunctionDeclaration(node) || ts.isTypeAliasDeclaration(node)) && node.name?.text === name)
    ) {
      out.push(node as Declaration);
    }
  });
  return out;
}

let allSources: string[] | undefined;

function locate(file: string, name: string): Declaration {
  const home = path.join(SRC, file);
  const here = fs.existsSync(home) ? declarationsIn(sourceOf(home), name) : [];
  if (here.length === 1) return here[0];
  if (here.length > 1) throw new EvalError(`${file} declares ${name} ${here.length} times; name the copy more precisely`);
  allSources ??= productionSources();
  const elsewhere = allSources.flatMap((f) => declarationsIn(sourceOf(f), name));
  if (elsewhere.length === 1) return elsewhere[0];
  throw new EvalError(
    `no declaration ${name} in ${file}` +
      (elsewhere.length ? `, and ${elsewhere.length} elsewhere (${elsewhere.map(at).join(', ')})` : ', nor anywhere else') +
      ': the copy has moved or been renamed; point COPIES at it'
  );
}

function initializerOf(file: string, name: string): ts.Expression {
  const d = locate(file, name);
  if (!ts.isVariableDeclaration(d) || !d.initializer) throw new EvalError(`${at(d)}: ${name} is not a const with a value`);
  return d.initializer;
}

/** The function a declaration holds: a function declaration or a const arrow/function. */
function functionBody(file: string, name: string): ts.Node {
  const d = locate(file, name);
  if (ts.isFunctionDeclaration(d) && d.body) return d.body;
  if (ts.isVariableDeclaration(d) && d.initializer) {
    const init = unwrap(d.initializer);
    if (ts.isArrowFunction(init) || ts.isFunctionExpression(init)) return init.body;
  }
  throw new EvalError(`${at(d)}: ${name} is not a function`);
}

const withAt = (node: ts.Node, entries: [string, Fields][]): Items =>
  new Map(entries.map(([k, fields]) => [k, { fields, at: at(node) }]));

function noDuplicates(node: ts.Node, keys: string[]): string[] {
  const seen = new Set<string>();
  for (const k of keys) {
    if (seen.has(k)) fail(node, `lists ${JSON.stringify(k)} twice`);
    seen.add(k);
  }
  return keys;
}

/** A const list of strings (or a Set of them). */
function listOf(file: string, name: string): Items {
  const init = initializerOf(file, name);
  return withAt(init, noDuplicates(init, asStrings(valueOf(init), init)).map((k) => [k, {}]));
}

/** A const object's keys, with each value as the given field. */
function keysOf(file: string, name: string, valueField?: string): Items {
  const init = initializerOf(file, name);
  const rec = asRecord(valueOf(init), init);
  return withAt(
    init,
    Object.entries(rec).map(([k, v]) => [
      k,
      valueField ? { [valueField]: Array.isArray(v) ? asStrings(v, init) : asString(v, init) } : {},
    ])
  );
}

/** A const list of objects, keyed by one property, with others as fields. */
function objectsOf(file: string, name: string, key: string, fields: Record<string, string> = {}): Items {
  const init = initializerOf(file, name);
  const list = valueOf(init);
  if (!Array.isArray(list)) fail(init, 'is not a list');
  const rows = (list as Value[]).map((row) => asRecord(row, init));
  noDuplicates(init, rows.map((r) => asString(r[key], init)));
  return withAt(
    init,
    rows.map((r) => [
      asString(r[key], init),
      Object.fromEntries(
        Object.entries(fields).map(([tsName, field]) => {
          const v = r[tsName];
          return [field, Array.isArray(v) ? asStrings(v, init) : asString(v, init)];
        })
      ),
    ])
  );
}

/** A type alias that is a union of string literal types. */
function unionOf(file: string, name: string): Items {
  const d = locate(file, name);
  if (!ts.isTypeAliasDeclaration(d) || !ts.isUnionTypeNode(d.type)) throw new EvalError(`${at(d)}: ${name} is not a union type`);
  const keys = d.type.types.map((t) =>
    ts.isLiteralTypeNode(t) && ts.isStringLiteral(t.literal) ? t.literal.text : fail(t, 'is not a string literal type')
  );
  return withAt(d, noDuplicates(d, keys).map((k) => [k, {}]));
}

/** The string literals of the case clauses in a function. */
function casesOf(file: string, name: string): Items {
  const body = functionBody(file, name);
  const out: [string, Fields][] = [];
  walk(body, (node) => {
    if (ts.isCaseClause(node)) out.push([asString(valueOf(node.expression), node.expression), {}]);
  });
  return withAt(body, out);
}

/** The string literals a function compares something with (=== and !==). */
function comparedIn(file: string, name: string): Items {
  const body = functionBody(file, name);
  const out: Items = new Map();
  walk(body, (node) => {
    if (!ts.isBinaryExpression(node) || !isEquality(node.operatorToken.kind)) return;
    for (const side of [node.left, node.right]) {
      const lit = literalText(unwrap(side));
      if (lit !== null) out.set(lit, { fields: {}, at: at(side) });
    }
  });
  return out;
}

const isEquality = (k: ts.SyntaxKind) =>
  k === ts.SyntaxKind.EqualsEqualsEqualsToken ||
  k === ts.SyntaxKind.ExclamationEqualsEqualsToken ||
  k === ts.SyntaxKind.EqualsEqualsToken ||
  k === ts.SyntaxKind.ExclamationEqualsToken;

/** A JSX string attribute's text; undefined when absent, null when not a string literal. */
function jsxAttr(el: ts.JsxOpeningLikeElement, name: string): string | null | undefined {
  for (const a of el.attributes.properties) {
    if (ts.isJsxAttribute(a) && a.name.getText() === name && a.initializer) {
      return ts.isStringLiteral(a.initializer) ? a.initializer.text : null;
    }
  }
  return undefined;
}

function selectsIn(sf: ts.SourceFile, id: string): ts.JsxElement[] {
  const out: ts.JsxElement[] = [];
  walk(sf, (node) => {
    if (ts.isJsxElement(node) && node.openingElement.tagName.getText() === 'select' && jsxAttr(node.openingElement, 'id') === id) {
      out.push(node);
    }
  });
  return out;
}

/**
 * The <option value="...">Label</option>s of the <select id={id}> in a file,
 * or, once moved, of the one such <select> in the production sources.
 */
function selectOptions(file: string, id: string): Items {
  const home = path.join(SRC, file);
  let selects = fs.existsSync(home) && file.endsWith('.tsx') ? selectsIn(sourceOf(home), id) : [];
  if (selects.length === 0) {
    allSources ??= productionSources();
    selects = allSources.filter((f) => f.endsWith('.tsx')).flatMap((f) => selectsIn(sourceOf(f), id));
  }
  if (selects.length !== 1) {
    throw new EvalError(`${selects.length} <select id="${id}"> elements (${selects.map(at).join(', ')}), expected 1 in ${file}`);
  }
  const out: [string, Fields][] = [];
  for (const child of selects[0].children) {
    if (!ts.isJsxElement(child) || child.openingElement.tagName.getText() !== 'option') continue;
    const value = jsxAttr(child.openingElement, 'value') ?? fail(child, 'has no literal value');
    const label = child.children
      .map((c) => (ts.isJsxText(c) ? c.text : fail(c, 'is not plain text')))
      .join('')
      .trim();
    out.push([value, { label }]);
  }
  noDuplicates(selects[0], out.map(([k]) => k));
  return withAt(selects[0], out);
}

// ---------------------------------------------------------------------------
// Scans: vocabularies the code uses rather than declares.
// ---------------------------------------------------------------------------

interface Scan {
  found: Items;
  unresolved: string[];
}

function scan(files: string[], visit: (node: ts.Node, add: (key: string, node: ts.Node) => void, bad: (node: ts.Node) => void) => void): Scan {
  const found: Items = new Map();
  const unresolved: string[] = [];
  for (const file of files) {
    walk(sourceOf(file), (node) =>
      visit(
        node,
        (key, n) => {
          if (!found.has(key)) found.set(key, { fields: {}, at: at(n) });
        },
        (n) => unresolved.push(`${at(n)}  ${n.getText().replace(/\s+/g, ' ').slice(0, 80)}`)
      )
    );
  }
  return { found, unresolved };
}

const tryString = (expr: ts.Expression): string | null => {
  try {
    const v = valueOf(expr);
    return typeof v === 'string' ? v : null;
  } catch (e) {
    if (e instanceof EvalError) return null;
    throw e;
  }
};

/**
 * The feature keys the frontend gates on (release.Registry's keys): every
 * `const X_FEATURE = '...'`, every useFeature(...) argument, and every
 * literal read of a features map (`x.features['key']`).
 */
function featureGates(): Scan {
  return scan(productionSources(), (node, add, bad) => {
    if (ts.isVariableDeclaration(node) && ts.isIdentifier(node.name) && /_FEATURE$/.test(node.name.text) && node.initializer) {
      const key = tryString(node.initializer);
      if (key === null) bad(node);
      else add(key, node);
    } else if (ts.isCallExpression(node) && ts.isIdentifier(node.expression) && node.expression.text === 'useFeature') {
      const key = node.arguments.length === 1 ? tryString(node.arguments[0]) : null;
      if (key === null) bad(node);
      else add(key, node);
    } else if (
      ts.isElementAccessExpression(node) &&
      ts.isPropertyAccessExpression(unwrap(node.expression)) &&
      (unwrap(node.expression) as ts.PropertyAccessExpression).name.text === 'features'
    ) {
      const key = literalText(unwrap(node.argumentExpression));
      if (key !== null) add(key, node);
    }
  });
}

/** An expression that reads an API error's code: apiErrorCode(err), or `.code` of a response's data. */
function readsErrorCode(expr: ts.Expression): boolean {
  const e = unwrap(expr);
  if (ts.isCallExpression(e) && ts.isIdentifier(e.expression) && e.expression.text === 'apiErrorCode') return true;
  if (ts.isPropertyAccessExpression(e) && e.name.text === 'code') {
    const of = unwrap(e.expression);
    return (ts.isIdentifier(of) && of.text === 'data') || (ts.isPropertyAccessExpression(of) && of.name.text === 'data');
  }
  return false;
}

/** The API error codes the frontend branches on (comparisons and switches on a code read). */
function errorCodeChecks(): Scan {
  return scan(productionSources(), (node, add, bad) => {
    if (ts.isBinaryExpression(node) && isEquality(node.operatorToken.kind)) {
      for (const [code, other] of [
        [node.left, node.right],
        [node.right, node.left],
      ]) {
        if (!readsErrorCode(code)) continue;
        const lit = tryString(other);
        if (lit === null) bad(node);
        else add(lit, other);
      }
    } else if (ts.isSwitchStatement(node) && readsErrorCode(node.expression)) {
      for (const clause of node.caseBlock.clauses) {
        if (!ts.isCaseClause(clause)) continue;
        const lit = tryString(clause.expression);
        if (lit === null) bad(clause);
        else add(lit, clause);
      }
    }
  });
}

/** The API paths src/api's 401 interceptor never redirects for: .includes('/api/...') and .startsWith('/api/...'). */
function interceptorExemptions(): Scan {
  return scan(productionSources(path.join(SRC, 'api')), (node, add) => {
    if (
      ts.isCallExpression(node) &&
      ts.isPropertyAccessExpression(node.expression) &&
      ['includes', 'startsWith'].includes(node.expression.name.text)
    ) {
      const text = literalText(node.arguments[0]);
      if (text?.startsWith('/api/')) add(text, node);
    }
  });
}

// ---------------------------------------------------------------------------
// The copies. `id` is the name contracts/vocab-allowed-diffs.json uses; it
// names no file, so moving a copy leaves that file alone.
// ---------------------------------------------------------------------------

interface Copy {
  id: string;
  vocabulary: VocabName;
  /** mirror: the whole vocabulary, so a missing item is a difference; uses: some of it. */
  kind: 'mirror' | 'uses';
  /** Also compare the order of the items both sides hold. */
  ordered?: boolean;
  /** Where it is, for messages. */
  where: string;
  read: () => Items | Scan;
  /** The Go vocabulary in this copy's terms, when they are not the vocabulary's own. */
  goView?: (go: Items) => Items;
}

const COPIES: Copy[] = [
  {
    id: 'linkTypeRules',
    vocabulary: 'link_rules',
    kind: 'mirror',
    ordered: true,
    where: 'config/linkTypeRules.ts linkTypeRules',
    read: () =>
      objectsOf('config/linkTypeRules.ts', 'linkTypeRules', 'type', {
        label: 'label',
        inverseLabel: 'inverse_label',
        allowedFromTypes: 'allowed_from_types',
        allowedToTypes: 'allowed_to_types',
        description: 'description',
      }),
  },
  {
    id: 'ARTIFACT_TYPES',
    vocabulary: 'artifact_types',
    kind: 'mirror',
    ordered: true,
    where: 'components/wizard/suggestionDrafts.ts ARTIFACT_TYPES',
    read: () => listOf('components/wizard/suggestionDrafts.ts', 'ARTIFACT_TYPES'),
  },
  {
    // The type picker shows the catalogue's own labels.
    id: 'ArtifactEditor type options',
    vocabulary: 'artifact_types',
    kind: 'mirror',
    where: 'components/ArtifactEditor.tsx <select id="type">',
    read: () => selectOptions('components/ArtifactEditor.tsx', 'type'),
  },
  {
    // Per-view labels are UI copy (refactor plan X4 keeps them as named
    // overrides): these maps are compared by key only.
    id: 'GuidedChatPanel TYPE_LABELS',
    vocabulary: 'artifact_types',
    kind: 'mirror',
    where: 'components/wizard/GuidedChatPanel.tsx TYPE_LABELS',
    read: () => keysOf('components/wizard/GuidedChatPanel.tsx', 'TYPE_LABELS'),
  },
  {
    id: 'ImpactView TYPE_LABELS',
    vocabulary: 'artifact_types',
    kind: 'mirror',
    where: 'views/ImpactView.tsx TYPE_LABELS',
    read: () => keysOf('views/ImpactView.tsx', 'TYPE_LABELS'),
  },
  {
    id: 'SharedProjectView TYPE_LABELS',
    vocabulary: 'artifact_types',
    kind: 'mirror',
    where: 'views/SharedProjectView.tsx TYPE_LABELS',
    read: () => keysOf('views/SharedProjectView.tsx', 'TYPE_LABELS'),
  },
  {
    // The open-source page counts some types by name.
    id: 'OpenSourceProjects countsLine',
    vocabulary: 'artifact_types',
    kind: 'uses',
    where: 'site/OpenSourceProjects.tsx countsLine names',
    read: () => keysOf('site/OpenSourceProjects.tsx', 'names'),
  },
  {
    id: 'QUALITY_LINTED_TYPES',
    vocabulary: 'quality_linted_types',
    kind: 'mirror',
    where: 'components/ArtifactDetails.tsx QUALITY_LINTED_TYPES',
    read: () => listOf('components/ArtifactDetails.tsx', 'QUALITY_LINTED_TYPES'),
  },
  {
    id: 'ArtifactStatus',
    vocabulary: 'artifact_statuses',
    kind: 'mirror',
    where: 'api/client.ts type ArtifactStatus',
    read: () => unionOf('api/client.ts', 'ArtifactStatus'),
  },
  {
    id: 'STATUS_META',
    vocabulary: 'artifact_statuses',
    kind: 'mirror',
    where: 'components/ArtifactHeader.tsx STATUS_META',
    read: () => keysOf('components/ArtifactHeader.tsx', 'STATUS_META'),
  },
  {
    id: 'NEXT_STATUSES',
    vocabulary: 'artifact_statuses',
    kind: 'mirror',
    where: 'components/ArtifactHeader.tsx NEXT_STATUSES',
    read: () => keysOf('components/ArtifactHeader.tsx', 'NEXT_STATUSES', 'next'),
  },
  {
    id: 'TRANSITION_LABELS',
    vocabulary: 'artifact_statuses',
    kind: 'mirror',
    where: 'components/ArtifactHeader.tsx TRANSITION_LABELS',
    read: () => keysOf('components/ArtifactHeader.tsx', 'TRANSITION_LABELS'),
  },
  {
    // Every registered feature should gate something in the UI.
    id: 'feature gates',
    vocabulary: 'feature_keys',
    kind: 'mirror',
    where: '*_FEATURE consts, useFeature() arguments and features[...] reads',
    read: featureGates,
  },
  {
    // A project's activity log: every event type but the workspace-level
    // org.* ones, which carry no project and so never reach it.
    id: 'ActivityLog EVENT_TYPES',
    vocabulary: 'event_types',
    kind: 'mirror',
    where: 'views/ActivityLog.tsx EVENT_TYPES',
    read: () => listOf('views/ActivityLog.tsx', 'EVENT_TYPES'),
    goView: (go) => plain([...go.keys()].filter((k) => !k.startsWith('org.'))),
  },
  {
    id: 'AutomationsPage EVENT_TYPES',
    vocabulary: 'event_types',
    kind: 'mirror',
    where: 'views/AutomationsPageEvents.ts EVENT_TYPES',
    read: () => listOf('views/AutomationsPageEvents.ts', 'EVENT_TYPES'),
  },
  {
    id: 'PLAN_LABELS',
    vocabulary: 'plans',
    kind: 'mirror',
    where: 'components/org/OrgBillingTab.tsx PLAN_LABELS',
    read: () => keysOf('components/org/OrgBillingTab.tsx', 'PLAN_LABELS'),
  },
  {
    id: 'PLANS',
    vocabulary: 'plans',
    kind: 'mirror',
    where: 'api/client.ts PLANS',
    read: () => objectsOf('api/client.ts', 'PLANS', 'value'),
  },
  {
    // The server has codes for any client; the frontend branches on some.
    id: 'error code checks',
    vocabulary: 'error_codes',
    kind: 'uses',
    where: 'comparisons and switches on apiErrorCode(...) or a response data .code',
    read: errorCodeChecks,
  },
  {
    id: 'GAP_LABELS',
    vocabulary: 'gap_labels',
    kind: 'mirror',
    where: 'views/vvGapLabels.ts GAP_LABELS',
    read: () => keysOf('views/vvGapLabels.ts', 'GAP_LABELS', 'label'),
  },
  {
    id: 'FALLBACK_PROVIDERS',
    vocabulary: 'providers',
    kind: 'mirror',
    ordered: true,
    where: 'components/agents/AgentEditor.tsx FALLBACK_PROVIDERS',
    read: () => listOf('components/agents/AgentEditor.tsx', 'FALLBACK_PROVIDERS'),
  },
  {
    id: 'providerBadgeColor',
    vocabulary: 'providers',
    kind: 'mirror',
    where: 'views/AgentsPage.tsx providerBadgeColor (case labels)',
    read: () => casesOf('views/AgentsPage.tsx', 'providerBadgeColor'),
  },
  {
    // The CLIs a member signs in to: some providers, by design.
    id: 'CLI_PROVIDERS',
    vocabulary: 'providers',
    kind: 'uses',
    where: 'components/UserSettingsPanel.tsx CLI_PROVIDERS',
    read: () => objectsOf('components/UserSettingsPanel.tsx', 'CLI_PROVIDERS', 'key'),
  },
  {
    id: 'CrewCanvas providerColor',
    vocabulary: 'providers',
    kind: 'uses',
    where: 'components/crews/CrewCanvas.tsx providerColor (compared literals)',
    read: () => comparedIn('components/crews/CrewCanvas.tsx', 'providerColor'),
  },
  {
    // The picker's accept list: every extension that is not an image, plus
    // image/* for the images, which a browser types reliably.
    id: 'ACCEPTED_UPLOAD_TYPES',
    vocabulary: 'upload_extensions',
    kind: 'mirror',
    where: 'components/attachmentKinds.ts ACCEPTED_UPLOAD_TYPES',
    read: () => {
      const init = initializerOf('components/attachmentKinds.ts', 'ACCEPTED_UPLOAD_TYPES');
      const accept = asString(valueOf(init), init).split(',');
      return withAt(init, noDuplicates(init, accept).map((k) => [k, {}]));
    },
    goView: (go) => {
      const keys = [...go].filter(([, i]) => i.fields.kind !== 'image').map(([k]) => k);
      if ([...go.values()].some((i) => i.fields.kind === 'image')) keys.unshift('image/*');
      return plain(keys);
    },
  },
  {
    // isOpenPath's prefixes: a 401 from one never sends the page to /login.
    id: '401 interceptor exemptions',
    vocabulary: 'open_paths',
    kind: 'uses',
    where: "src/api .includes('/api/...') and .startsWith('/api/...')",
    read: interceptorExemptions,
    goView: (go) => plain([...go].filter(([, i]) => i.fields.match === 'prefix').map(([k]) => k)),
  },
  {
    id: 'STEP_LABELS',
    vocabulary: 'wizard_step_labels',
    kind: 'mirror',
    ordered: true,
    where: 'views/GuidedWizard.tsx STEP_LABELS',
    read: () => listOf('views/GuidedWizard.tsx', 'STEP_LABELS'),
  },
];

// ---------------------------------------------------------------------------
// Comparing.
// ---------------------------------------------------------------------------

/**
 * One difference between Go and a copy. side "go": only Go has the item;
 * "ts": only the copy has it; "both": both have it and field differs, or,
 * with item "(order)", they hold their common items in another order.
 */
interface Diff {
  vocabulary: VocabName;
  copy: string;
  item: string;
  side: 'go' | 'ts' | 'both';
  field?: string;
  go?: string | string[];
  ts?: string | string[];
  /** Where the copy names the item, for messages only (not part of the entry). */
  at?: string;
}

const SIDES = ['go', 'ts', 'both'];
const sortedIfList = (v: string | string[]) => (Array.isArray(v) ? [...v].sort() : v);
const same = (a: string | string[], b: string | string[]) => JSON.stringify(sortedIfList(a)) === JSON.stringify(sortedIfList(b));

/** A diff as one canonical line, its keys in a fixed order. */
const keyOf = (d: Diff) =>
  JSON.stringify({ vocabulary: d.vocabulary, copy: d.copy, item: d.item, side: d.side, field: d.field, go: d.go, ts: d.ts });

function compare(copy: Copy, go: Items, mine: Items): Diff[] {
  const out: Diff[] = [];
  const base = { vocabulary: copy.vocabulary, copy: copy.id };
  for (const [key, item] of mine) {
    const want = go.get(key);
    if (!want) {
      out.push({ ...base, item: key, side: 'ts', at: item.at });
      continue;
    }
    for (const [field, value] of Object.entries(item.fields)) {
      const goValue = want.fields[field];
      if (goValue === undefined) throw new Error(`copy ${copy.id} reads a field ${field} that ${copy.vocabulary} does not have`);
      if (!same(goValue, value)) {
        out.push({ ...base, item: key, side: 'both', field, go: sortedIfList(goValue), ts: sortedIfList(value), at: item.at });
      }
    }
  }
  if (copy.kind === 'mirror') {
    for (const key of go.keys()) if (!mine.has(key)) out.push({ ...base, item: key, side: 'go' });
  }
  if (copy.ordered) {
    const goOrder = [...go.keys()].filter((k) => mine.has(k));
    const tsOrder = [...mine.keys()].filter((k) => go.has(k));
    if (JSON.stringify(goOrder) !== JSON.stringify(tsOrder)) {
      out.push({ ...base, item: '(order)', side: 'both', field: 'order', go: goOrder, ts: tsOrder });
    }
  }
  return out;
}

const vocabOrder = (): VocabName[] => Object.keys(JSON.parse(readRepoFile(VOCAB))).filter((k) => k !== 'about') as VocabName[];

function sortDiffs(diffs: Diff[]): Diff[] {
  const vocabs = vocabOrder();
  const copyIndex = (id: string) => COPIES.findIndex((c) => c.id === id);
  return [...diffs].sort(
    (a, b) =>
      vocabs.indexOf(a.vocabulary) - vocabs.indexOf(b.vocabulary) ||
      copyIndex(a.copy) - copyIndex(b.copy) ||
      SIDES.indexOf(a.side) - SIDES.indexOf(b.side) ||
      (a.item < b.item ? -1 : a.item > b.item ? 1 : 0) ||
      (keyOf(a) < keyOf(b) ? -1 : keyOf(a) > keyOf(b) ? 1 : 0)
  );
}

const ALLOWED_ABOUT =
  'Differences between the Go vocabularies (contracts/vocab.json) and their hand-written TypeScript copies that ' +
  'exist today (refactor plan S13; quirk Q6): side "go" is an item only Go has, "ts" one only the copy has, and ' +
  '"both" one whose field differs (with both values), or, as item "(order)", common items held in another order. ' +
  'frontend/src/arch/vocabParity.test.ts fails on a difference not listed here and on a listed one that no longer ' +
  'occurs, so the list only shrinks as drift is fixed. Fixing a drift or allowing a new one is a behavior change ' +
  `with a release note: regenerate with ${ALLOWED_REGENERATE} (only UPDATE_GOLDEN=1 writes; any other value compares).`;

/** The allowed-diffs file: one diff per line, keys in keyOf's order. */
function renderAllowed(diffs: Diff[]): string {
  const lines = sortDiffs(diffs).map((d) => {
    const fields = Object.entries(JSON.parse(keyOf(d)) as Record<string, unknown>);
    return `    { ${fields.map(([k, v]) => `${JSON.stringify(k)}: ${JSON.stringify(v)}`).join(', ')} }`;
  });
  return `{\n  "about": ${JSON.stringify(ALLOWED_ABOUT)},\n  "diffs": [\n${lines.join(',\n')}\n  ]\n}\n`;
}

const describeDiff = (d: Diff, where: string) => {
  const what =
    d.side === 'go'
      ? `${JSON.stringify(d.item)} is in Go only`
      : d.side === 'ts'
        ? `${JSON.stringify(d.item)} is in the copy only`
        : d.item === '(order)'
          ? `the order differs: Go ${JSON.stringify(d.go)}, copy ${JSON.stringify(d.ts)}`
          : `${JSON.stringify(d.item)} ${d.field}: Go ${JSON.stringify(d.go)}, copy ${JSON.stringify(d.ts)}`;
  return `  ${d.vocabulary} · ${d.copy} (${d.at ?? where}): ${what}\n    ${keyOf(d)}`;
};

// ---------------------------------------------------------------------------

// Reading the sources takes a moment; allow for a loaded CI runner.
vi.setConfig({ testTimeout: 60_000 });

describe('Go↔TS vocabularies (refactor plan S13)', () => {
  const vocab = JSON.parse(readRepoFile(VOCAB)) as Vocab;
  const go = goItems(vocab);

  const results = new Map<string, { items: Items; unresolved: string[] }>();
  const resultOf = (copy: Copy) => {
    let r = results.get(copy.id);
    if (!r) {
      const got = copy.read();
      r = got instanceof Map ? { items: got, unresolved: [] } : { items: got.found, unresolved: got.unresolved };
      results.set(copy.id, r);
    }
    return r;
  };
  const actualDiffs = () =>
    COPIES.flatMap((c) => {
      const view = c.goView ? c.goView(go[c.vocabulary]) : go[c.vocabulary];
      return compare(c, view, resultOf(c).items);
    });

  // Only UPDATE_GOLDEN=1 writes the allowed differences; any other value compares.
  beforeAll(() => {
    if (process.env.UPDATE_GOLDEN === '1') fs.writeFileSync(path.join(REPO, ALLOWED), renderAllowed(actualDiffs()));
  });

  it(`reads every vocabulary of ${VOCAB} and every copy (no reader has gone blind)`, () => {
    expect(vocabOrder().sort()).toEqual(Object.keys(go).sort());
    for (const name of Object.keys(go) as VocabName[]) {
      expect(go[name].size, `${VOCAB} ${name} is empty`).toBeGreaterThan(0);
      expect(COPIES.some((c) => c.vocabulary === name), `no TypeScript copy of ${name} is checked`).toBe(true);
    }
    expect(new Set(COPIES.map((c) => c.id)).size, 'two copies share an id').toBe(COPIES.length);
    for (const c of COPIES) {
      expect(resultOf(c).items.size, `${c.id} (${c.where}) reads no items`).toBeGreaterThan(0);
    }
  });

  it('resolves every feature key and error code the frontend uses', () => {
    const unresolved = COPIES.flatMap((c) => resultOf(c).unresolved.map((u) => `${c.id}: ${u}`));
    expect(
      unresolved,
      'name each key or code with a string literal, or a const holding one in this file or one it imports'
    ).toEqual([]);
  });

  it(`differ from Go only as ${ALLOWED} allows`, () => {
    const allowed = new Set((JSON.parse(readRepoFile(ALLOWED)).diffs as Diff[]).map(keyOf));
    const where = new Map(COPIES.map((c) => [c.id, c.where]));
    const fresh = sortDiffs(actualDiffs().filter((d) => !allowed.has(keyOf(d))));
    expect(
      fresh.map((d) => describeDiff(d, where.get(d.copy)!)),
      `a TypeScript copy and the Go catalogue (${VOCAB}, written by TestVocabulary) disagree where ${ALLOWED} ` +
        'allows no difference. Make the copy agree with Go, or Go with the copy (then ' +
        `${GO_REGENERATE}). A difference that is meant is a behavior change with a release note: ` +
        `list it in ${ALLOWED} (${ALLOWED_REGENERATE})`
    ).toEqual([]);
  });

  it(`still show every difference ${ALLOWED} allows (the list only shrinks)`, () => {
    const actual = new Set(actualDiffs().map(keyOf));
    const listed = JSON.parse(readRepoFile(ALLOWED)).diffs as Diff[];
    const gone = listed.map(keyOf).filter((k) => !actual.has(k));
    expect(
      gone,
      `these differences in ${ALLOWED} do not occur: a drift was fixed, or a copy renamed. Remove the entries ` +
        `(${ALLOWED_REGENERATE}); fixing a drift is a behavior change with a release note`
    ).toEqual([]);
  });
});
