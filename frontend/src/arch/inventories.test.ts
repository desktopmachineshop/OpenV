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
// emailed or bookmarked links depend on the old names.
import path from 'node:path';
import ts from 'typescript';
import { beforeAll, describe, expect, it, vi } from 'vitest';
import { loadDeepLinks } from './appRoutes';
import { FRONTEND, REGENERATE, literalText, parseFile, productionSources, srcRel, tsProgram, walk } from './repo';

let program: ts.Program;
let checker: ts.TypeChecker;
let sources: ts.SourceFile[];

beforeAll(() => {
  const files = productionSources();
  program = tsProgram(files);
  checker = program.getTypeChecker();
  sources = files.map((f) => program.getSourceFile(f)!);
}, 120_000);

const typeName = (e: ts.Expression) => checker.typeToString(checker.getTypeAtLocation(e));

/**
 * What string an expression evaluates to, as far as the code says: a literal,
 * a const (followed across imports), a template whose unknown parts read as
 * <expression>, or a call to a one-expression function whose parameters read
 * as <parameter>. Anything else is <dynamic: expression>.
 */
function resolveKey(expr: ts.Expression, depth = 0): string {
  const lit = literalText(expr);
  if (lit !== null) return lit;
  if (depth > 5) return `<dynamic: ${expr.getText()}>`;
  const type = checker.getTypeAtLocation(expr);
  if (type.isStringLiteral()) return type.value;
  if (ts.isParenthesizedExpression(expr)) return resolveKey(expr.expression, depth + 1);
  if (ts.isTemplateExpression(expr)) {
    return (
      expr.head.text +
      expr.templateSpans
        .map((s) => {
          const inner = resolveKey(s.expression, depth + 1);
          return (inner.startsWith('<dynamic: ') ? `<${s.expression.getText()}>` : inner) + s.literal.text;
        })
        .join('')
    );
  }
  if (ts.isIdentifier(expr) || ts.isPropertyAccessExpression(expr)) {
    let sym = checker.getSymbolAtLocation(expr);
    if (sym && sym.flags & ts.SymbolFlags.Alias) sym = checker.getAliasedSymbol(sym);
    const decl = sym?.valueDeclaration;
    if (decl && ts.isParameter(decl)) return `<${decl.name.getText()}>`;
    if (decl && ts.isVariableDeclaration(decl) && decl.initializer && ts.getCombinedNodeFlags(decl) & ts.NodeFlags.Const) {
      return resolveKey(decl.initializer, depth + 1);
    }
  }
  if (ts.isCallExpression(expr)) {
    let sym = checker.getSymbolAtLocation(expr.expression);
    if (sym && sym.flags & ts.SymbolFlags.Alias) sym = checker.getAliasedSymbol(sym);
    const decl = sym?.valueDeclaration;
    const fn = decl && ts.isVariableDeclaration(decl) ? decl.initializer : decl;
    if (fn && (ts.isArrowFunction(fn) || ts.isFunctionExpression(fn) || ts.isFunctionDeclaration(fn)) && fn.body) {
      let body: ts.Node = fn.body;
      if (ts.isBlock(body) && body.statements.length === 1 && ts.isReturnStatement(body.statements[0])) {
        body = body.statements[0].expression ?? body;
      }
      if (!ts.isBlock(body)) return resolveKey(body as ts.Expression, depth + 1);
    }
  }
  return `<dynamic: ${expr.getText().replace(/\s+/g, ' ')}>`;
}

const receiverOf = (call: ts.CallExpression) =>
  ts.isPropertyAccessExpression(call.expression) ? call.expression : null;

// ---------------------------------------------------------------- storage

const STORAGE_METHODS = new Set(['getItem', 'setItem', 'removeItem', 'clear', 'key']);

interface StorageUse {
  key: string;
  storage: string;
  op: string;
  file: string;
}

function collectStorageUses(): StorageUse[] {
  const uses: StorageUse[] = [];
  for (const sf of sources) {
    walk(sf, (node) => {
      if (!ts.isCallExpression(node)) return;
      const access = receiverOf(node);
      if (!access || !STORAGE_METHODS.has(access.name.text) || typeName(access.expression) !== 'Storage') return;
      const arg = node.arguments[0];
      uses.push({
        key: access.name.text === 'clear' ? '(every key)' : arg ? resolveKey(arg) : '(no key)',
        storage: access.expression.getText().replace(/^window\./, ''),
        op: access.name.text,
        file: srcRel(sf.fileName),
      });
    });
  }
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
      file: '../public/theme-init.js',
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
          uses.push({ name: arg ? resolveKey(arg) : '(none)', op: access.name.text, file, scope });
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

/** Parameters written into in-app links: '/login?mode=register', `/x?run=${id}`. */
function collectLinkParams(): LinkParam[] {
  const out: LinkParam[] = [];
  for (const sf of sources) {
    walk(sf, (node) => {
      let text: string | null = literalText(node);
      if (text === null && ts.isTemplateExpression(node)) {
        text = node.head.text + node.templateSpans.map((s) => `{}${s.literal.text}`).join('');
      }
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
      ...groupBy(uses, (u) => u.key, (u) => `${u.storage.padEnd(15)} ${u.op.padEnd(10)} ${u.file}`),
    ].join('\n');
    await expect(text + '\n').toMatchFileSnapshot('./__snapshots__/storageKeys.txt');
  });

  it('resolves every key to a name', () => {
    expect(storageUses().filter((u) => u.key.startsWith('<dynamic'))).toEqual([]);
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
      ...groupBy(page, (u) => u.name, (u) => `${u.op.padEnd(12)} ${u.file}`),
      '',
      '## Parameters written into in-app links',
      ...groupBy(links, (l) => l.name, (l) => `${l.link.padEnd(44)} ${l.file}`),
      '',
      '## API query strings built with URLSearchParams',
      ...groupBy(api, (u) => u.name, (u) => `${u.op.padEnd(12)} ${u.file}`),
    ].join('\n');
    await expect(text + '\n').toMatchFileSnapshot('./__snapshots__/queryParams.txt');
  });

  it('resolves SHORTCUT_PARAM to "go"', () => {
    expect(paramUses().some((u) => u.name === 'go' && u.op === 'get' && u.file === 'components/ProjectList.tsx')).toBe(true);
  });

  it('reads every parameter an in-app link or a backend deep link carries', () => {
    const read = new Set(paramUses().filter((u) => u.scope === 'page' && READS.has(u.op)).map((u) => u.name));
    const backend = loadDeepLinks().flatMap((l) => [...new URL(l.url, 'http://openv.test').searchParams.keys()]);
    const carried = uniq([...linkParams().map((l) => l.name), ...backend]);
    expect(carried.filter((name) => !read.has(name))).toEqual([]);
  });
});
