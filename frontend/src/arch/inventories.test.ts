// Regenerate the snapshots (only in a PR that means to change a key or param):
//   npx vitest run src/arch -u
//
// Two inventories built from the TypeScript AST of every production file,
// with the type checker resolving each key to its value (a constant imported
// from another module included, so SHORTCUT_PARAM reads as "go"):
//   storageKeys.txt  every getItem/setItem/removeItem on localStorage or
//                    sessionStorage, plus public/theme-init.js (invariant I22)
//   queryParams.txt  every get/getAll/has/set/append/delete on a
//                    URLSearchParams, the keys of object-form
//                    setSearchParams calls, and the parameters that in-app
//                    links write into their URLs (invariant I19)
// A renamed key or parameter changes a snapshot: persisted preferences and
// emailed or bookmarked links depend on the old names. The rows hold names and
// operations only, never the file a use sits in, so a pure move (F1's
// api/http.ts, F3's state/activeOrgStorage.ts, F6's tab files) leaves both
// snapshots unchanged; the files appear in the failure messages instead.
// A storage key built from a parameter resolves through the calls of the
// function that declares the parameter, climbing callers and following
// object-pattern parameters, so that moving a storage call into a hook (X16a's
// usePanelMode and useColumnResize) leaves storageKeys.txt as it was; the
// in-memory fixtures at the end try those shapes before the production tree
// has them.
import path from 'node:path';
import ts from 'typescript';
import { beforeAll, describe, expect, it, vi } from 'vitest';
import { loadDeepLinks } from './appRoutes';
import { FRONTEND, REGENERATE, SRC, literalText, parseFile, productionSources, srcRel, tsProgram, walk } from './repo';

let program: ts.Program;
let checker: ts.TypeChecker;
let sources: ts.SourceFile[];
let inventory: Inventory;

beforeAll(() => {
  const files = productionSources();
  program = tsProgram(files);
  checker = program.getTypeChecker();
  sources = files.map((f) => program.getSourceFile(f)!);
  inventory = inventoryOf(checker, sources);
}, 120_000);

const typeName = (e: ts.Expression) => checker.typeToString(checker.getTypeAtLocation(e));

/** A parameter's value at one call: the key its argument resolves to, and the parameters that key still reads unbound. */
interface Bound {
  key: string;
  free: readonly ts.Node[];
}

/**
 * Parameter declarations, and the names an object-pattern parameter
 * destructures ({ storageKey }), bound to the value one call passes for them.
 */
type Env = Map<ts.Node, Bound>;

/** Where a value comes from: an expression, and the bindings it is read with; null when the code does not say. */
type Value = { expr: ts.Expression; env: Env } | null;

/** A call of fn in file. A chain of them, innermost first, is the context a storage call runs in. */
interface Call {
  fn: ts.SignatureDeclaration;
  call: ts.CallExpression;
  file: string;
}

/** The most levels of callers a key climbs (ModuleView → usePanelMode → loadPanelMode is two). */
const CALLER_LEVELS = 4;

/** The parameter decl is, or the one an object pattern destructures it from; null for anything else. */
function parameterOf(decl: ts.Node): ts.ParameterDeclaration | null {
  let node = decl;
  while (ts.isBindingElement(node) || ts.isObjectBindingPattern(node)) node = node.parent;
  return ts.isParameter(node) ? node : null;
}

/** A property's name as written, or null for a computed name or a pattern. */
function nameText(name: ts.PropertyName | ts.BindingName): string | null {
  return ts.isIdentifier(name) || ts.isStringLiteral(name) || ts.isNumericLiteral(name) ? name.text : null;
}

/** Of the functions declaring these parameters, the most deeply nested: the one a climb binds first. */
function innermostOwner(params: Iterable<ts.Node>): ts.SignatureDeclaration | undefined {
  let owner: ts.SignatureDeclaration | undefined;
  let deepest = -1;
  for (const p of params) {
    const fn = parameterOf(p)!.parent;
    let depth = 0;
    for (let n: ts.Node | undefined = fn; n; n = n.parent) depth++;
    if (depth > deepest) [owner, deepest] = [fn, depth];
  }
  return owner;
}

/**
 * The key resolvers over one type-checked program: the production tree (set
 * up in beforeAll), or the in-memory fixtures at the end of this file.
 */
function inventoryOf(checker: ts.TypeChecker, sources: readonly ts.SourceFile[]) {
  const typeName = (e: ts.Expression) => checker.typeToString(checker.getTypeAtLocation(e));

  /** The symbol a name refers to, through imports; for a shorthand property ({ storageKey }), the value it reads. */
  const symbolOf = (e: ts.Node) => {
    const sym =
      ts.isShorthandPropertyAssignment(e.parent) && e.parent.name === e
        ? checker.getShorthandAssignmentValueSymbol(e.parent)
        : checker.getSymbolAtLocation(e);
    return sym && sym.flags & ts.SymbolFlags.Alias ? checker.getAliasedSymbol(sym) : sym;
  };

  /**
   * What string an expression evaluates to, as far as the code says: a literal,
   * a const (followed across imports), a template whose unknown parts read as
   * <expression>, or a call to a one-expression function with its parameters
   * bound to the call's arguments. A parameter, or a name an object-pattern
   * parameter destructures, reads as its value in env, else as <parameter>
   * and joins free. Anything else is <dynamic: expression>.
   */
  function resolveKey(expr: ts.Expression, depth = 0, env: Env = new Map(), free?: Set<ts.Node>): string {
    const lit = literalText(expr);
    if (lit !== null) return lit;
    if (depth > 5) return `<dynamic: ${expr.getText()}>`;
    const type = checker.getTypeAtLocation(expr);
    if (type.isStringLiteral()) return type.value;
    if (ts.isParenthesizedExpression(expr)) return resolveKey(expr.expression, depth + 1, env, free);
    if (ts.isTemplateExpression(expr)) {
      return (
        expr.head.text +
        expr.templateSpans
          .map((s) => {
            const inner = resolveKey(s.expression, depth + 1, env, free);
            return (inner.startsWith('<dynamic: ') ? `<${s.expression.getText()}>` : inner) + s.literal.text;
          })
          .join('')
      );
    }
    if (ts.isIdentifier(expr) || ts.isPropertyAccessExpression(expr)) {
      const decl = symbolOf(expr)?.valueDeclaration;
      if (decl && parameterOf(decl)) {
        const bound = env.get(decl);
        if (bound) {
          bound.free.forEach((p) => free?.add(p));
          return bound.key;
        }
        free?.add(decl);
        return `<${(decl as ts.ParameterDeclaration | ts.BindingElement).name.getText()}>`;
      }
      if (decl && ts.isVariableDeclaration(decl) && decl.initializer && ts.getCombinedNodeFlags(decl) & ts.NodeFlags.Const) {
        return resolveKey(decl.initializer, depth + 1, env, free);
      }
    }
    if (ts.isCallExpression(expr)) {
      const decl = symbolOf(expr.expression)?.valueDeclaration;
      const fn = decl && ts.isVariableDeclaration(decl) ? decl.initializer : decl;
      if (fn && (ts.isArrowFunction(fn) || ts.isFunctionExpression(fn) || ts.isFunctionDeclaration(fn)) && fn.body) {
        let body: ts.Node = fn.body;
        if (ts.isBlock(body) && body.statements.length === 1 && ts.isReturnStatement(body.statements[0])) {
          body = body.statements[0].expression ?? body;
        }
        if (!ts.isBlock(body)) {
          return resolveKey(body as ts.Expression, depth + 1, bind(fn, expr.arguments, env, depth + 1), free);
        }
      }
    }
    return `<dynamic: ${expr.getText().replace(/\s+/g, ' ')}>`;
  }

  const boundTo = (value: NonNullable<Value>, depth: number): Bound => {
    const free = new Set<ts.Node>();
    return { key: resolveKey(value.expr, depth, value.env, free), free: [...free] };
  };

  /** env plus fn's parameters bound to the arguments of one call of fn, or to a parameter's default where it passes none. */
  function bind(fn: ts.SignatureDeclaration, args: ts.NodeArray<ts.Expression>, env: Env, depth: number): Env {
    const bound: Env = new Map(env);
    fn.parameters.forEach((p, i) => {
      // An argument reads the caller's names; a default, the parameters before it.
      const value: Value = args[i] ? { expr: args[i], env } : p.initializer ? { expr: p.initializer, env: bound } : null;
      if (ts.isObjectBindingPattern(p.name)) bindPattern(p.name, value, bound, depth);
      else if (value) bound.set(p, boundTo(value, depth));
    });
    return bound;
  }

  /**
   * The names an object pattern destructures, bound from the object literal
   * passed for it: { storageKey } and { storageKey: key } read its storageKey
   * property, { storageKey = 'k' } falls back to 'k' when the literal has
   * none, and a nested pattern reads the nested literal. A name stays unbound,
   * and reads as <name>, where the code does not show the value.
   */
  function bindPattern(pattern: ts.ObjectBindingPattern, source: Value, bound: Env, depth: number) {
    for (const el of pattern.elements) {
      const name = el.dotDotDotToken ? null : nameText(el.propertyName ?? el.name);
      let value: Value = null;
      if (source && name !== null) {
        const found = propertyOf(source.expr, name, depth);
        if (found) value = { expr: found, env: source.env };
        else if (found === null && el.initializer) value = { expr: el.initializer, env: bound };
      }
      if (ts.isObjectBindingPattern(el.name)) bindPattern(el.name, value, bound, depth);
      else if (value && ts.isIdentifier(el.name)) bound.set(el, boundTo(value, depth));
    }
  }

  /**
   * The expression an object literal gives property name (its last property
   * of that name, a spread literal searched in turn, a const followed to its
   * literal); null when the literal has no such property; undefined when the
   * code does not show (not a literal, a computed name, a spread of anything
   * else).
   */
  function propertyOf(expr: ts.Expression, name: string, depth: number): ts.Expression | null | undefined {
    if (depth > 5) return undefined;
    if (ts.isParenthesizedExpression(expr) || ts.isAsExpression(expr) || ts.isSatisfiesExpression(expr)) {
      return propertyOf(expr.expression, name, depth + 1);
    }
    if (ts.isIdentifier(expr)) {
      const decl = symbolOf(expr)?.valueDeclaration;
      const isConst = decl && ts.isVariableDeclaration(decl) && ts.getCombinedNodeFlags(decl) & ts.NodeFlags.Const;
      return isConst && decl.initializer ? propertyOf(decl.initializer, name, depth + 1) : undefined;
    }
    if (!ts.isObjectLiteralExpression(expr)) return undefined;
    for (const prop of [...expr.properties].reverse()) {
      if (ts.isSpreadAssignment(prop)) {
        const inner = propertyOf(prop.expression, name, depth + 1);
        if (inner !== null) return inner;
        continue;
      }
      const text = nameText(prop.name);
      if (text === null) return undefined;
      if (text !== name) continue;
      if (ts.isPropertyAssignment(prop)) return prop.initializer;
      return ts.isShorthandPropertyAssignment(prop) ? prop.name : undefined;
    }
    return null;
  }

  const callsByFn = new Map<ts.Node, Call[]>();

  /** Every call of fn in sources, in source order; none when fn has no name to be called by. */
  function callsOf(fn: ts.SignatureDeclaration): Call[] {
    const cached = callsByFn.get(fn);
    if (cached) return cached;
    const calls: Call[] = [];
    const nameNode = ts.isVariableDeclaration(fn.parent) ? fn.parent.name : fn.name;
    const target = nameNode && symbolOf(nameNode);
    if (target) {
      for (const sf of sources) {
        walk(sf, (node) => {
          if (ts.isCallExpression(node) && symbolOf(node.expression) === target) {
            calls.push({ fn, call: node, file: srcRel(sf.fileName) });
          }
        });
      }
    }
    callsByFn.set(fn, calls);
    return calls;
  }

  /**
   * A key built from a parameter (loadPanelMode(panel) reads
   * `openv-panel-mode-${panel}`) resolves once per call of the function that
   * declares the parameter, with the call's arguments bound: a renamed panel
   * id is a renamed key. Where an argument is itself a parameter of the
   * function around the call (usePanelMode(panel) calls loadPanelMode(panel)),
   * the key climbs that function's calls in turn, innermost function first,
   * up to CALLER_LEVELS levels and never into a function already on the chain:
   * a recursive call stays unresolved. `via` names the file of the outermost
   * call.
   */
  function resolveThroughCallers(arg: ts.Expression): { key: string; via?: string }[] {
    const out: { key: string; via?: string }[] = [];
    const climb = (chain: Call[]) => {
      // The outermost call binds first: an inner call's arguments read its parameters.
      let env: Env = new Map();
      for (let i = chain.length - 1; i >= 0; i--) env = bind(chain[i].fn, chain[i].call.arguments, env, 0);
      const free = new Set<ts.Node>();
      const key = resolveKey(arg, 0, env, free);
      const fn = key.includes('<') && chain.length < CALLER_LEVELS ? innermostOwner(free) : undefined;
      const calls = fn && !chain.some((c) => c.fn === fn) ? callsOf(fn) : [];
      if (calls.length === 0) out.push({ key, via: chain[chain.length - 1]?.file });
      for (const call of calls) climb([...chain, call]);
    };
    climb([]);
    return out;
  }

  /** Every getItem/setItem/removeItem/clear/key call on a Storage in sources. */
  function storageCalls(): StorageUse[] {
    const uses: StorageUse[] = [];
    for (const sf of sources) {
      walk(sf, (node) => {
        if (!ts.isCallExpression(node)) return;
        const access = receiverOf(node);
        if (!access || !STORAGE_METHODS.has(access.name.text) || typeName(access.expression) !== 'Storage') return;
        const arg = node.arguments[0];
        const keys = access.name.text === 'clear' ? [{ key: '(every key)' }] : arg ? resolveThroughCallers(arg) : [{ key: '(no key)' }];
        for (const { key, via } of keys) {
          uses.push({
            key,
            storage: access.expression.getText().replace(/^window\./, ''),
            op: access.name.text,
            file: srcRel(sf.fileName) + (via ? ` (via ${via})` : ''),
          });
        }
      });
    }
    return uses;
  }

  return { resolveKey, storageCalls };
}

type Inventory = ReturnType<typeof inventoryOf>;

const receiverOf = (call: ts.CallExpression) =>
  ts.isPropertyAccessExpression(call.expression) ? call.expression : null;

// ---------------------------------------------------------------- storage

const STORAGE_METHODS = new Set(['getItem', 'setItem', 'removeItem', 'clear', 'key']);
const THEME_INIT = '../public/theme-init.js';

interface StorageUse {
  key: string;
  storage: string;
  op: string;
  file: string;
}

function collectStorageUses(): StorageUse[] {
  const uses = inventory.storageCalls();
  // The pre-paint theme script is plain JS served from public/ (no program).
  const initScript = path.join(FRONTEND, 'public/theme-init.js');
  walk(parseFile(initScript), (node) => {
    if (!ts.isCallExpression(node)) return;
    const access = receiverOf(node);
    if (!access || !STORAGE_METHODS.has(access.name.text)) return;
    const storage = access.expression.getText().replace(/^window\./, '');
    if (storage !== 'localStorage' && storage !== 'sessionStorage') return;
    uses.push({
      key: literalText(node.arguments[0]) ?? `<dynamic: ${node.arguments[0]?.getText()}>`,
      storage,
      op: access.name.text,
      file: THEME_INIT,
    });
  });
  return uses;
}

// ------------------------------------------------------------ query params

const PARAM_METHODS = new Set(['get', 'getAll', 'has', 'set', 'append', 'delete']);
const READS = new Set(['get', 'getAll', 'has']);

interface ParamUse {
  name: string;
  op: string;
  file: string;
  scope: 'page' | 'api';
}

/** Page code reads the browser URL through react-router's useSearchParams. */
const isPageModule = (sf: ts.SourceFile) => /\buseSearchParams\b/.test(sf.text);

function collectParamUses(): ParamUse[] {
  const uses: ParamUse[] = [];
  for (const sf of sources) {
    const scope = isPageModule(sf) ? 'page' : 'api';
    const file = srcRel(sf.fileName);
    walk(sf, (node) => {
      if (!ts.isCallExpression(node) && !ts.isNewExpression(node)) return;
      if (ts.isCallExpression(node)) {
        const access = receiverOf(node);
        if (access && PARAM_METHODS.has(access.name.text) && typeName(access.expression) === 'URLSearchParams') {
          const arg = node.arguments[0];
          uses.push({ name: arg ? inventory.resolveKey(arg) : '(none)', op: access.name.text, file, scope });
          return;
        }
      }
      // setSearchParams({ tab: x }) and new URLSearchParams({ a: b }).
      const callee = node.expression;
      const objectForm =
        (ts.isCallExpression(node) && typeName(callee) === 'SetURLSearchParams') ||
        (ts.isNewExpression(node) && callee.getText() === 'URLSearchParams');
      const first = node.arguments?.[0];
      if (objectForm && first && ts.isObjectLiteralExpression(first)) {
        for (const prop of first.properties) {
          const name = prop.name && (ts.isIdentifier(prop.name) || ts.isStringLiteral(prop.name)) ? prop.name.text : '<computed>';
          uses.push({ name, op: 'set (object)', file, scope });
        }
      }
    });
  }
  return uses;
}

interface LinkParam {
  name: string;
  /** The link's path and this parameter with its value, {} where computed. */
  link: string;
  file: string;
}

/**
 * A template's text with {} for each computed part, except that a query-string
 * fragment (`?run=${id}`, possibly behind a conditional) is spliced in as text.
 */
function templateText(node: ts.TemplateExpression): string {
  return node.head.text + node.templateSpans.map((s) => `${queryFragment(s.expression) ?? '{}'}${s.literal.text}`).join('');
}

function queryFragment(e: ts.Expression): string | null {
  if (ts.isParenthesizedExpression(e)) return queryFragment(e.expression);
  if (ts.isConditionalExpression(e)) return queryFragment(e.whenTrue) ?? queryFragment(e.whenFalse);
  const text = literalText(e) ?? (ts.isTemplateExpression(e) ? templateText(e) : null);
  return text !== null && /^[?&][A-Za-z_]+=/.test(text) ? text : null;
}

/**
 * Parameters written into in-app links: '/login?mode=register', `/x?run=${id}`,
 * and `/x${id ? `?run=${id}` : ''}`.
 */
function collectLinkParams(): LinkParam[] {
  const out: LinkParam[] = [];
  for (const sf of sources) {
    walk(sf, (node) => {
      let text: string | null = literalText(node);
      if (text === null && ts.isTemplateExpression(node)) text = templateText(node);
      if (text === null || !/^\/(?!api\/)[^\s?]*\?[A-Za-z_]+=/.test(text)) return;
      const [p, query] = text.split('?');
      for (const pair of query.split('&')) {
        const name = pair.split('=')[0];
        if (name) out.push({ name, link: `${p}?${pair}`, file: srcRel(sf.fileName) });
      }
    });
  }
  return out;
}

// ------------------------------------------------------------------- output

/** Run a collector once (after beforeAll has built the program). */
const once = <T,>(collect: () => T) => {
  let value: T | undefined;
  return () => (value ??= collect());
};
const storageUses = once(collectStorageUses);
const paramUses = once(collectParamUses);
const linkParams = once(collectLinkParams);

const uniq = (rows: string[]) => [...new Set(rows)].sort();

function groupBy<T>(items: T[], key: (t: T) => string, row: (t: T) => string): string[] {
  const names = uniq(items.map(key));
  return names.flatMap((name) => [name, ...uniq(items.filter((i) => key(i) === name).map(row)).map((r) => `  ${r}`)]);
}

// Parsing and type-checking the source tree takes seconds; allow for a loaded CI runner.
vi.setConfig({ testTimeout: 60_000 });

/** Storage and operation; public/theme-init.js runs before the app, so its uses stay marked. */
const storageRow = (u: StorageUse) =>
  `${u.storage.padEnd(15)} ${u.op.padEnd(10)} ${u.file === THEME_INIT ? '(public/theme-init.js)' : ''}`.trimEnd();

describe('browser storage keys', () => {
  it('matches the pinned inventory', async () => {
    const uses = storageUses();
    const keys = uniq(uses.map((u) => u.key));
    const text = [
      '# Browser storage keys: every getItem/setItem/removeItem on localStorage or sessionStorage (invariant I22).',
      `# Regenerate: ${REGENERATE}. Never rename a key: it holds members' preferences and workspace choice.`,
      '',
      `keys: ${keys.length}`,
      '',
      ...groupBy(uses, (u) => u.key, storageRow),
    ].join('\n');
    await expect(text + '\n').toMatchFileSnapshot('./__snapshots__/storageKeys.txt');
  });

  it('resolves every key to a name', () => {
    expect(storageUses().filter((u) => u.key.includes('<'))).toEqual([]);
  });

  it('reads the active workspace from both storages and writes both', () => {
    const org = storageUses().filter((u) => u.key === 'openv_active_org');
    const has = (storage: string, op: string) => org.some((u) => u.storage === storage && u.op === op);
    expect([has('sessionStorage', 'getItem'), has('localStorage', 'getItem')]).toEqual([true, true]);
    expect([has('sessionStorage', 'setItem'), has('localStorage', 'setItem')]).toEqual([true, true]);
  });
});

describe('query parameters', () => {
  it('matches the pinned inventory', async () => {
    const uses = paramUses();
    const links = linkParams();
    const page = uses.filter((u) => u.scope === 'page');
    const api = uses.filter((u) => u.scope === 'api');
    const read = uniq(page.filter((u) => READS.has(u.op)).map((u) => u.name));
    const text = [
      '# Query parameters (invariant I19): the URL parameters pages read and write, the ones in-app links carry,',
      '# and the query strings built with URLSearchParams for API calls.',
      `# Regenerate: ${REGENERATE}. Emails, pushes, Stripe returns, shortcuts and bookmarks carry these names.`,
      '',
      `page parameters read: ${read.length} (${read.join(', ')})`,
      `page parameters written: ${uniq(page.filter((u) => !READS.has(u.op)).map((u) => u.name)).length}`,
      `parameters carried by in-app links: ${uniq(links.map((l) => l.name)).length}`,
      `API query parameters built with URLSearchParams: ${uniq(api.map((u) => u.name)).length}`,
      '',
      '## Page URL parameters (modules using useSearchParams)',
      ...groupBy(page, (u) => u.name, (u) => u.op),
      '',
      '## Parameters written into in-app links',
      ...groupBy(links, (l) => l.name, (l) => l.link),
      '',
      '## API query strings built with URLSearchParams',
      ...groupBy(api, (u) => u.name, (u) => u.op),
    ].join('\n');
    await expect(text + '\n').toMatchFileSnapshot('./__snapshots__/queryParams.txt');
  });

  it('resolves SHORTCUT_PARAM to "go"', () => {
    expect(paramUses().some((u) => u.name === 'go' && u.op === 'get' && u.scope === 'page')).toBe(true);
  });

  it('reads every parameter an in-app link or a backend deep link carries', () => {
    const read = new Set(paramUses().filter((u) => u.scope === 'page' && READS.has(u.op)).map((u) => u.name));
    const backend = loadDeepLinks().flatMap((l) => [...new URL(l.url, 'http://openv.test').searchParams.keys()]);
    const carried = uniq([...linkParams().map((l) => l.name), ...backend]);
    expect(carried.filter((name) => !read.has(name))).toEqual([]);
  });
});

// -------------------------------------------------------- resolver fixtures

/** Where the in-memory fixture modules sit; nothing is on disk there. */
const FIXTURE_DIR = path.join(SRC, 'arch', 'fixture');

/**
 * A type-checked program over in-memory modules (a path under FIXTURE_DIR and
 * its source) with the app's compiler options, less its ambient types. A
 * fixture that does not compile throws, so a typo cannot pass for a case.
 */
function fixtureProgram(files: Record<string, string | string[]>): { checker: ts.TypeChecker; sources: ts.SourceFile[] } {
  const config = ts.readConfigFile(path.join(FRONTEND, 'tsconfig.json'), ts.sys.readFile).config;
  const options = { ...ts.parseJsonConfigFileContent(config, ts.sys, FRONTEND).options, types: [] };
  const virtual = new Map(Object.entries(files).map(([name, text]) => [path.join(FIXTURE_DIR, name), [text].flat().join('\n')]));
  const host = ts.createCompilerHost(options, true);
  const { getSourceFile, fileExists, readFile, directoryExists } = host;
  host.getSourceFile = (name, target, ...rest) => {
    const text = virtual.get(name);
    return text === undefined ? getSourceFile.call(host, name, target, ...rest) : ts.createSourceFile(name, text, target, true);
  };
  host.fileExists = (name) => virtual.has(name) || fileExists.call(host, name);
  host.readFile = (name) => virtual.get(name) ?? readFile.call(host, name);
  host.directoryExists = (dir) =>
    [...virtual.keys()].some((f) => f.startsWith(dir + path.sep)) || (directoryExists?.call(host, dir) ?? true);
  const roots = [...virtual.keys()];
  const fixture = ts.createProgram(roots, options, host);
  const fixtureSources = roots.map((f) => fixture.getSourceFile(f)!);
  const errors = fixtureSources.flatMap((sf) => [...fixture.getSyntacticDiagnostics(sf), ...fixture.getSemanticDiagnostics(sf)]);
  if (errors.length) throw new Error(ts.formatDiagnostics(errors, host));
  return { checker: fixture.getTypeChecker(), sources: fixtureSources };
}

// Shapes the production tree does not have yet, one directory each.
const FIXTURES: Record<string, string | string[]> = {
  // A calls useX('left'), useX(panel) calls load(panel), load(p) reads `k-${p}`.
  'chain2/load.ts': 'export function load(p: string) { return localStorage.getItem(`k-${p}`); }',
  'chain2/useX.ts': "import { load } from './load'; export function useX(panel: string) { return load(panel); }",
  'chain2/A.ts': "import { useX } from './useX'; export const A = () => useX('left');",

  'chain3/load.ts': "export const load = (p: string) => localStorage.setItem(`k3-${p}`, '1');",
  'chain3/hooks.ts': [
    "import { load } from './load';",
    'export const useInner = (side: string) => load(side);',
    'export const useOuter = (where: string) => useInner(where);',
  ],
  'chain3/B.ts': "import { useOuter } from './hooks'; export const B = () => useOuter('right');",

  'sites/save.ts': [
    'export const save = (p: string) => localStorage.removeItem(`s-${p}`);',
    'export const useSave = (side: string) => save(side);',
  ],
  'sites/left.ts': "import { useSave } from './save'; export const L = () => useSave('left');",
  'sites/right.ts': "import { useSave } from './save'; export const R = () => useSave('right');",

  'pattern/cols.ts': [
    'export function useCols({ storageKey }: { storageKey: string }) { return localStorage.getItem(storageKey); }',
    'export function useRenamed({ storageKey: key }: { storageKey: string }) { return localStorage.getItem(`renamed-${key}`); }',
    "export function useDefault({ storageKey = 'default' }: { storageKey?: string } = {}) { return localStorage.getItem(storageKey); }",
    'export function useNested({ keys: { left } }: { keys: { left: string } }) { return localStorage.getItem(left); }',
  ],
  'pattern/view.ts': [
    "import { useCols, useDefault, useNested, useRenamed } from './cols';",
    "const storageKey = 'shorthand';",
    "const options = { storageKey: 'const-object' };",
    "const spread = { storageKey: 'spread' };",
    'export function View() {',
    "  useCols({ storageKey: 'property' });",
    '  useCols({ storageKey });',
    '  useCols(options);',
    '  useCols({ ...spread });',
    "  useCols({ ...spread, storageKey: 'after-spread' });",
    "  useRenamed({ storageKey: 'x' });",
    '  useDefault({});',
    '  useDefault();',
    "  useDefault({ storageKey: 'given' });",
    "  useNested({ keys: { left: 'nested' } });",
    '}',
  ],

  'recursion/walk.ts': [
    'export function walkKeys(p: string): void {',
    '  localStorage.removeItem(`w-${p}`);',
    '  if (p.length < 9) walkKeys(`${p}/x`);',
    '}',
    "export const start = () => walkKeys('root');",
    'export function ping(a: string): string | null { return localStorage.getItem(`p-${a}`) ?? pong(a); }',
    'export function pong(b: string): string | null { return ping(b); }',
  ],

  // A closure reads its own parameter and its hook's: the inner function binds first.
  'closure/panel.ts': [
    'export function useOuter(a: string) {',
    '  const inner = (b: string) => localStorage.getItem(`n-${a}-${b}`);',
    "  return [inner('x'), inner(a)];",
    '}',
    "export const N = () => useOuter('y');",
  ],

  'dynamic/store.ts': [
    'export function orphan(name: string) { return localStorage.getItem(`o-${name}`); }',
    'export function fromInput(input: HTMLInputElement) { return localStorage.getItem(input.value); }',
    'export function passThrough(k: string) { return localStorage.getItem(k); }',
    'export const caller = () => passThrough(String(Date.now()));',
  ],

  // ModuleView as X16a will split it: the storage calls one or two levels
  // further down, inside closures, and a column's key passed in an options object.
  'x16a/react.ts': [
    'export const useState = <T>(init: () => T): [T, (next: T) => void] => [init(), () => undefined];',
    'export const useCallback = <T>(fn: T, deps: unknown[]): T => fn;',
  ],
  'x16a/panelMode.ts': [
    'const storageKey = (panel: string) => `openv-panel-mode-${panel}`;',
    "export const loadPanelMode = (panel: string, fallback = 'pinned'): string => {",
    '  try {',
    '    return localStorage.getItem(storageKey(panel)) ?? fallback;',
    '  } catch {',
    '    return fallback;',
    '  }',
    '};',
    'export const savePanelMode = (panel: string, mode: string): void => {',
    '  try {',
    '    localStorage.setItem(storageKey(panel), mode);',
    '  } catch {',
    '    /* a preference that cannot be stored */',
    '  }',
    '};',
  ],
  'x16a/usePanelMode.ts': [
    "import { loadPanelMode, savePanelMode } from './panelMode';",
    "import { useCallback, useState } from './react';",
    'export function usePanelMode(panel: string, fallback: string) {',
    '  const [mode, setMode] = useState(() => loadPanelMode(panel, fallback));',
    '  const cycle = useCallback((next: string) => {',
    '    setMode(next);',
    '    savePanelMode(panel, next);',
    '  }, [panel]);',
    '  return [mode, cycle] as const;',
    '}',
  ],
  'x16a/useColumnResize.ts': [
    "import { useState } from './react';",
    'export function useColumnResize({ storageKey, initial }: { storageKey: string; initial: number }) {',
    '  const [width] = useState(() => {',
    '    const saved = localStorage.getItem(storageKey);',
    '    return saved ? parseInt(saved) : initial;',
    '  });',
    '  const onMouseUp = () => localStorage.setItem(storageKey, width.toString());',
    '  return { width, onMouseUp };',
    '}',
  ],
  'x16a/ModuleView.ts': [
    "import { useColumnResize } from './useColumnResize';",
    "import { usePanelMode } from './usePanelMode';",
    'export function ModuleView() {',
    "  const [notesMode, cycleNotesMode] = usePanelMode('artifact-notes', window.innerWidth < 1200 ? 'autohide' : 'pinned');",
    "  const left = useColumnResize({ storageKey: 'openv-leftColumnWidth', initial: 400 });",
    "  const right = useColumnResize({ storageKey: 'openv-rightColumnWidth', initial: 320 });",
    '  return { notesMode, cycleNotesMode, left, right };',
    '}',
  ],
  'x16a/ProjectLayout.ts': [
    "import { loadPanelMode, savePanelMode } from './panelMode';",
    "export const navMode = () => loadPanelMode('project-nav');",
    "export const cycleNav = (next: string) => savePanelMode('project-nav', next);",
  ],
};

const fixtureUses = once(() => {
  const fixture = fixtureProgram(FIXTURES);
  return inventoryOf(fixture.checker, fixture.sources).storageCalls();
});

/** The fixture's storage calls in one directory, as `key op file (via file)`, paths relative to FIXTURE_DIR. */
function fixtureRows(dir: string): string[] {
  const prefix = `${srcRel(FIXTURE_DIR)}/`;
  return uniq(
    fixtureUses()
      .filter((u) => u.file.startsWith(`${prefix}${dir}/`))
      .map((u) => `${u.key} ${u.op} ${u.file.split(prefix).join('')}`)
  );
}

describe('a storage key built from a parameter (in-memory fixtures)', () => {
  it('resolves through two levels of callers, via the outermost', () => {
    expect(fixtureRows('chain2')).toEqual(['k-left getItem chain2/load.ts (via chain2/A.ts)']);
  });

  it('resolves through three levels of callers', () => {
    expect(fixtureRows('chain3')).toEqual(['k3-right setItem chain3/load.ts (via chain3/B.ts)']);
  });

  it('resolves once per call site', () => {
    expect(fixtureRows('sites')).toEqual([
      's-left removeItem sites/save.ts (via sites/left.ts)',
      's-right removeItem sites/save.ts (via sites/right.ts)',
    ]);
  });

  it('follows object-pattern parameters: property, renamed, shorthand, const, spread, default, nested', () => {
    const via = 'pattern/cols.ts (via pattern/view.ts)';
    expect(fixtureRows('pattern')).toEqual(
      ['after-spread', 'const-object', 'default', 'given', 'nested', 'property', 'renamed-x', 'shorthand', 'spread'].map(
        (key) => `${key} getItem ${via}`
      )
    );
  });

  it('binds the inner function first when a key reads the parameters of two nested functions', () => {
    expect(fixtureRows('closure')).toEqual([
      'n-y-x getItem closure/panel.ts (via closure/panel.ts)',
      'n-y-y getItem closure/panel.ts (via closure/panel.ts)',
    ]);
  });

  it('stops at a recursive call, its key unresolved', () => {
    expect(fixtureRows('recursion')).toEqual([
      'p-<a> getItem recursion/walk.ts (via recursion/walk.ts)',
      'w-<p>/x removeItem recursion/walk.ts (via recursion/walk.ts)',
      'w-root removeItem recursion/walk.ts (via recursion/walk.ts)',
    ]);
  });

  it('stays unresolved where the code does not say, so "resolves every key to a name" fails', () => {
    expect(fixtureRows('dynamic')).toEqual([
      '<dynamic: String(Date.now())> getItem dynamic/store.ts (via dynamic/store.ts)',
      '<dynamic: input.value> getItem dynamic/store.ts',
      'o-<name> getItem dynamic/store.ts',
    ]);
    expect(fixtureUses().filter((u) => u.file.includes('/dynamic/')).every((u) => u.key.includes('<'))).toBe(true);
  });

  it("keeps the names of ModuleView's keys through X16a's hooks", () => {
    expect(fixtureRows('x16a')).toEqual([
      'openv-leftColumnWidth getItem x16a/useColumnResize.ts (via x16a/ModuleView.ts)',
      'openv-leftColumnWidth setItem x16a/useColumnResize.ts (via x16a/ModuleView.ts)',
      'openv-panel-mode-artifact-notes getItem x16a/panelMode.ts (via x16a/ModuleView.ts)',
      'openv-panel-mode-artifact-notes setItem x16a/panelMode.ts (via x16a/ModuleView.ts)',
      'openv-panel-mode-project-nav getItem x16a/panelMode.ts (via x16a/ProjectLayout.ts)',
      'openv-panel-mode-project-nav setItem x16a/panelMode.ts (via x16a/ProjectLayout.ts)',
      'openv-rightColumnWidth getItem x16a/useColumnResize.ts (via x16a/ModuleView.ts)',
      'openv-rightColumnWidth setItem x16a/useColumnResize.ts (via x16a/ModuleView.ts)',
    ]);
  });
});
