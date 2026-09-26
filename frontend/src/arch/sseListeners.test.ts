// Regenerate the snapshot (only in a PR that means to change what the app
// listens for):
//   npx vitest run src/arch -u
//
// The SSE events the frontend listens for (refactor plan S6, invariant I9):
// every addEventListener call, and every onmessage, onerror and onopen
// handler, on a value the type checker types as EventSource, across the
// production sources. Each name is resolved by the checker: a literal; a
// name the function's callers bind (a parameter, or the key of an
// Object.keys or Object.entries loop over a parameter), read at every call
// from that call's argument and never from the declared type, so a helper
// such as X15's hooks/useEventStream listens for exactly the names its
// callers pass and, with no callers yet, for none; otherwise a const or a
// string-literal type. Each server event must be one the server sends:
// contracts/sse-events.json, which TestSSEContract writes from the Go
// sources. A rename on either side fails here or there. The snapshot holds
// names only, never a file, so moving a listener into another module leaves
// it unchanged while each name still resolves; the files appear in failure
// messages instead. onerror also receives the server's own "error" event, so
// it counts as a listener for it.
import ts from 'typescript';
import { beforeAll, describe, expect, it, vi } from 'vitest';
import { REGENERATE, lineOf, literalText, productionSources, readRepoFile, srcRel, tsProgram, walk } from './repo';

/** The file the Go test writes; its events[].name are what the server sends. */
const CONTRACT = 'contracts/sse-events.json';

/** EventSource's own connection events, which the server never names. */
const CONNECTION_EVENTS = new Set(['open']);

/** The EventSource handler properties and the event each one receives. */
const HANDLER_PROPERTIES: Record<string, string> = { onmessage: 'message', onerror: 'error', onopen: 'open' };

interface Listener {
  /** The event name, or <unresolved: expression> when the scan cannot tell. */
  name: string;
  at: string;
}

let program: ts.Program;
let checker: ts.TypeChecker;
let sources: ts.SourceFile[];

beforeAll(() => {
  const files = productionSources();
  program = tsProgram(files);
  checker = program.getTypeChecker();
  sources = files.map((f) => program.getSourceFile(f)!);
}, 120_000);

const at = (node: ts.Node) => `${srcRel(node.getSourceFile().fileName)}:${lineOf(node)}`;

const symbolOf = (node: ts.Node) => {
  const sym = checker.getSymbolAtLocation(node);
  return sym && sym.flags & ts.SymbolFlags.Alias ? checker.getAliasedSymbol(sym) : sym;
};

/** Whether an expression is an EventSource (null and undefined aside). */
function isEventSource(expr: ts.Expression): boolean {
  const type = checker.getNonNullableType(checker.getTypeAtLocation(expr));
  return type.getSymbol()?.getName() === 'EventSource';
}

/**
 * The event names an expression can hold: a literal; a name its function's
 * callers bind (a parameter, or a key of Object.keys/Object.entries over a
 * parameter), read at every call with that call's argument; otherwise a value
 * of a single string literal type (a const, say), never a union of them. A caller-bound
 * name is never read from its declared type, which lists every name the
 * function could take rather than the ones its callers pass. null when the
 * scan cannot tell.
 */
function resolveNames(expr: ts.Expression, depth = 0): string[] | null {
  const lit = literalText(expr);
  if (lit !== null) return [lit];
  if (depth >= 4) return null;
  const bound = ts.isIdentifier(expr) ? callerBound(expr, depth) : undefined;
  if (bound !== undefined) return bound;
  // A single string-literal type (a const, say) names one event. A union of
  // them is not read: it lists every name a value could hold, not the ones
  // this listener takes, so a hook casting its keys to the server-event union
  // would pass whatever its callers listen for.
  const type = checker.getTypeAtLocation(expr);
  if (type.isStringLiteral()) return [type.value];
  return null;
}

const unwrap = (e: ts.Expression): ts.Expression => {
  while (
    ts.isParenthesizedExpression(e) ||
    ts.isAsExpression(e) ||
    ts.isSatisfiesExpression(e) ||
    ts.isNonNullExpression(e)
  ) {
    e = e.expression;
  }
  return e;
};

/** A parameter's function and the parameter's position in it, or null. */
function paramOf(id: ts.Identifier): { fn: ts.SignatureDeclaration; index: number } | null {
  const decl = symbolOf(id)?.valueDeclaration;
  if (!decl || !ts.isParameter(decl) || !ts.isFunctionLike(decl.parent)) return null;
  return { fn: decl.parent, index: decl.parent.parameters.indexOf(decl) };
}

/**
 * Every call of a function in the production sources. null when the function
 * has no name to find its calls by, or is referenced other than as a callee
 * (passed as a value), since its arguments are then out of sight.
 */
function callsOf(fn: ts.SignatureDeclaration): ts.CallExpression[] | null {
  const nameNode = ts.isVariableDeclaration(fn.parent) ? fn.parent.name : (fn as ts.FunctionDeclaration).name;
  const target = nameNode && symbolOf(nameNode);
  if (!nameNode || !target) return null;
  const calls: ts.CallExpression[] = [];
  let escaped = false;
  for (const sf of sources) {
    walk(sf, (node) => {
      if (!ts.isIdentifier(node) || node === nameNode || symbolOf(node) !== target) return;
      const p = node.parent;
      if (ts.isImportSpecifier(p) || ts.isImportClause(p) || ts.isExportSpecifier(p)) return;
      if (ts.isCallExpression(p) && p.expression === node) calls.push(p);
      else escaped = true;
    });
  }
  return escaped ? null : calls;
}

/**
 * The names a caller-bound identifier takes across every call, or undefined
 * when the identifier is not caller-bound. A function nobody calls binds none.
 */
function callerBound(id: ts.Identifier, depth: number): string[] | null | undefined {
  const param = paramOf(id);
  if (param) {
    const calls = callsOf(param.fn);
    if (!calls) return null;
    const names: string[] = [];
    for (const call of calls) {
      const arg = call.arguments[param.index];
      const got = arg ? resolveNames(arg, depth + 1) : null;
      if (got === null) return null;
      names.push(...got);
    }
    return names;
  }
  // for (const name of Object.keys(p)) / for (const [name] of Object.entries(p))
  const decl = symbolOf(id)?.valueDeclaration;
  if (!decl) return undefined;
  let v: ts.Node = decl;
  let viaEntries = false;
  if (ts.isBindingElement(v) && ts.isArrayBindingPattern(v.parent) && v.parent.elements.indexOf(v) === 0) {
    viaEntries = true;
    v = v.parent.parent;
  }
  const loop = ts.isVariableDeclaration(v) && ts.isVariableDeclarationList(v.parent) ? v.parent.parent : undefined;
  if (!loop || !ts.isForOfStatement(loop)) return undefined;
  const over = unwrap(loop.expression);
  if (
    !ts.isCallExpression(over) ||
    !ts.isPropertyAccessExpression(over.expression) ||
    over.expression.expression.getText() !== 'Object' ||
    over.expression.name.text !== (viaEntries ? 'entries' : 'keys')
  ) {
    return undefined;
  }
  const source = over.arguments[0] && unwrap(over.arguments[0]);
  const sourceParam = source && ts.isIdentifier(source) ? paramOf(source) : null;
  if (!sourceParam) return null;
  const calls = callsOf(sourceParam.fn);
  if (!calls) return null;
  const names: string[] = [];
  for (const call of calls) {
    const arg = call.arguments[sourceParam.index];
    const obj = arg && unwrap(arg);
    if (!obj || !ts.isObjectLiteralExpression(obj)) return null;
    for (const prop of obj.properties) {
      const keyed =
        ts.isPropertyAssignment(prop) || ts.isShorthandPropertyAssignment(prop) || ts.isMethodDeclaration(prop);
      if (!keyed) return null;
      const key = ts.isIdentifier(prop.name) ? prop.name.text : literalText(prop.name);
      if (key === null) return null;
      names.push(key);
    }
  }
  return names;
}

function collectListeners(): Listener[] {
  const out: Listener[] = [];
  const add = (names: string[] | null, node: ts.Node, expr: ts.Node) => {
    for (const name of names ?? [`<unresolved: ${expr.getText().replace(/\s+/g, ' ')}>`]) out.push({ name, at: at(node) });
  };
  for (const sf of sources) {
    walk(sf, (node) => {
      // es.addEventListener(name, handler)
      if (ts.isCallExpression(node) && ts.isPropertyAccessExpression(node.expression)) {
        const access = node.expression;
        if (access.name.text === 'addEventListener' && isEventSource(access.expression)) {
          const arg = node.arguments[0];
          add(arg ? resolveNames(arg) : null, node, arg ?? node);
        }
        return;
      }
      // es.onerror = handler
      if (
        ts.isBinaryExpression(node) &&
        node.operatorToken.kind === ts.SyntaxKind.EqualsToken &&
        ts.isPropertyAccessExpression(node.left) &&
        Object.prototype.hasOwnProperty.call(HANDLER_PROPERTIES, node.left.name.text) &&
        isEventSource(node.left.expression)
      ) {
        add([HANDLER_PROPERTIES[node.left.name.text]], node, node.left);
      }
    });
  }
  return out;
}

interface Contract {
  events: { name: string; streams: string[] }[];
}

function serverEvents(): string[] {
  const contract = JSON.parse(readRepoFile(CONTRACT)) as Contract;
  return contract.events.map((e) => e.name);
}

let cached: Listener[] | undefined;
const listeners = () => (cached ??= collectListeners());
const uniq = (items: string[]) => [...new Set(items)].sort();

// Parsing and type-checking the source tree takes seconds; allow for a loaded CI runner.
vi.setConfig({ testTimeout: 60_000 });

describe('SSE listeners', () => {
  it('are found at all (the scan has not gone blind)', () => {
    expect(listeners().length).toBeGreaterThan(0);
    expect(serverEvents().length).toBeGreaterThan(0);
  });

  it('resolve every event name', () => {
    const unresolved = listeners().filter((l) => l.name.startsWith('<unresolved'));
    expect(
      unresolved.map((l) => `${l.at}  ${l.name}`),
      'name each event with a literal, a const or a string-literal type the checker can read, or take it from ' +
        'callers that pass a literal (as an argument, or as the keys of an object literal a helper loops over)'
    ).toEqual([]);
  });

  it(`listen only for events the server sends (${CONTRACT})`, () => {
    const sent = new Set(serverEvents());
    const stray = listeners().filter((l) => !CONNECTION_EVENTS.has(l.name) && !sent.has(l.name));
    expect(
      stray.map((l) => `${l.at}  ${l.name}`),
      `the server sends only: ${[...sent].sort().join(', ')} (${CONTRACT}, written by TestSSEContract in ` +
        'internal/api/sse_contract_test.go); an event is renamed on both sides together, in a behavior-change PR'
    ).toEqual([]);
  });

  it('match the pinned list', async () => {
    const all = listeners();
    const names = uniq(all.map((l) => l.name));
    const text = [
      '# SSE events the frontend listens for (invariant I9): each addEventListener, onmessage, onerror and onopen',
      `# on an EventSource, by event name. Every server event must be in ${CONTRACT}.`,
      `# Regenerate: ${REGENERATE}. Deployed servers send these names; renaming one silently drops its events.`,
      '',
      `events: ${names.length}`,
      '',
      ...names.map((n) => (CONNECTION_EVENTS.has(n) ? `${n}  (EventSource connection event)` : n)),
    ].join('\n');
    await expect(text + '\n').toMatchFileSnapshot('./__snapshots__/sseListeners.txt');
  });
});
